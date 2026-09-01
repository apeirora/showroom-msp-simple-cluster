#!/usr/bin/env bash
# Generate the repository SBOM set (CycloneDX + SPDX JSON, one pair per shipped
# component) from the current working tree.
#
#   ./hack/generate-sbom.sh [version]
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
SYFT_IMAGE="${SYFT_IMAGE:-anchore/syft:latest}"

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

mkdir -p sbom
for c in "${COMPONENTS[@]}"; do
  echo ">>> SBOM for ${c} @ ${VERSION}"
  docker run --rm \
    -v "$ROOT:/src:ro" -v "$ROOT/sbom:/out" -v "$GOMODCACHE:/gomodcache:ro" \
    -e SYFT_GOLANG_SEARCH_LOCAL_MOD_CACHE_LICENSES=true \
    -e SYFT_GOLANG_LOCAL_MOD_CACHE_DIR=/gomodcache \
    "$SYFT_IMAGE" scan "dir:/src/${c}" \
      --source-name "${c}" --source-version "${VERSION}" \
      --exclude './bin/**' --exclude './dist/**' \
      --exclude './node_modules/**' --exclude './testbin/**' \
      -o "cyclonedx-json=/out/${c}.cdx.json" \
      -o "spdx-json=/out/${c}.spdx.json"
done

# syft runs as root inside the container.
if [ -n "${SUDO_USER:-}" ] || [ "$(id -u)" -ne 0 ]; then
  docker run --rm -v "$ROOT/sbom:/out" --entrypoint chown "$SYFT_IMAGE" -R "$(id -u):$(id -g)" /out 2>/dev/null || true
fi

echo
echo "Wrote $(ls -1 sbom/*.json | wc -l) SBOM files to sbom/"
