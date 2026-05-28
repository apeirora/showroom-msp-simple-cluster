# samples/cloudferro-ubuntu

End-to-end: one `SimpleCluster` CR triggers OpenStack VM provisioning on
CloudFerro and then k3s installation on those VMs over SSH.

```
SimpleCluster (you)
   ↓ simple-cluster-operator renders SCPC
   ├── OpenStackVMSet              (simple-cluster-operator-openstack: Nova VMs + FIPs + SSH keypair)
   └── UbuntuCluster (depends on)  (simple-cluster-operator-ubuntu: k3s on top via SSH)
        ↓
        <cluster>-kubeconfig Secret  → mirrored into SC.namespace as `cloudferro-demo-kubeconfig`
```

## Prerequisites

```sh
# 1. Three operators running on the management cluster (KinD here).
make -C simple-cluster-operator deploy
make -C simple-cluster-operator-openstack deploy
make -C simple-cluster-operator-ubuntu deploy

# 2. CloudFerro credentials in the SC's namespace.
cd samples/openstack && ./create-secret.sh   # creates `cloudferro-creds`
```

## Run

```sh
kubectl apply -k samples/cloudferro-ubuntu/

kubectl get simc -w
kubectl get osvms,ubuc
```

Expected progression:
- SC `cloudferro-demo`: Pending → Provisioning → Ready
- OpenStackVMSet `cloudferro-demo`: Init → Provisioning → Ready (2 VMs, 2 FIPs)
- UbuntuCluster `cloudferro-demo`: Init → Provisioning (per-host Installing) → Ready

## Verify

```sh
kubectl get secret cloudferro-demo-kubeconfig -o jsonpath='{.data.kubeconfig}' \
  | base64 -d > /tmp/k3s.yaml
KUBECONFIG=/tmp/k3s.yaml kubectl get nodes
```

## Tear down

```sh
kubectl delete -k samples/cloudferro-ubuntu/
```

Cascade order via finalizers: UbuntuCluster (k3s uninstall over SSH) →
OpenStackVMSet (delete Nova servers + release FIPs + drop keypair) →
SimpleCluster removes itself.
