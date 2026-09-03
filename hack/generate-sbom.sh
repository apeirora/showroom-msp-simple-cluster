#!/usr/bin/env bash
# Generate the repository SBOM set (CycloneDX + SPDX JSON, one pair per shipped
# component) from the current working tree.
#
#   ./hack/generate-sbom.sh [version]
#
# Writes to sbom/ unless SBOM_DIR is set.
#
# `version` defaults to the release-please manifest version, i.e. the version the
# release candidate is cut at. Requires Docker (uses anchore/syft).
#
# Locally built tool binaries (bin/), build output (dist/) and installed
# node_modules are excluded: the SBOM must describe what the release candidate
# ships, which is the declared dependency graph in go.mod / package-lock.json,
# not whatever a developer happens to have on disk.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${1:-$(python3 -c 'import json;print(json.load(open(".release-please-manifest.json"))["."])')}"
# Pinned: hack/check-sbom-current.sh fails the build on any SBOM diff, so an
# unpinned syft would turn that gate red whenever upstream changes its output.
SYFT_IMAGE="${SYFT_IMAGE:-anchore/syft:v1.51.1}"
SBOM_DIR="${SBOM_DIR:-$ROOT/sbom}"

# syft can only read a Go dependency's licence from the module cache, so make sure
# every module is downloaded and hand the cache to the container read-only.
GOMODCACHE="$(go env GOMODCACHE)"

# Every component that is built and published from this repository.
COMPONENTS=(
  simple-cluster-operator
  simple-cluster-operator-kind
  simple-cluster-operator-openstack
  simple-cluster-operator-ubuntu
  ubuntu-machines
  sshkey-operator
  bind-watcher-operator
  ui
)

for c in "${COMPONENTS[@]}"; do
  [ -f "${c}/go.mod" ] || continue
  ( cd "$c" && go mod download )
done

mkdir -p "$SBOM_DIR"
for c in "${COMPONENTS[@]}"; do
  echo ">>> SBOM for ${c} @ ${VERSION}"
  docker run --rm \
    -v "$ROOT:/src:ro" -v "$SBOM_DIR:/out" -v "$GOMODCACHE:/gomodcache:ro" \
    -e SYFT_GOLANG_SEARCH_LOCAL_MOD_CACHE_LICENSES=true \
    -e SYFT_GOLANG_LOCAL_MOD_CACHE_DIR=/gomodcache \
    "$SYFT_IMAGE" scan "dir:/src/${c}" \
      --source-name "${c}" --source-version "${VERSION}" \
      --exclude './bin/**' --exclude './dist/**' \
      --exclude './node_modules/**' --exclude './testbin/**' \
      -o "cyclonedx-json=/out/${c}.cdx.json" \
      -o "spdx-json=/out/${c}.spdx.json"
done

# syft runs as root inside the container; hand the files back.
docker run --rm -v "$SBOM_DIR:/out" --entrypoint chown "$SYFT_IMAGE" \
  -R "$(id -u):$(id -g)" /out >/dev/null 2>&1 || true

# syft reads a Go dependency's licence from the module cache. When that cache is
# not readable -- never populated, or mounted somewhere the container cannot see
# -- syft still lists the dependency, just with no licence, and the output looks
# perfectly plausible. Refuse it: a licence-less Go module means a broken run,
# not a dependency without a licence.
python3 - "$SBOM_DIR" <<'LICENCE_GUARD'
import glob, json, os, sys

missing = []
for path in sorted(glob.glob(os.path.join(sys.argv[1], "*.cdx.json"))):
    component = os.path.basename(path)[: -len(".cdx.json")]
    for c in json.load(open(path)).get("components", []):
        if not (c.get("purl") or "").startswith("pkg:golang/"):
            continue
        if not any(
            (e.get("license") or {}).get("id")
            or (e.get("license") or {}).get("name")
            or e.get("expression")
            for e in (c.get("licenses") or [])
        ):
            missing.append(f"{component}  {c['name']} {c.get('version', '')}")

if missing:
    print("Go dependencies came back with no licence:", file=sys.stderr)
    for m in missing:
        print("  " + m, file=sys.stderr)
    print("", file=sys.stderr)
    print("The module cache was not readable. Check that GOMODCACHE is "
          "populated and that Docker can bind-mount it.", file=sys.stderr)
    sys.exit(1)
LICENCE_GUARD

echo
echo "Wrote $(ls -1 "$SBOM_DIR"/*.json | wc -l) SBOM files to $SBOM_DIR"
