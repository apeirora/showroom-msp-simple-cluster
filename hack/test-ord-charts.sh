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

# The create dialog discovers providers from this served JSON, not from SCPCs.
python3 - "$output_dir/ui.yaml" <<'PYTEST'
import json
from pathlib import Path
import sys

lines = Path(sys.argv[1]).read_text().splitlines()
config_line = next(i for i, line in enumerate(lines)
                   if line.strip() == 'simple-cluster-config.json: |-')
config = json.loads(lines[config_line + 1])
source = next((s for s in config['providerSources']
               if s.get('providerConfigName') == 'telekom'), None)
expected = {
    'group': 'gardener.t-systems.com', 'version': 'v1',
    'listField': 'ShootClaims', 'label': 'Telekom OSC',
    'providerConfigName': 'telekom', 'namespaced': True,
}
if source != expected:
    raise SystemExit(f'Telekom provider discovery has the wrong contract: {source}')
PYTEST
