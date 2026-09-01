#!/usr/bin/env python3
"""Render docs/third-party-licenses.md from the committed CycloneDX SBOMs.

Run `hack/generate-sbom.sh` first, then this script. Both are re-run whenever
dependencies change, so the audit document never drifts from the SBOM.

Reads sbom/ and writes docs/third-party-licenses.md unless SBOM_DIR and
LICENSES_OUT say otherwise.
"""
import glob
import json
import os
from collections import defaultdict

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SBOM_DIR = os.environ.get("SBOM_DIR") or os.path.join(ROOT, "sbom")
OUT = os.environ.get("LICENSES_OUT") or os.path.join(ROOT, "docs", "third-party-licenses.md")

# syft records a content hash instead of an SPDX id when a LICENSE file does not
# match a known template verbatim. These were resolved by reading the LICENSE
# file in the Go module cache; see the "Unmatched licence texts" section below.
OVERRIDES = {
    "dario.cat/mergo": "BSD-3-Clause",
    "github.com/prometheus/client_golang": "Apache-2.0",
    "github.com/prometheus/client_model": "Apache-2.0",
    "github.com/prometheus/common": "Apache-2.0",
    "github.com/prometheus/procfs": "Apache-2.0",
    "k8s.io/kube-openapi": "Apache-2.0",
    "sigs.k8s.io/kind": "Apache-2.0",
    "sigs.k8s.io/randfill": "Apache-2.0",
}

# Components published from this repository — never third party.
def is_self(name: str) -> bool:
    return name.startswith("/src/") or name == "simple-cluster-ui"


def licences_of(component) -> list:
    out = []
    for entry in component.get("licenses") or []:
        lic = entry.get("license") or {}
        value = lic.get("id") or lic.get("name") or entry.get("expression")
        if value and not value.startswith("sha256:"):
            out.append(value)
    if not out:
        out = [OVERRIDES.get(component["name"], "UNKNOWN")]
    return sorted(set(out))


def main():
    deps = {}                       # (name, version) -> {"lic": [...], "in": {component}}
    unmatched = set()
    for path in sorted(glob.glob(os.path.join(SBOM_DIR, "*.cdx.json"))):
        shipped = os.path.basename(path)[: -len(".cdx.json")]
        for c in json.load(open(path)).get("components", []):
            if is_self(c["name"]):
                continue
            key = (c["name"], c.get("version", ""))
            rec = deps.setdefault(key, {"lic": set(), "in": set()})
            rec["lic"].update(licences_of(c))
            rec["in"].add(shipped)
            if any(
                ((e.get("license") or {}).get("id") or (e.get("license") or {}).get("name") or e.get("expression") or "").startswith("sha256:")
                for e in (c.get("licenses") or [])
            ):
                unmatched.add(key)

    by_lic = defaultdict(list)
    for (name, version), rec in deps.items():
        by_lic[" AND ".join(sorted(rec["lic"]))].append((name, version, sorted(rec["in"])))

    copyleft = sorted(
        k for k in by_lic if any(t in k for t in ("GPL", "MPL", "EPL", "CDDL", "SSPL"))
    )

    lines = []
    w = lines.append
    w("# Third-party licences")
    w("")
    w("Every direct and transitive dependency of every component published from this")
    w("repository, with the licence it is distributed under.")
    w("")
    w("This document is generated — do not edit it by hand:")
    w("")
    w("```sh")
    w("./hack/generate-sbom.sh                     # refresh sbom/*.json")
    w("python3 hack/generate-third-party-licenses.py")
    w("```")
    w("")
    w("The source of truth is the CycloneDX SBOM set under [`sbom/`](../sbom). Go")
    w("dependencies come from each module's `go.mod`; UI dependencies come from")
    w("`ui/package-lock.json`, production scope only, because dev-only packages are")
    w("build-time tooling and are not part of any shipped artefact.")
    w("")
    w("## Summary")
    w("")
    w(f"- **{len(deps)}** distinct third-party dependencies across "
      f"**{len(glob.glob(os.path.join(SBOM_DIR, '*.cdx.json')))}** published components.")
    w("- Licences in use: " + ", ".join(f"`{k}` ({len(v)})" for k, v in sorted(by_lic.items(), key=lambda kv: -len(kv[1]))) + ".")
    if copyleft:
        w("- **Copyleft licences present — review obligations:** " + ", ".join(f"`{c}`" for c in copyleft) + ".")
    else:
        w("- **No copyleft dependencies.** Every licence above is permissive "
          "(Apache-2.0, BSD, MIT, ISC), so redistribution under Apache-2.0 carries no "
          "source-disclosure obligation.")
    w("")
    w("## Obligations")
    w("")
    w("| Licence | Obligation when redistributing | How this repository satisfies it |")
    w("|---|---|---|")
    w("| Apache-2.0 | Retain copyright, licence and NOTICE; state modifications | Dependencies are consumed unmodified as Go modules / npm packages; their licence headers ship inside the module and inside the container image layers. This repository's own licence is [Apache-2.0](../LICENSE). |")
    w("| BSD-2-Clause, BSD-3-Clause | Retain copyright notice and disclaimer; do not use the author's name to endorse | Notices ship unmodified with each dependency; no endorsement is claimed. |")
    w("| MIT, ISC | Retain copyright notice and permission notice | Notices ship unmodified with each dependency. |")
    w("| Unlicense OR Apache-2.0 | Dual-licensed; this project takes the Apache-2.0 branch | Treated as Apache-2.0 above. |")
    w("")
    w("The one vendored artefact in this repository is")
    w("`charts/simple-cluster-sync-agent/charts/api-syncagent-0.4.2.tgz`, the upstream")
    w("[kcp api-syncagent](https://github.com/kcp-dev/helm-charts) Helm chart,")
    w("redistributed unmodified under Apache-2.0 and annotated as such in")
    w("[`REUSE.toml`](../REUSE.toml). Its own `LICENSE` travels inside the archive.")
    w("")
    if unmatched:
        w("## Unmatched licence texts")
        w("")
        w("syft could not match these projects' `LICENSE` files to an SPDX template")
        w("verbatim (added preamble, reformatting). Each was resolved by reading the file")
        w("in the Go module cache and is recorded in `OVERRIDES` in")
        w("`hack/generate-third-party-licenses.py`:")
        w("")
        w("| Dependency | Resolved licence |")
        w("|---|---|")
        for name, version in sorted(unmatched):
            w(f"| `{name}` {version} | `{OVERRIDES.get(name, 'UNKNOWN')}` |")
        w("")
    w("## Full inventory")
    w("")
    for lic in sorted(by_lic, key=lambda k: (-len(by_lic[k]), k)):
        rows = sorted(by_lic[lic])
        w(f"### {lic} ({len(rows)})")
        w("")
        w("| Dependency | Version | Used by |")
        w("|---|---|---|")
        for name, version, where in rows:
            w(f"| `{name}` | `{version}` | {', '.join(where)} |")
        w("")

    out = OUT
    os.makedirs(os.path.dirname(out), exist_ok=True)
    with open(out, "w") as fh:
        fh.write("\n".join(lines).rstrip() + "\n")
    print(f"wrote {out}: {len(deps)} dependencies, {len(by_lic)} distinct licences")


if __name__ == "__main__":
    main()
