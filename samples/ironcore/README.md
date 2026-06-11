# samples/ironcore

`SimpleCluster` -> `ironcore-sample` SCPC -> N `IronCoreVM`s ->
`UbuntuCluster` -> kubeconfig Secret synced to `SimpleCluster`.

IronCore creates the VMs. Once all VMs are Ready the `UbuntuCluster`
operator SSHes into them to install k3s.

## Prerequisites

1. **IronCore credentials Secret** (`ironcore-kubeconfig-secret.yaml`)
   carrying a base64-encoded kubeconfig for your IronCore API server
   under key `kubeconfig`.

2. Nothing else to pre-create: the operator ensures the **Network**
   (`network` input, default `sample-net`) and a **root ipam Prefix**
   (`networkCIDR`, default `10.0.0.0/24`) exist, and allocates each NIC a
   `/32` from it. Set the `ipPool` input only to allocate from an existing
   admin-managed Prefix instead.

## Flow

`simplecluster.yaml` declares `spec.controlPlane.{nodes,requested}` and
`spec.workers.{nodes,requested}`. `scpc-ironcore.yaml`:
- fans out `vmcp` (one IronCoreVM per control-plane node, each with a public IP),
- fans out `vmworker` (one IronCoreVM per worker, no public IP),
- once both groups are Ready, renders one `UbuntuCluster` collecting all
  VMs' internal/external addresses, which installs k3s and writes its
  kubeconfig to `<cluster-target-name>-kubeconfig` — that Secret is
  mirrored into `SimpleCluster.status.kubeconfigSecretRef`.

The operator auto-generates the shared `ironcore-sample-ssh` ed25519
keypair Secret and injects the public key into each VM via an IronCore
ignition Secret.

## Caveats (validate on live cluster)

- **NIC IP allocation**: the operator creates a root Prefix (`networkCIDR`)
  and each NIC allocates an ephemeral `/32` from it. Override with `ipPool`
  to draw from an existing admin-managed Prefix. Confirm the resulting
  internal address is reachable from where the `UbuntuCluster` operator runs.

- **SSH login user**: the default `sshUser: root` injects the key for the
  root account via ignition. Verify this yields an SSH-reachable account
  on the gardenlinux image used. Adjust the `sshUser` input if needed.

## Apply

```sh
kubectl apply -k samples/ironcore
```

## Standalone IronCoreVM smoke test

Apply independently to exercise just `simple-cluster-operator-ironcore`.
`vm-instances.yaml` shows both flavor-selection forms: `ic-instance-1`
names a class via `cloudProfile`, `ic-instance-2` gives `resources`
(cpu/memory) and lets the operator pick the smallest fit.

```sh
kubectl apply -f samples/ironcore/ironcore-kubeconfig-secret.yaml
kubectl apply -f samples/ironcore/vm-instances.yaml
```
