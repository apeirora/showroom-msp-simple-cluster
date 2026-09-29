# OpenBao KMS encryption (optional)

Encrypts the Kubernetes Secrets of a k3s-based cluster in etcd with a key held in
[OpenBao](https://openbao.org/)'s Transit engine, through the Kubernetes
[KMS v2](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
interface. The plugin between the kube-apiserver and OpenBao is
[vault-kubernetes-kms](https://github.com/FalcoSuessgott/vault-kubernetes-kms)
(OpenBao is API-compatible with Vault).

Nothing here is required to run Simple Cluster. It is a provisional reference for
external key management; Gardener clusters are out of scope. Today a
control-plane host is prepared with the scripts below **before** k3s is
installed on it. A `SimpleCluster` opt-in that does this during provisioning is
tracked in [apeirora/showroom#470](https://github.com/apeirora/showroom/issues/470).

## How it fits together

```
 management cluster                 control-plane host                        OpenBao
┌──────────────────────┐      ┌──────────────────────────────────────┐    ┌──────────────────┐
│ Secret               │ SSH  │ /etc/vault-kms/env (0600 root)       │    │ Transit key      │
│  <cluster>-openbao-  │─────►│        │ EnvironmentFile             │    │  <cluster>       │
│  kms (connection +   │ push │        ▼                             │TLS │ AppRole role     │
│  AppRole secret ID)  │      │ vault-kubernetes-kms.service ────────┼───►│  <cluster>       │
└──────────────────────┘      │        ▲ unix socket                 │    │ policy: encrypt, │
                              │        │ KMS v2                      │    │  decrypt, read   │
                              │ k3s.service (kube-apiserver)         │    │  this key only   │
                              │   --encryption-provider-config       │    └──────────────────┘
                              └──────────────────────────────────────┘
```

The kube-apiserver encrypts each Secret with a data encryption key it derives
locally. The plugin wraps that key's seed with the Transit key, and only the
wrapped seed is stored in etcd. The Transit key never leaves OpenBao.

k3s runs the kube-apiserver inside its own process rather than as a static pod,
so the plugin runs as a systemd unit that starts before `k3s.service`.

## Prerequisites

| What | Detail |
|---|---|
| OpenBao | Reachable over HTTPS from every control-plane host. Tested with 2.5.5. A namespace is optional. |
| An OpenBao login | Once per environment: may enable a secrets engine and an auth method and write policies in the target namespace |
| Tools where you run the scripts | `bao`, `jq`, `kubectl`, `ssh` |
| Control-plane hosts | Ubuntu with systemd and `curl`, amd64 or arm64, **k3s not yet installed**. Tested with k3s v1.36.4+k3s1. |
| OpenBao backups | Must cover the Transit mount. **If the key is lost, every Secret in the cluster is unreadable.** |

## 1. Configure OpenBao

```sh
export BAO_ADDR=https://openbao.example.com BAO_NAMESPACE=my/namespace
bao login -method=oidc
hack/openbao-kms/setup-openbao.sh --cluster demo --secret team-a/demo-openbao-kms
```

The script is idempotent. It creates what is missing and never modifies an
existing key:

| Object | Default name | Settings |
|---|---|---|
| Transit mount | `simplecluster-kms/` | shared by all clusters |
| Transit key | `simplecluster-kms/keys/<cluster>` | `aes256-gcm96`, not exportable, no plaintext backup, deletion not allowed |
| Policy | `simplecluster-kms-<cluster>` | [policy.hcl.tpl](../hack/openbao-kms/policy.hcl.tpl): `update` on `encrypt/<cluster>` and `decrypt/<cluster>`, `read` on `keys/<cluster>` |
| AppRole mount | `simplecluster-kms-approle/` | shared by all clusters |
| AppRole role | `<cluster>` | periodic service token (`--token-period`, default 1h); secret ID TTL `--secret-id-ttl` (default 90 days); unlimited secret ID uses (the plugin logs in again whenever a renewal fails) |

`--bound-cidrs` limits where the secret ID and its tokens may be used from; list
every control-plane host's egress address. `--ca-cert-file` hands a CA to the
plugin when OpenBao's certificate is not publicly trusted.

A secret ID is issued only when the destination has none yet, or with
`--rotate-secret-id`. It goes straight into the Secret (or a 0600 file with
`--out-file`) and is never printed.

## 2. The connection Secret

One Secret per cluster, in the management cluster, labelled
`showroom.apeirora.eu/purpose: openbao-kms`:

| Key | Content |
|---|---|
| `address` | OpenBao URL; must be `https://` |
| `namespace` | OpenBao namespace, when used |
| `transitMount`, `transitKey` | where the key lives |
| `approleMount`, `roleID` | the AppRole identity |
| `secretID` | **the credential** |
| `secretIDAccessor` | identifies the secret ID for revocation (not a credential) |
| `caCert` | optional PEM |

Anyone who can read this Secret can log in as the cluster's plugin. Scope RBAC
accordingly.

## 3. Prepare each control-plane host

Before k3s is installed on the host:

```sh
hack/openbao-kms/push-to-host.sh --secret team-a/demo-openbao-kms --host ubuntu@10.0.0.10
```

The Secret's data travels base64-encoded over SSH into
[install.sh](../hack/openbao-kms/install.sh), which decodes it on the host as
root. It lays down the following:

| Path | Purpose |
|---|---|
| `/usr/local/bin/vault-kubernetes-kms` | the pinned plugin release, SHA-256 verified |
| `/etc/vault-kms/env` | 0600 root; read by systemd, not by the plugin's user |
| `/etc/vault-kms/encryption-config.yaml` | KMS v2 for `secrets`, no plaintext fallback |
| `/etc/systemd/system/vault-kubernetes-kms.service` | runs as an unprivileged dynamic user with systemd sandboxing; socket in `/run/vault-kms/` |
| `/etc/systemd/system/k3s.service.d/10-vault-kms.conf` | orders k3s after the plugin (`Wants`, so a plugin restart does not restart k3s) |
| `/etc/rancher/k3s/config.yaml.d/90-vault-kms.yaml` | adds `--kube-apiserver-arg=encryption-provider-config=…`; nothing to add to `k3sServerArgs` |

`install.sh` refuses to run on a host where k3s already stored data, because the
cluster's existing Secrets would become unreadable. Encrypting an existing
cluster is not supported.

## 4. Install k3s

Provision the cluster as usual. With several control-plane nodes, prepare every
one of them before it joins. They all share the key and the role.

## Verify

On a control-plane host:

```sh
kubectl get --raw='/readyz?verbose' | grep kms          # [+]kms-providers ok
curl -s http://127.0.0.1:10271/health                   # OK
kubectl create secret generic kms-check --from-literal=value=hello
```

The stored value starts with `k8s:enc:kms:v2:openbao:` and does not contain the
plaintext. Read it with `etcdctl` using k3s's etcd client certificate from
`/var/lib/rancher/k3s/server/tls/etcd/`:

```sh
etcdctl --endpoints=https://127.0.0.1:2379 \
  --cacert=/var/lib/rancher/k3s/server/tls/etcd/server-ca.crt \
  --cert=/var/lib/rancher/k3s/server/tls/etcd/client.crt \
  --key=/var/lib/rancher/k3s/server/tls/etcd/client.key \
  get /registry/secrets/default/kms-check --print-value-only | head -c 23
```

## Security model

- **TLS is always verified.** `address` must be `https://`. Without `caCert` the
  host's trust store is used.
- **Least privilege.** The role can encrypt and decrypt with its own key and read
  that key's metadata. It cannot use another cluster's key, delete, export or
  rotate its own key, or list mounts. The end-to-end test asserts each denial.
- **Credentials.** The secret ID exists only in the Secret and in
  `/etc/vault-kms/env`. It never appears in process arguments, unit files, k3s
  configuration or logs. Known limitation: the plugin logs the AppRole
  **role ID** (the identifier, not the secret) in its startup line.
- **Tokens** are periodic and renewed by the plugin. When a renewal fails, it logs
  in again with the secret ID.
- **Health and metrics** listen on port `10271` on all interfaces; the plugin
  has no bind-address option. Block the port from outside the host with a
  firewall or security group. The port is moved off upstream's `8080` to avoid
  k3s ServiceLB host ports.

## Failure behaviour

- There is no plaintext fallback. If OpenBao is unreachable when the
  kube-apiserver starts, `kms-providers` reports unhealthy, Secret writes fail
  with `got unexpected nil transformer`, and nothing is stored. Once OpenBao is
  back, the cluster recovers without intervention.
- While OpenBao hangs, the plugin logs no error and its error metrics stay at
  zero. Watch the kube-apiserver's `kms-providers` check.
- Restarting the plugin does not restart k3s.

## Rotation

**Secret ID.** Run `setup-openbao.sh --rotate-secret-id` with the same
arguments, run `push-to-host.sh` for every control-plane host, then destroy the
previous secret ID with the command the script prints. k3s keeps running.

**Transit key.** `bao write -f simplecluster-kms/keys/<cluster>/rotate` by an
administrator. The plugin reports the new key version, and OpenBao keeps the old
versions for decryption. The end-to-end test does not cover this yet.

## Testing

[e2e-local.sh](../hack/openbao-kms/e2e-local.sh) runs the whole flow in Docker.
OpenBao runs in dev-TLS mode. An Ubuntu container with systemd acts as the
control-plane host, where the scripts above and the stock k3s installer run the
way `simple-cluster-operator-ubuntu` runs them. The test checks:

- idempotent setup
- process identity and file modes
- ordering of the plugin before k3s
- ciphertext for every Secret in etcd
- the AppRole denials
- that credentials are absent from logs, unit files and process arguments
- restarts
- failing closed when OpenBao is unavailable
- the connection Secret and `push-to-host.sh`
- secret ID rotation
- the refusal to encrypt an existing cluster

It runs in CI when `hack/openbao-kms/` changes.

## Limitations

- k3s-based providers only (`UbuntuCluster`: OpenStack, IronCore VMs, Ubuntu
  machines). Gardener and KinD clusters are not covered.
- No migration or re-encryption of existing clusters.
- Host preparation is manual until the `SimpleCluster` opt-in lands.
- The plugin release is pinned by SHA-256; its Sigstore signature is not
  verified.
