# samples/ironcore

`SimpleCluster` -> `ironcore-sample` SCPC -> N `IronCoreVM`s ->
`UbuntuCluster` -> kubeconfig Secret synced to `SimpleCluster`.

IronCore creates the VMs. Once all VMs are Ready the `UbuntuCluster`
operator SSHes into them to install k3s.

## Prerequisites

1. **IronCore credentials Secret** (`ironcore-creds-secret.yaml`)
   carrying a base64-encoded kubeconfig for your IronCore API server
   under key `kubeconfig`.

2. **A Network** named `sample-net` (or override via the `network` input)
   must already exist in the IronCore cluster.

3. Optionally, an **ipam Prefix pool** (override via the `ipPool` input)
   for NIC IP allocation. Leave empty to rely on the environment default.

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

- **NIC IP allocation**: whether setting `ipPool` to a named ipam Prefix
  yields per-NIC /32 ephemeral IPs depends on the target IronCore cluster
  configuration. Leave `ipPool` empty to omit explicit IP sources and rely
  on the environment default.

- **SSH login user**: the default `sshUser: root` injects the key for the
  root account via ignition. Verify this yields an SSH-reachable account
  on the gardenlinux image used. Adjust the `sshUser` input if needed.

## Apply

```sh
kubectl apply -k samples/ironcore
```

## Standalone IronCoreVM smoke test

Apply independently to exercise just `simple-cluster-operator-ironcore`:

```sh
kubectl apply -f samples/ironcore/ironcore-creds-secret.yaml
kubectl apply -f samples/ironcore/vm-instances.yaml
```
