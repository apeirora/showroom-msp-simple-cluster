#!/usr/bin/env bash
# Experiment helper for bind-watcher-operator: build+push a personal image and
# deploy it to msp03 with a plain manifest. Not for production — once proven,
# switch to the official build/chart.
#
# Usage:
#   ./experiment.sh publish   # docker build + push the :latest image
#   ./experiment.sh deploy    # kubectl apply the manifest on msp03
#   ./experiment.sh all       # publish then deploy
#
# Between publish and deploy, make the ghcr package PUBLIC (the manifest has no
# imagePullSecret).
set -euo pipefail

IMAGE="${IMAGE:-ghcr.io/m-szalik/bind-watcher}"
TAG="${TAG:-latest}"
NAMESPACE="${NAMESPACE:-simple-cluster-system}"
KUBECONFIG_MSP03="${KUBECONFIG_MSP03:-$HOME/SAP/cc-d2-msp03-kubeconfig.yaml}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OPERATOR_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFEST="$SCRIPT_DIR/deploy.yaml"

publish() {
  echo ">>> build $IMAGE:$TAG"
  docker build -t "$IMAGE:$TAG" "$OPERATOR_DIR"
  echo ">>> push $IMAGE:$TAG"
  docker push "$IMAGE:$TAG"
  echo ">>> now make the ghcr package public, then: $0 deploy"
}

deploy() {
  [ -f "$KUBECONFIG_MSP03" ] || { echo "kubeconfig not found: $KUBECONFIG_MSP03" >&2; exit 1; }
  export KUBECONFIG="$KUBECONFIG_MSP03"

  echo ">>> target: $(kubectl config current-context)"
  if ! kubectl -n "$NAMESPACE" get secret pm-kubeconfig >/dev/null 2>&1; then
    echo "WARNING: secret pm-kubeconfig missing in $NAMESPACE — the operator will crashloop until KCP access is provided." >&2
  fi

  echo ">>> apply $MANIFEST"
  kubectl apply -f "$MANIFEST"
  # Force a re-pull of :latest on repeat deploys.
  kubectl -n "$NAMESPACE" rollout restart deploy/bind-watcher-operator
  kubectl -n "$NAMESPACE" rollout status deploy/bind-watcher-operator --timeout=120s
  echo ">>> logs: KUBECONFIG=$KUBECONFIG_MSP03 kubectl -n $NAMESPACE logs -l app=bind-watcher-operator -f"
}

case "${1:-}" in
  publish) publish ;;
  deploy)  deploy ;;
  all)     publish; deploy ;;
  *) echo "usage: $0 {publish|deploy|all}" >&2; exit 2 ;;
esac
