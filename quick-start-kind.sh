#!/usr/bin/env bash
#
# quick-start-kind.sh — boot a kind cluster, install KubeVirt + the
# simple-cluster-operator, and leave you with a working environment.
#
# Usage:
#   ./quick-start-kind.sh
#
# Optional env vars:
#   CLUSTER_NAME      (default: simple-cluster)
#   IMG               (default: simple-cluster-operator:dev — built locally
#                      and side-loaded into kind; no registry push)
#   KUBEVIRT_VERSION  (default: latest GitHub release of kubevirt/kubevirt)

set -euo pipefail

# Some snap-installed CLIs (kubectl, go) ship a /snap/bin/ wrapper that
# refuses to run on hosts where snap-confine lacks cap_dac_override.
# Prefer the unwrapped binary inside /snap/<name>/current/.
for inner in /snap/kubectl/current /snap/go/current/bin; do
    [ -d "$inner" ] && PATH="$inner:$PATH"
done
export PATH

CLUSTER_NAME="${CLUSTER_NAME:-simple-cluster}"
IMG="${IMG:-simple-cluster-operator:dev}"
KUBEVIRT_VERSION="${KUBEVIRT_VERSION:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OPERATOR_DIR="$SCRIPT_DIR/simple-cluster-operator"
OPERATOR_NS=simple-cluster-operator-system
OPERATOR_DEPLOY=simple-cluster-operator-controller-manager

log()  { printf '\033[1;34m▶ %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m⚠ %s\033[0m\n' "$*"; }

require() {
    command -v "$1" >/dev/null 2>&1 || {
        echo "missing required tool: $1" >&2
        exit 1
    }
}
require kind
require kubectl
require docker
require curl
require jq

[ -d "$OPERATOR_DIR" ] || {
    echo "operator directory not found: $OPERATOR_DIR" >&2
    exit 1
}

# ----------------------------------------------------------------------
# 1. kind cluster
# ----------------------------------------------------------------------
# Always mount the host docker socket so the kind-cluster-operator can
# spawn sibling kind clusters from inside this one (DooD). /dev/kvm is
# mounted only when present, so KubeVirt gets HW accel where possible.
KIND_CONFIG="$(mktemp)"
{
    echo "kind: Cluster"
    echo "apiVersion: kind.x-k8s.io/v1alpha4"
    echo "nodes:"
    echo "  - role: control-plane"
    echo "    extraMounts:"
    echo "      - hostPath: /var/run/docker.sock"
    echo "        containerPath: /var/run/docker.sock"
    if [ -e /dev/kvm ]; then
        echo "      - hostPath: /dev/kvm"
        echo "        containerPath: /dev/kvm"
    fi
} > "$KIND_CONFIG"

if [ -e /dev/kvm ]; then
    log "/dev/kvm present: kind nodes will use hardware-accelerated KVM"
    USE_EMULATION=false
else
    warn "/dev/kvm missing: KubeVirt will run in software-emulation mode (slow)"
    USE_EMULATION=true
fi
log "host docker.sock will be mounted into the kind node (for kind-cluster-operator DooD)"
KIND_ARGS=(--config "$KIND_CONFIG")

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
    log "kind cluster '$CLUSTER_NAME' already exists; deleting it"
    kind delete cluster --name "$CLUSTER_NAME"
fi
log "creating kind cluster '$CLUSTER_NAME'"
kind create cluster --name "$CLUSTER_NAME" "${KIND_ARGS[@]}"
kubectl cluster-info --context "kind-$CLUSTER_NAME"

# ----------------------------------------------------------------------
# 2. KubeVirt
# ----------------------------------------------------------------------
if [ -z "$KUBEVIRT_VERSION" ]; then
    KUBEVIRT_VERSION="$(curl -fsSL https://api.github.com/repos/kubevirt/kubevirt/releases/latest | jq -r .tag_name)"
fi
log "installing KubeVirt $KUBEVIRT_VERSION"

kubectl apply -f "https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_VERSION}/kubevirt-operator.yaml"
kubectl wait --for=condition=established --timeout=120s crd/kubevirts.kubevirt.io

