#!/usr/bin/env bash
# CI gate: the committed SBOM set and licence audit must still describe the
# dependencies the repository actually declares.
#
# CycloneDX compares dependency tuples. SPDX compares complete normalized
# documents, excluding only the creation timestamp and document namespace.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo ">>> regenerating the SBOM set from the working tree"
SBOM_DIR="$TMP/sbom" bash hack/generate-sbom.sh >/dev/null

normalise() {
  python3 - "$1" <<'PY'
import glob, json, os, sys
rows = []
for path in sorted(glob.glob(os.path.join(sys.argv[1], "*.cdx.json"))):
    component = os.path.basename(path)[: -len(".cdx.json")]
    for c in json.load(open(path)).get("components", []):
        licences = sorted({
            (e.get("license") or {}).get("id")
            or (e.get("license") or {}).get("name")
            or e.get("expression")
            or ""
            for e in (c.get("licenses") or [])
        })
        rows.append(f"{component}\t{c['name']}\t{c.get('version','')}\t{','.join(licences)}")
print("\n".join(sorted(rows)))
PY
}

normalise sbom > "$TMP/committed.txt"
normalise "$TMP/sbom" > "$TMP/regenerated.txt"

if ! diff -u "$TMP/committed.txt" "$TMP/regenerated.txt"; then
  echo
  echo "The committed SBOM no longer matches the declared dependencies." >&2
  echo "Run ./hack/generate-sbom.sh and hack/generate-third-party-licenses.py," >&2
  echo "then commit sbom/ and docs/third-party-licenses.md." >&2
  exit 1
fi

python3 hack/normalize-spdx.py sbom > "$TMP/committed-spdx.json"
python3 hack/normalize-spdx.py "$TMP/sbom" > "$TMP/regenerated-spdx.json"
if ! diff -u "$TMP/committed-spdx.json" "$TMP/regenerated-spdx.json"; then
  echo "The committed SPDX documents are stale. Run ./hack/generate-sbom.sh and commit sbom/." >&2
  exit 1
fi

echo ">>> regenerating the licence audit"
SBOM_DIR="$TMP/sbom" LICENSES_OUT="$TMP/third-party-licenses.md" \
  python3 hack/generate-third-party-licenses.py >/dev/null

if ! diff -u docs/third-party-licenses.md "$TMP/third-party-licenses.md"; then
  echo
  echo "docs/third-party-licenses.md is stale — regenerate and commit it." >&2
  exit 1
fi

echo "SBOM and licence audit are current."
