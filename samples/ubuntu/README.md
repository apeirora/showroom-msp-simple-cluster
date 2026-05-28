# samples/ubuntu

Standalone smoke test for `simple-cluster-operator-ubuntu`. Installs k3s
on a list of already-running Ubuntu hosts you reach over SSH, materialises
the cluster's kubeconfig as a Secret, and reports per-host progress in
status.

## Files

| File | Purpose |
| --- | --- |
| `ubuntucluster.yaml` | The CR (placeholder host addresses) |
| `sshkey.example.yaml` | Template for the SSH key Secret |
| `kustomization.yaml` | Applies the CR only (Secret stays out of git) |
| `.gitignore` | Keeps a filled `sshkey.yaml` out of the repo |

## Prerequisites

1. Operator deployed in the cluster:
   ```sh
   cd simple-cluster-operator-ubuntu/
   make docker-build
   kind load docker-image simple-cluster-operator-ubuntu:dev --name <kind-cluster>
   make deploy
   ```
2. A few Ubuntu hosts you can SSH into as a **sudo-capable** user with
   key auth (no password prompts — operator runs `sudo -n`).

## Run

```sh
# 1. SSH key Secret. Easiest path:
kubectl create secret generic ubuntu-ssh \
  --type=kubernetes.io/ssh-auth \
  --from-file=ssh-privatekey=$HOME/.ssh/id_ed25519

# Or use the declarative template:
# cp sshkey.example.yaml sshkey.yaml && $EDITOR sshkey.yaml && kubectl apply -f sshkey.yaml

# 2. Fill in spec.controlPlane.hosts[].address and spec.workers.hosts[].address.
$EDITOR ubuntucluster.yaml

# 3. Apply.
kubectl apply -k .

# 4. Watch.
kubectl get ubuc -w
kubectl describe ubuc/demo
```

## Verify

```sh
kubectl get secret demo-kubeconfig -o jsonpath='{.data.kubeconfig}' | base64 -d > /tmp/k3s.yaml
KUBECONFIG=/tmp/k3s.yaml kubectl get nodes
```

## Tear down

```sh
kubectl delete -k .
kubectl delete secret ubuntu-ssh
```

The operator's finalizer runs `k3s-uninstall.sh` on every CP host and
`k3s-agent-uninstall.sh` on every worker before letting the CR disappear.
The kubeconfig Secret is owned by the CR and cascade-deletes.

## Chaining with simple-cluster-operator-openstack

When the VMs come from `OpenStackVMSet`, point
`spec.ssh.privateKeySecretRef` at the openstack operator's
`<vmset>-ssh` Secret directly — no copying required. The eventual
`simple-cluster-operator` SCPC will wire this through automatically.
