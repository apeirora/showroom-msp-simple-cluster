"""Exercise the SBOM gate with isolated local generator fixtures."""

import json
import importlib.util
import contextlib
import io
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("normalize_spdx", ROOT / "hack/normalize-spdx.py")
NORMALIZER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(NORMALIZER)


class SbomGateTest(unittest.TestCase):
    """Run the real gate against fake generators without Docker or network access."""

    def setUp(self):
        """Create an isolated repository and generators; errors propagate to unittest."""
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for directory in ("hack", "sbom", "generated", "docs"):
            (self.root / directory).mkdir()
        shutil.copy(ROOT / "hack/check-sbom-current.sh", self.root / "hack")
        helper = ROOT / "hack/normalize-spdx.py"
        if helper.exists():
            shutil.copy(helper, self.root / "hack")
        (self.root / "hack/generate-sbom.sh").write_text(
            'mkdir -p "$SBOM_DIR"\ncp generated/* "$SBOM_DIR"/\n'
        )
        (self.root / "hack/generate-third-party-licenses.py").write_text(
            'import os\nfrom pathlib import Path\n'
            'Path(os.environ["LICENSES_OUT"]).write_text("audit\\n")\n'
        )
        (self.root / "docs/third-party-licenses.md").write_text("audit\n")
        self.spdx = {
            "creationInfo": {"created": "2026-01-01", "creators": ["Tool: syft"]},
            "documentNamespace": "https://example.com/one",
            "packages": [{"SPDXID": "SPDXRef-package", "versionInfo": "1", "licenseConcluded": "MIT"}],
            "relationships": [{"spdxElementId": "SPDXRef-DOCUMENT", "relatedSpdxElement": "SPDXRef-package", "relationshipType": "DESCRIBES"}],
        }
        for directory in ("sbom", "generated"):
            (self.root / directory / "app.cdx.json").write_text('{"components": []}')
            self.write_spdx(directory, self.spdx)

    def write_spdx(self, directory, document):
        """Write document as app SPDX JSON in directory; filesystem errors propagate."""
        (self.root / directory / "app.spdx.json").write_text(json.dumps(document))

    def run_gate(self):
        """Return the gate result from the isolated repository, capturing its output."""
        return subprocess.run(
            ["bash", "hack/check-sbom-current.sh"], cwd=self.root,
            text=True, capture_output=True, check=False,
        )

    def test_stale_spdx_fails(self):
        """A changed SPDX licence must fail even when CycloneDX and audit match."""
        self.spdx["packages"][0]["licenseConcluded"] = "GPL-3.0-only"
        self.write_spdx("generated", self.spdx)
        self.assertNotEqual(self.run_gate().returncode, 0)

    def test_volatile_fields_and_order_are_ignored(self):
        """Timestamp, namespace, and SPDX array order do not change the result."""
        self.spdx["creationInfo"]["created"] = "2026-02-01"
        self.spdx["documentNamespace"] = "https://example.com/two"
        self.write_spdx("generated", self.spdx)
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_missing_or_extra_spdx_fails(self):
        """Neither missing nor extra SPDX documents are silently skipped."""
        for directory in ("sbom", "generated"):
            with self.subTest(directory=directory, situation="missing"):
                path = self.root / directory / "app.spdx.json"
                path.unlink()
                self.assertNotEqual(self.run_gate().returncode, 0)
                self.write_spdx(directory, self.spdx)
            with self.subTest(directory=directory, situation="extra"):
                path = self.root / directory / "extra.spdx.json"
                path.write_text("{}")
                self.assertNotEqual(self.run_gate().returncode, 0)
                path.unlink()

    def test_other_spdx_content_is_checked(self):
        """Changes to versions, checksums, relationships and creators fail."""
        mutations = [
            lambda d: d["packages"][0].update(versionInfo="2"),
            lambda d: d["packages"][0].update(checksums=[{"algorithm": "SHA256", "checksumValue": "abc"}]),
            lambda d: d["relationships"].clear(),
            lambda d: d["creationInfo"].update(creators=["Tool: other"]),
        ]
        for mutate in mutations:
            with self.subTest(mutation=mutate):
                document = json.loads(json.dumps(self.spdx))
                mutate(document)
                self.write_spdx("generated", document)
                self.assertNotEqual(self.run_gate().returncode, 0)

    def test_invalid_spdx_fails(self):
        """Invalid JSON is an error, not an empty or ignored document."""
        (self.root / "generated/app.spdx.json").write_text("{")
        self.assertNotEqual(self.run_gate().returncode, 0)

    def test_array_order_is_ignored(self):
        """Equivalent SPDX sets compare equal despite reordered elements."""
        self.spdx["creationInfo"]["creators"].append("Person: somebody")
        self.write_spdx("sbom", self.spdx)
        self.spdx["creationInfo"]["creators"].reverse()
        self.write_spdx("generated", self.spdx)
        self.assertEqual(self.run_gate().returncode, 0)

    def test_no_documents_fails(self):
        """An empty SBOM set cannot pass as current."""
        for directory in ("sbom", "generated"):
            for path in (self.root / directory).glob("*"):
                path.unlink()
        self.assertNotEqual(self.run_gate().returncode, 0)

    def test_normalizer_directly(self):
        """Exercise normalization and missing or malformed files in process."""
        documents = NORMALIZER.load_documents(self.root / "sbom")
        self.assertNotIn("documentNamespace", documents["app"])
        self.assertNotIn("created", documents["app"]["creationInfo"])
        self.assertEqual(NORMALIZER.normalize([{"b": 2}, {"a": 1}]), [{"a": 1}, {"b": 2}])
        (self.root / "sbom/app.spdx.json").unlink()
        with self.assertRaises(ValueError):
            NORMALIZER.load_documents(self.root / "sbom")
        (self.root / "sbom/app.spdx.json").write_text("{")
        with self.assertRaises(json.JSONDecodeError):
            NORMALIZER.load_documents(self.root / "sbom")

    def test_normalizer_cli(self):
        """The normalizer prints JSON and rejects missing command arguments."""
        with mock.patch.object(NORMALIZER.sys, "argv", ["normalize-spdx.py", str(self.root / "sbom")]):
            with contextlib.redirect_stdout(io.StringIO()) as output:
                NORMALIZER.main()
        self.assertIn("app", json.loads(output.getvalue()))
        with mock.patch.object(NORMALIZER.sys, "argv", ["normalize-spdx.py"]):
            with self.assertRaises(ValueError):
                NORMALIZER.main()


if __name__ == "__main__":
    unittest.main()
