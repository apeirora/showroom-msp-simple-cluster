#!/usr/bin/env bash
#
# Regenerates the two Helm charts under `charts/` from each operator's
# kubebuilder helm/v1-alpha output.
#
# Result:
#   charts/simple-cluster-system/charts/
#     ├── simple-cluster-operator/
#     ├── kind-cluster-operator/
#     ├── simple-cluster-operator-openstack/
#     ├── simple-cluster-operator-ubuntu/
#     └── sshkey-operator/
#   charts/ubuntu-machines-operator/
#
# Usage:
#   scripts/regen-helm-charts.sh

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# (operator-dir → destination-under-charts/)
declare -A REST_OPERATORS=(
  [simple-cluster-operator]=simple-cluster-system/charts/simple-cluster-operator
  [simple-cluster-operator-kind]=simple-cluster-system/charts/kind-cluster-operator
  [simple-cluster-operator-openstack]=simple-cluster-system/charts/simple-cluster-operator-openstack
  [simple-cluster-operator-ubuntu]=simple-cluster-system/charts/simple-cluster-operator-ubuntu
  [sshkey-operator]=simple-cluster-system/charts/sshkey-operator
)

regenerate_one() {
  local op=$1 dest=$2
  echo ">>> $op"
  (cd "$op" && kubebuilder edit --plugins=helm/v1-alpha >/dev/null)
  # The plugin also creates .github/workflows in each operator. We
  # don't ship per-operator CI; drop it.
  rm -rf "$op/.github"
  rm -rf "charts/$dest"
  mkdir -p "$(dirname "charts/$dest")"
  cp -r "$op/dist/chart" "charts/$dest"
}

for op in "${!REST_OPERATORS[@]}"; do
  regenerate_one "$op" "${REST_OPERATORS[$op]}"
done

regenerate_one ubuntu-machines ubuntu-machines-operator
regenerate_one sshkey-operator sshkey-operator

echo
echo "OK. Charts regenerated:"
echo "  charts/simple-cluster-system   (umbrella for the 5 simple-cluster-system operators)"
echo "  charts/ubuntu-machines-operator"
echo "  charts/sshkey-operator"
