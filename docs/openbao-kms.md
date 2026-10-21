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
│  kms (connection;    │ push │        ▼                             │TLS │ AppRole role     │
│  no secret ID when   │      │ vault-kubernetes-kms.service ────────┼───►│  <cluster>       │
│  wrapped delivery)   │      │        ▲ unix socket                 │    │ policy: encrypt, │
└──────────────────────┘      │        │ KMS v2                      │    │  decrypt, read   │
                              │ k3s.service (kube-apiserver)         │    │  this key only   │
                              │   --encryption-provider-config       │    └──────────────────┘
                              └──────────────────────────────────────┘
```

The kube-apiserver encrypts each Secret with a data encryption key it derives
locally. The plugin wraps that key's seed with the Transit key, and only the
wrapped seed is stored in etcd. The Transit key never leaves OpenBao.

k3s runs the kube-apiserver inside its own process rather than as a static pod,
so the plugin runs as a systemd unit that starts before `k3s.service`.

## Two ways to get there

**Through SimpleCluster (the normal way).** A `SimpleCluster` with
`spec.encryption.provider.type: OpenBao` on a provider config that lists
`encryptionProviders: [OpenBao]` renders an `UbuntuCluster` with the same
`spec.encryption`. `simple-cluster-operator-ubuntu` then does everything below by
itself, before k3s is installed on each control-plane host:

1. logs in to the platform's SimpleCluster KMS tenancy with its own projected
   ServiceAccount token (JWT auth, audience `openbao`); the tenancy (namespace,
   Transit and AppRole mounts, the JWT role and two fixed policies) is platform
   configuration owned by showroom-infra, not by this repository;
2. creates the cluster's Transit key and AppRole role, both named
   `<namespace>-<name>`, pinned by policy to the plugin's capabilities;
3. mints one response-wrapped, single-use secret ID per control-plane host and
   streams [install.sh](../hack/openbao-kms/install.sh) with the connection data to
   the host over SSH stdin; the host unwraps the secret ID itself and the operator
   records only its accessor in `status.encryption.hosts`;
4. installs k3s, then requires the plugin unit to be active and the kube-apiserver
   to report `kms-providers ok`. Only then does the host count as Ready, and only
   when every control-plane host did does the cluster, and with it the kubeconfig
   the SimpleCluster hands out.

Fail-closed: an operator without `--openbao-address`, a key that is soft-deleted,
or a host that already runs k3s without the plugin or with another cluster's plugin
configuration puts the `UbuntuCluster` in `Phase=Failed` with `Error`/`EncryptionReady`
conditions saying why. Nothing is provisioned unencrypted. Key and role are named
`<namespace>_<name>` and recorded in `status.encryption` before anything is created
in OpenBao. A reused pool host whose plugin still points at an earlier cluster is
re-prepared when k3s is not running on it. Each host's secret ID is replaced through
the same script after 75% of the role's secret ID TTL (default 90 days), the plugin
restarts, k3s does not, and the previous accessor is destroyed. On delete the
operator first destroys the hosts' secret IDs and the role, independently of SSH
access to the hosts; the Transit key stays so etcd snapshots remain restorable
(removing it is an OpenBao admin action, see [Removing a cluster](#removing-a-cluster)).

The operator is configured through the `openbao.*` values of its Helm chart
(`charts/simple-cluster-system/charts/simple-cluster-operator-ubuntu`), and the
`ubuntu-machines` provider config gains the passthrough with
`encryption.openbao: true` in `charts/simple-cluster-scpc-config`.

**By hand (what follows).** The scripts in `hack/openbao-kms` do the same steps
with your own `bao` login, for a standalone `UbuntuCluster`, a lab, or to understand
what the operator does. Steps 1 to 3 below are the manual equivalents of the
operator's steps 1 to 3.

## Prerequisites

| What | Detail |
|---|---|
| OpenBao | Reachable over HTTPS from every control-plane host. Tested with 2.5.5. A namespace is optional. |
| An OpenBao login | Once per environment: may enable a secrets engine and an auth method and write policies in the target namespace |
| Tools where you run the scripts | `bao`, `jq`, `kubectl`, `ssh` |
| Control-plane hosts | Ubuntu with systemd, `curl` and `python3`, amd64 or arm64, **k3s not yet installed**. Tested with k3s v1.36.4+k3s1. |

## What OpenBao has to provide

The Transit key is the only way to read the cluster's Secrets. That puts a few
demands on the OpenBao instance, beyond what the scripts can check:

- **A namespace nothing deletes.** Keep the Transit mount in a namespace that no
  controller manages with a delete policy (for example a Crossplane
  `VaultNamespace` with `deletionPolicy: Delete`). Deleting the namespace
  deletes every key in it.
- **Backups.** The keys are not exportable, so scheduled, encrypted raft snapshots
  stored off the cluster are the only copy. Test a restore before a cluster you
  care about depends on a key.
- **Availability.** A running kube-apiserver keeps its unwrapped seeds in memory; a
  restarting one needs OpenBao before it can read Secrets.
- **TLS to OpenBao itself.** If a gateway terminates TLS in front of OpenBao, keep
  the hop behind it encrypted too: Transit requests and responses carry
  data-key seeds.
- **Real client addresses**, only if you want `--bound-cidrs`: OpenBao must see
  the host's address, for example by trusting `X-Forwarded-For` from the gateway
  (`x_forwarded_for_authorized_addrs` on the listener).

## 1. Configure OpenBao

```sh
export BAO_ADDR=https://openbao.example.com BAO_NAMESPACE=my/namespace
bao login -method=oidc
hack/openbao-kms/setup-openbao.sh --cluster demo --secret team-a/demo-openbao-kms --no-secret-id
```

The script is idempotent. It creates what is missing and leaves an existing key
as it is, except for its rotation period when `--auto-rotate-period` is given:

| Object | Default name | Settings |
|---|---|---|
| Transit mount | `simplecluster-kms/` | shared by all clusters |
| Transit key | `simplecluster-kms/keys/<cluster>` | `aes256-gcm96`, not exportable, no plaintext backup, deletion not allowed; `--auto-rotate-period` turns on automatic rotation |
| Policy | `simplecluster-kms-<cluster>` | [policy.hcl.tpl](../hack/openbao-kms/policy.hcl.tpl): `update` on `encrypt/<cluster>` and `decrypt/<cluster>`, `read` on `keys/<cluster>` |
| AppRole mount | `simplecluster-kms-approle/` | shared by all clusters |
| AppRole role | `<cluster>` | periodic service token (`--token-period`, default 1h); secret ID TTL `--secret-id-ttl` (default 90 days); unlimited secret ID uses (the plugin logs in again whenever a renewal fails) |

Use a name for `--cluster` that is unique within the OpenBao namespace, such as
one derived from the `SimpleCluster` UID; cluster names alone are only unique
within one account.

`--bound-cidrs` limits where the secret ID and its tokens may be used from; list
every control-plane host's egress address. It only works when OpenBao sees the
client's real address (see above); otherwise every login is refused. Leaving the
flag out clears the binding, but a secret ID keeps the CIDRs it was issued with,
so rotate it after changing them. `--ca-cert-file` hands a CA to the plugin when
OpenBao's certificate is not publicly trusted.

## 2. The connection Secret

One Secret per cluster, in the management cluster, labelled
`showroom.apeirora.eu/purpose: openbao-kms`. Keep it in a namespace only the
operator can read, not in the account's namespace.

| Key | Content |
|---|---|
| `address` | OpenBao URL; must be `https://` |
| `namespace` | OpenBao namespace, when used |
| `transitMount`, `transitKey` | where the key lives |
| `approleMount`, `approleRole`, `roleID` | the AppRole identity |
| `secretID` | **the credential**; absent with `--no-secret-id` |
| `secretIDAccessor` | identifies that secret ID for revocation (not a credential) |
| `caCert` | optional PEM |

