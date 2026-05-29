# samples/ubuntu

Standalone smoke test for `simple-cluster-operator-ubuntu`. Installs k3s
on a list of already-running Ubuntu hosts you reach over SSH, materialises
the cluster's kubeconfig as a Secret, and reports per-host progress in
status.

## Files

| File                         | Purpose                             |
|------------------------------|-------------------------------------|
| `sshkey-sample-prv-key.yaml` | SSH private key Secret              |
| `sshkey-sample-key.pub`      | Public key                          |
| `ubuntu-cluster-sample.yaml` | The CR (placeholder host addresses) |
| `kustomization.yaml`         | Applies the CR                      |


## Prerequisites

1. Operator `simple-cluster-operator-ubuntu` deployed in the cluster
2. A few Ubuntu hosts you can SSH into as a **sudo-capable** user with
   key auth (no password prompts — operator runs `sudo -n`). 
   You can use ssh key from this directory for presentation purpose.

## Run

1. Edit `ubuntu-cluster-sample.yaml` add workers and control-plane hosts.
2. Make sure that `sshkey-sample-prv-key.yaml` allows to access configured Ubuntus.

### Apply.
kubectl apply -k .

### Watch.
kubectl get ubuntucluster -w
kubectl describe ubuntucluster.spec ubuntu-cluster-sample

### Verify

```sh
kubectl get secret ubuntu-cluster-sample-kubeconfig -o jsonpath='{.data.kubeconfig}' | base64 -d > /tmp/ubuntu-cluster-sample-kubeconfig.yaml
kubectl --kubeconfig /tmp/ubuntu-cluster-sample-kubeconfig.yaml get nodes
```

## Cleanup

```sh
kubectl delete -k .
```
The operator's finalizer runs `k3s-uninstall.sh` on every CP host and
`k3s-agent-uninstall.sh` on every worker before letting the CR disappear.
The kubeconfig Secret is owned by the CR and cascade-deletes.

