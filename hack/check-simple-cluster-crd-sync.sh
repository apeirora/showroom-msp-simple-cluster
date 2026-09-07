#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
base_dir="$repo_root/simple-cluster-operator/config/crd/bases"
chart_dir="$repo_root/charts/simple-cluster-system/charts/simple-cluster-operator/templates/crd"

render_expected_chart_crd() {
  local base=$1

  printf '%s\n' '{{- if .Values.crd.enable }}'
  sed -n '1,4p' "$base"
  printf '%s\n' \
    '  labels:' \
    '    {{- include "chart.labels" . | nindent 4 }}' \
    '  annotations:' \
    '    {{- if .Values.crd.keep }}' \
    '    "helm.sh/resource-policy": keep' \
    '    {{- end }}'
  sed -n '6p' "$base"
  sed -n '7,$p' "$base"
  printf '%s\n' '{{- end -}}'
}

failed=0
for resource in \
  showroom.apeirora.eu_simpleclusterproviderconfigs.yaml \
  showroom.apeirora.eu_simpleclusters.yaml; do
  base="$base_dir/$resource"
  chart="$chart_dir/$resource"
  if ! diff -u "$chart" <(render_expected_chart_crd "$base"); then
    failed=1
  fi
done

if (( failed != 0 )); then
  echo "generated SimpleCluster CRDs and the shipped Helm chart are out of sync" >&2
  exit 1
fi

echo "SimpleCluster generated and Helm-packaged CRDs are in sync"