With a stored `secretID`, anyone who can read this Secret can log in as the
cluster's plugin.

## 3. Prepare each control-plane host

Before k3s is installed on the host:

```sh
hack/openbao-kms/push-to-host.sh --secret team-a/demo-openbao-kms --host ubuntu@10.0.0.10 --wrap-ttl 5m
```

With `--wrap-ttl` (recommended) the script mints a new secret ID for this host with
your `bao` login, response-wrapped: a single-use token valid for the given time.
[install.sh](../hack/openbao-kms/install.sh) unwraps it on the host and prints the
secret ID's accessor. The management cluster never stores a credential, each host
has its own secret ID, and a token that someone else unwrapped first is refused
loudly, which means the secret ID was compromised. This is the trusted-broker
pattern the `SimpleCluster` operator should follow.

Without `--wrap-ttl`, the Secret's stored `secretID` is sent instead. Either way
the data travels base64-encoded over SSH and is decoded only on the host, as
root. `install.sh` lays down:

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

[verify-host.sh](../hack/openbao-kms/verify-host.sh) checks a control-plane host
and prints one line per check; keep the output as evidence:

```sh
sudo bash verify-host.sh            # add --restart to also restart k3s once
```

It checks the plugin unit, its unprivileged user, the ordering before k3s, the
env file's mode, the plugin's health and the kube-apiserver's `kms-providers`
check. It writes a synthetic Secret and confirms it is stored with the prefix
`k8s:enc:kms:v2:openbao:`, without its plaintext, and reads back. It then checks
that every Secret in etcd has that prefix and that the secret ID appears nowhere
in the journal, unit files, k3s configuration or process list. k3s's embedded
etcd only speaks gRPC, so the script fetches a pinned, checksum-verified
`etcdctl` into a temporary directory.

