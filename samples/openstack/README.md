# samples/openstack

`SimpleCluster` -> `openstack-sample` SCPC -> N `OpenStackVM`s ->
`UbuntuCluster` -> kubeconfig Secret synced to `SimpleCluster`.

Target cloud is **CloudFerro WAW4-1**.

## Prerequisites

1. **OpenStack credentials Secret** (`openstack-creds-secret.yaml`)
   carrying `OS_AUTH_URL`, `OS_REGION_NAME`,
   `OS_APPLICATION_CREDENTIAL_ID`, `OS_APPLICATION_CREDENTIAL_SECRET`.
   Hardcoded in the SCPC as `credentialsRef: { name: openstack-creds }`.

2. **Shared SSH keypair Secret** (`kubernetes.io/ssh-auth`) named
   `openstack-sample-ssh` with both `ssh-privatekey` and `ssh-publickey`
   keys.

   Generate one with:
   ```sh
   ssh-keygen -t ed25519 -N '' -f /tmp/openstack-sample-ssh
   kubectl create secret generic openstack-sample-ssh \
     --type=kubernetes.io/ssh-auth \
     --from-file=ssh-privatekey=/tmp/openstack-sample-ssh \
     --from-file=ssh-publickey=/tmp/openstack-sample-ssh.pub
   ```

3. **Paste the public key into `scpc-openstack.yaml`**. cloud-init writes
   it into each VM's default user (`eouser`) authorized_keys; the
   UbuntuCluster operator uses the matching private key from the Secret
   above to SSH in. Look for the `REPLACE_WITH_openstack-sample-ssh_PUBLIC_KEY`
   marker in the `cp` and `worker` templates.

## Flow

`simplecluster.yaml` declares `spec.controlPlane.{nodes,requested}` and
`spec.workers.{nodes,requested}`. `scpc-openstack.yaml`:
- fans out `cp` (one OpenStackVM per CP node, each with a floating IP),
- fans out `worker` (one OpenStackVM per worker, no floating IP),
- once both groups are Ready, renders one `UbuntuCluster` collecting all
  VMs' addresses, which installs k3s and writes its kubeconfig to
  `<cluster-target-name>-kubeconfig` — that Secret is mirrored into
  `SimpleCluster.status.kubeconfigSecretRef`.

## Apply

```sh
kubectl apply -k samples/openstack
```

## Standalone OpenStackVM smoke test

`single-vm.yaml` is independent of the SimpleCluster flow — apply it
on its own to exercise just `simple-cluster-operator-openstack`:

```sh
kubectl apply -f samples/openstack/openstack-creds-secret.yaml
kubectl apply -f samples/openstack/vm-instances.yaml
```
