#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="$(mktemp -d /tmp/simple-cluster-ord-charts.XXXXXX)"
trap 'rm -rf "$output_dir"' EXIT

helm template simple-cluster "$repo_root/charts/simple-cluster-ui" \
  --set PUBLIC_HOST=simple-cluster.example \
  --set PUBLIC_SCHEME=https >"$output_dir/ui.yaml"
helm template simple-cluster "$repo_root/charts/simple-cluster-pm-integration" \
  --set PUBLIC_HOST=simple-cluster.example \
  --set PUBLIC_SCHEME=https >"$output_dir/pm.yaml"
helm template simple-cluster "$repo_root/charts/simple-cluster-system" \
  --set global.imageTag=0.35.0 >"$output_dir/system.yaml"

assert_contains() {
  local file="$1"
  local expected="$2"
  if ! grep -Fq -- "$expected" "$file"; then
    echo "Missing expected chart output in $file: $expected" >&2
    exit 1
  fi
}

assert_contains "$output_dir/ui.yaml" "name: simple-cluster-ord"
assert_contains "$output_dir/ui.yaml" "mountPath: /usr/share/nginx/html/ord"
assert_contains "$output_dir/pm.yaml" 'configUrl: "https://simple-cluster.example/.well-known/open-resource-discovery"'
assert_contains "$output_dir/pm.yaml" 'url: "https://simple-cluster.example/ui-extensions/ord/?v=0.6.0"'
assert_contains "$output_dir/system.yaml" "--ord-catalog-configmap=simple-cluster-ord"
assert_contains "$output_dir/system.yaml" "--ord-catalog-system-version=0.35.0"