## Security model

- **TLS is always verified.** `address` must be `https://`. Without `caCert` the
  host's trust store is used.
- **Least privilege.** The role can encrypt and decrypt with its own key and read
  that key's metadata. It cannot use another cluster's key, delete, export or
  rotate its own key, or list mounts. The end-to-end test asserts each denial.
- **Credentials.** With wrapped delivery the secret ID exists only in
  `/etc/vault-kms/env` on the host; otherwise also in the Secret. It never
  appears in process arguments, unit files, k3s configuration or logs. Known
  limitation: the plugin logs the AppRole **role ID** (the identifier, not the
  secret) in its startup line.
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

**Secret ID, wrapped delivery.** Run `push-to-host.sh --wrap-ttl` for the host
again, then destroy the old secret ID by its accessor:
`bao write auth/simplecluster-kms-approle/role/<cluster>/secret-id-accessor/destroy secret_id_accessor=<old>`.

**Secret ID, stored.** Run `setup-openbao.sh --rotate-secret-id` with the same
arguments, run `push-to-host.sh` for every control-plane host, then destroy the
previous secret ID with the command the script prints. k3s keeps running.

**Transit key.** Rotate with `bao write -f simplecluster-kms/keys/<cluster>/rotate`,
or let `--auto-rotate-period` do it. The plugin reports the new key version on
the kube-apiserver's next status check, and new writes use it. Old versions stay
available for decryption, so nothing else is required. To retire an old version:

```sh
kubectl get secrets -A -o json | kubectl replace -f -          # re-write every Secret
bao write simplecluster-kms/keys/<cluster>/config min_decryption_version=<new version>
```

Wait for the plugin's `v2 encryption request` log line after the rotation before
re-writing, so the Secrets are encrypted under the new version.

## Removing a cluster

```sh
hack/openbao-kms/remove-openbao.sh --cluster demo --secret team-a/demo-openbao-kms
```

This destroys every secret ID of the role, then deletes the role, the policy and
the Secret. The shared mounts stay. The Transit key stays too, unless you add
`--delete-key --confirm-key demo`. Deleting the key is irreversible: everything the
cluster ever wrote, including copies in etcd backups, becomes unreadable. Do it
only after the cluster is gone.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `install.sh` says the plugin is not running; the journal shows `source address "…" unauthorized by CIDR restrictions` | OpenBao sees a proxy's address, not the host's | Drop `--bound-cidrs`, re-run `setup-openbao.sh --rotate-secret-id` (or re-push wrapped) |
| `could not unwrap the secret ID … already used or has expired` | The wrapping token timed out, or someone else used it | Push again; if nobody here used it, destroy the role's secret IDs |
| `kms-providers` failing; Secret writes fail with `got unexpected nil transformer` | The plugin cannot reach OpenBao or log in | `systemctl status vault-kubernetes-kms`, `journalctl -u vault-kubernetes-kms`, OpenBao health |
| `k3s already has data on this host without the KMS config` | `install.sh` refuses existing clusters | Use a fresh host |
| etcd's HTTP API answers `415` | k3s's etcd serves gRPC only | Use `etcdctl`; `verify-host.sh` brings one |

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
- wrapped secret IDs, and refusal of a token someone else unwrapped
- Transit key rotation, re-encryption and retiring the old key version
- `verify-host.sh`, including a k3s restart
- the refusal to encrypt an existing cluster
- removal with `remove-openbao.sh`, after which the plugin can no longer log in

It runs in CI when `hack/openbao-kms/` changes.

## Limitations

- k3s-based providers only (`UbuntuCluster`: OpenStack, IronCore VMs, Ubuntu
  machines). Gardener and KinD clusters are not covered.
- No migration or re-encryption of existing clusters. An existing cluster is not
  onboarded in place; recreate it with the opt-in.
- Only the `ubuntu-machines` provider config carries the opt-in in this repository;
  branded providers (cloudferro, arsys, dati) live in their own repositories and need
  the same two template lines.
- The operator path is covered by unit tests (OpenBao client against a fake, host
  input encoding, embedded script identity); the end-to-end test still drives the
  scripts, not the operator.
- The plugin release is pinned by SHA-256; its Sigstore signature is not
  verified.