# Apply our own KubeVirt CR so useEmulation matches the host capability.
kubectl apply -f - <<EOF
apiVersion: kubevirt.io/v1
kind: KubeVirt
metadata:
  name: kubevirt
  namespace: kubevirt
spec:
  configuration:
    developerConfiguration:
      useEmulation: $USE_EMULATION
EOF

log "waiting for KubeVirt to become Available (this can take a few minutes)"
kubectl -n kubevirt wait --for=condition=Available --timeout=600s kubevirt/kubevirt

# ----------------------------------------------------------------------
# 3. simple-cluster-operator
# ----------------------------------------------------------------------
log "building simple-cluster-operator image: $IMG"
docker build -t "$IMG" "$OPERATOR_DIR"

log "loading image into kind cluster '$CLUSTER_NAME'"
kind load docker-image "$IMG" --name "$CLUSTER_NAME"

log "applying CRDs + manager manifests"
# Kustomize forbids absolute paths in `resources:`, so the overlay has
# to live inside the operator tree where it can reference ../default.
OVERLAY="$(mktemp -d -p "$OPERATOR_DIR/config" .quickstart.XXXXXX)"
trap 'rm -rf "$OVERLAY"' EXIT
IMG_NAME="${IMG%:*}"
IMG_TAG="${IMG##*:}"
cat > "$OVERLAY/kustomization.yaml" <<EOF
resources:
  - ../default
images:
  - name: controller
    newName: $IMG_NAME
    newTag: $IMG_TAG
EOF
kubectl apply -k "$OVERLAY"

# Force a roll so repeat runs pick up the freshly side-loaded image even
# when the Deployment spec is unchanged.
kubectl -n "$OPERATOR_NS" rollout restart "deployment/$OPERATOR_DEPLOY" >/dev/null

log "waiting for $OPERATOR_DEPLOY to become Available"
kubectl -n "$OPERATOR_NS" wait --for=condition=Available --timeout=180s "deployment/$OPERATOR_DEPLOY"

# ----------------------------------------------------------------------
# 4. kind-cluster-operator (DooD via mounted /var/run/docker.sock)
# ----------------------------------------------------------------------
KIND_OP_DIR="$SCRIPT_DIR/kind-cluster-operator"
KIND_OP_IMG=kind-cluster-operator:dev
KIND_OP_NS=kind-cluster-operator-system
KIND_OP_DEPLOY=kind-cluster-operator-controller-manager

if [ -d "$KIND_OP_DIR" ]; then
    log "building $KIND_OP_IMG"
    docker build -t "$KIND_OP_IMG" "$KIND_OP_DIR"

    log "loading $KIND_OP_IMG into kind cluster '$CLUSTER_NAME'"
    kind load docker-image "$KIND_OP_IMG" --name "$CLUSTER_NAME"

    log "applying kind-cluster-operator manifests"
    KIND_OVERLAY="$(mktemp -d -p "$KIND_OP_DIR/config" .quickstart.XXXXXX)"
    trap 'rm -rf "$OVERLAY" "$KIND_OVERLAY"' EXIT
    cat > "$KIND_OVERLAY/kustomization.yaml" <<EOF
resources:
  - ../default
EOF
    kubectl apply -k "$KIND_OVERLAY"
    kubectl -n "$KIND_OP_NS" rollout restart "deployment/$KIND_OP_DEPLOY" >/dev/null

    log "waiting for $KIND_OP_DEPLOY to become Available"
    kubectl -n "$KIND_OP_NS" wait --for=condition=Available --timeout=180s "deployment/$KIND_OP_DEPLOY"
else
    warn "$KIND_OP_DIR not found; skipping kind-cluster-operator install"
fi

# ----------------------------------------------------------------------
# 5. Done
# ----------------------------------------------------------------------
cat <<EOF

▶ ready.
    kubectl get kubevirt -A
    kubectl get scpc,simc,kindclusters -A
    kubectl apply -k $SCRIPT_DIR/samples                # all SCPCs (gardener / kind / kubevirt)
    kubectl apply -k $SCRIPT_DIR/samples/kind           # just the kind-on-host pair
    kubectl apply -k $SCRIPT_DIR/samples/kubevirt       # just the k3s-on-kubevirt pair

  tear down:
    kind delete cluster --name $CLUSTER_NAME
EOF
