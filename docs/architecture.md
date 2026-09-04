# Architecture

Simple Cluster turns a small, uniform cluster request into whatever concrete
resource a chosen provider actually needs, on this cluster or a remote one, and
syncs the resulting cluster's kubeconfig back to the requester.

The core operator knows nothing about Gardener, OpenStack, KinD or any other
platform. Everything provider-specific lives in data — a
`SimpleClusterProviderConfig` an administrator writes — and is applied with a
dynamic client, so a provider's resource type never has to be compiled in.

## The two custom resources

Both live in API group `showroom.apeirora.eu/v1alpha1`.

| Resource | Scope | Written by | Purpose |
|---|---|---|---|
| `SimpleClusterProviderConfig` (`scpc`) | namespaced | administrator | *How* to build a cluster on one platform: the resources to create, where to create them, and how to get the finished cluster's kubeconfig back |
| `SimpleCluster` (`simc`) | namespaced | user | *What* cluster is wanted: a worker pool sizing, a per-node resource ask, and a list of acceptable provider configs |

A `SimpleCluster` resolves its `providerConfigRefs` in its own namespace first
and then in the operator's fallback namespace (`--scpc-fallback-namespace`), so
an administrator can publish a shared catalogue centrally while still allowing
per-tenant overrides.

## From request to cluster

```
   user                     management cluster                      target cluster
                   ┌───────────────────────────────────┐   ┌──────────────────────────┐
 ┌──────────────┐  │  ┌─────────────────────────────┐  │   │                          │
 │ SimpleCluster│──┼─►│ simple-cluster-operator     │  │   │  ┌────────────────────┐  │
 │  workers     │  │  │                             │  │   │  │ resource R         │  │
 │  providerRefs│  │  │ 1. select an SCPC, lock it  │  │   │  │ (Shoot, KindCluster│  │
 │  inputs      │  │  │ 2. render its resource DAG  │──┼──►│  │  OpenStackVM, …)   │  │
 └──────────────┘  │  │ 3. apply with a dynamic     │  │   │  └─────────┬──────────┘  │
        ▲          │  │    client                   │  │   │            │ provider    │
        │          │  │ 4. watch readiness          │◄─┼───┤            ▼ operator    │
        │          │  │ 5. fetch NC's kubeconfig    │  │   │  ┌────────────────────┐  │
        │          │  └──────────────┬──────────────┘  │   │  │ new cluster NC     │  │
        │          │                 │                 │   │  └────────────────────┘  │
 ┌──────┴───────┐  │                 ▼                 │   └──────────────────────────┘
 │ Secret       │◄─┼──  <sc-name>-kubeconfig           │
 │  kubeconfig  │  │                                   │
 └──────────────┘  └───────────────────────────────────┘
```

1. **Select.** The operator walks `spec.providerConfigRefs` in order, takes the
   first `SimpleClusterProviderConfig` it can resolve, and atomically records its
   name, namespace and UID in status before target work. Later spec edits,
   same-name tenant overrides and same-name replacement objects cannot migrate
   or redirect a cluster that is already being built.
2. **Render.** Each entry in `scpc.spec.resources[]` is a Go `text/template`
   body — the same engine Helm uses, with [sprig](https://masterminds.github.io/sprig/)
   — evaluated against the SimpleCluster's fields, the resolved inputs, and any
   predecessor resources. See [Templating](#templating).
3. **Apply.** The rendered object is stamped with the `SimpleCluster` UID and
   exact SCPC namespace/UID, then applied with a dynamic client, either to the
   management cluster or, when the SCPC carries
   `spec.target.kubeconfigSecretRef`, to a remote cluster.
4. **Wait.** A provider operator (Gardener's gardenlet, this repository's KinD or
   OpenStack operators, an external branded provider) turns R into a real
   cluster. The operator projects R's own status onto a universal `Phase` using
   the SCPC's `readiness` block.
5. **Sync back.** Once the designated resource is Ready, the operator fetches the
   new cluster's kubeconfig using `spec.kubeconfigSource` and writes it as Secret
   `<sc-name>-kubeconfig` in the SimpleCluster's namespace. Any manifests in the
   SimpleCluster's own `spec.resources[]` are then applied *into* the new cluster,
   and its nodes are mirrored into `status.nodes`.

Immediately before every target `Create`, the operator bypasses its cache and
re-reads both the SimpleCluster and its selected SCPC. Creation stops if either
object was replaced or its spec changed, if a render-visible SimpleCluster label
changed, or if deletion, finalizer removal, or retirement is already visible at
that guard. The management and target APIs cannot make this cross-cluster check
atomic, so staged retirement is still required. Observation and reverse-order
deletion continue to use the same target contract.

## The resource DAG

`scpc.spec.resources[]` is not a list, it is a directed acyclic graph. Each entry
names its predecessors with `dependsOn`, and a resource is only rendered once all
of them report Ready. A predecessor's **live object** is then in scope for the
successor's template, so a cluster resource can read a network resource's
allocated ID:

```yaml
resources:
  - name: network
    apiVersion: networking.example.com/v1
    kind: Network
    template: |
      spec:
        cidr: {{ .Inputs.cidr }}
  - name: cluster
    dependsOn: [network]
    apiVersion: clusters.example.com/v1
    kind: Cluster
    template: |
      spec:
        networkID: {{ .Resources.network.status.networkID }}
```

Cycles, unknown names and self-references are rejected at reconcile time.

A single entry can also fan out: `repeat` is a template that must render to a
non-negative integer, and produces that many objects suffixed `-0`, `-1`, … .
Inside a repeated template, `.Index` is the current iteration and `.Prev` is the
list of objects already rendered for that entry. Siblings see a repeated
resource as a **list** under `.Resources.<name>`.

Deletion can reconstruct instances after a create-before-status crash when
`repeat` is constant or depends only on `.SC`/`.Inputs`. It does not rebuild the
predecessor context needed by a repeat count that reads `.Resources`. Cleanup
deletes recorded owned instances, then retains the SimpleCluster finalizer because
it cannot prove that no unrecorded instance exists. Providers using such a count
must provide verified manual crash cleanup before that finalizer is removed.

Invalid provisioning inputs do not prevent cleanup of identities derived only
from immutable `.SC.name`, `.SC.namespace` and `.SC.uid` fields (for example a
hashed cluster name in `.SC.namespace`). Creation still
requires the full valid input set. On input-resolution failure, cleanup accepts
only a conservatively checked immutable identity expression; `.Inputs`, mutable
SC spec/labels, whole SC/root context access, opaque aliases and named templates
are not evaluated with missing
or substituted values. This exceptional path allows only `printf`, `sha256sum`,
`trunc`, `len`, boolean operators and comparisons; other functions (including
randomness, clock reads and external lookups) fail closed. Normal rendering with
valid inputs is unchanged. Explicitly recorded owned targets can still be deleted
when reconstruction fails, but the finalizer remains with `Error=CleanupBlocked`
until the missing identities are resolved or independently verified. Empty
resource status is never treated as proof that no target was created.

This exceptional path rejects worker-based repeat counts as well as mutable
name/namespace expressions: the current SC cannot recover values from before an
edit. Recorded targets still clean up, but uncertain reconstruction holds the
finalizer. Reconstruction also assumes that the provider's identity contract has
not changed: an SCPC UID lock does not freeze its template/spec. Arbitrary
historical SCPC changes or mutable identities on the normal valid-input path
remain a separate identity-journaling limitation; they are not proven safe by
this input-error fallback.

## Templating

Templates are Go `text/template` plus sprig, rendered with `missingkey=error` so
a typo like `.Inputs.regoin` fails loudly instead of producing an empty string.
Sprig's `env` and `expandenv` are removed — they would expose the operator pod's
environment to template authors in a multi-tenant cluster.

The render context:

| Path | Value |
|---|---|
| `.SC.name`, `.SC.namespace`, `.SC.uid`, `.SC.labels` | the SimpleCluster's identity and labels |
| `.SC.workers.nodes` | effective floor of the worker pool — `autoscaling.min` when autoscaling, else the fixed `nodes` |
| `.SC.workers.min`, `.SC.workers.max` | the pool bounds; `max` equals `min` for a fixed pool |
| `.SC.workers.autoscaling` | `true` when the request asked for a range |
| `.SC.workers.requested.cpu`, `.memory` | the per-worker resource ask, stringified |
| `.SC.controlPlane.nodes`, `.SC.controlPlane.requested.{cpu,memory}` | the control-plane pool; defaults to one node mirroring the worker ask when the SimpleCluster omits it |
| `.Inputs.<key>` | the resolved value of every input the SCPC declares — never missing, never unknown |
| `.Target.name`, `.Target.namespace`, `.Target.kind` | identity computed for the resource currently being rendered |
| `.Resources.<name>` | the live object of a predecessor named in `dependsOn`, re-read every reconcile |
| `.Index`, `.Prev` | iteration number and objects so far, inside a `repeat`ed resource |

After rendering, the operator forces `apiVersion`, `kind`, `metadata.name` and
`metadata.namespace` to the values the SCPC declared — a template cannot retarget
itself — and stamps ownership annotations.

## Inputs and profiles

`scpc.spec.inputs[]` is a **closed set**: a SimpleCluster may override exactly the
keys the SCPC declares and no others. Each declaration carries a type
(`string`, `int`, `bool`, `duration`, `enum`), a default, and documentation. Both
the default and any override are parsed against the declared type, so a bad value
is a clear error naming the key rather than a mangled manifest.

An input declared `from: profile` is resolved differently: instead of a default,
it resolves to the name of the **smallest** entry in `scpc.spec.profiles[]` whose
CPU and memory both satisfy the request. That is how an abstract "8 CPU, 32 GiB
per node" ask becomes a concrete `m5.2xlarge`. When no profile is big enough the
SimpleCluster parks in `Phase=NoHardware` and creates nothing, rather than
provisioning something the user did not ask for.

## Kubeconfig retrieval

`scpc.spec.kubeconfigSource` names the resource whose cluster is the deliverable
and picks one of three retrieval strategies:

| Strategy | Use when |
|---|---|
| `secretRef` | the platform writes the kubeconfig into a Secret (Cluster API, Gardener seed secrets). Name, namespace, and optional exact required label/annotation values are templates, with the live resource in scope. The operator requests only `meta.k8s.io/v1 PartialObjectMetadata` (no full-object fallback), verifies response TypeMeta before exposing bytes to the decoder, and checks the requirements. When at least one required label exists, the data read is a server-filtered list constrained by namespace, `metadata.name`, and every rendered label; the sole result must keep the metadata-read UID and resourceVersion. Kubernetes cannot server-filter by annotation. For backward compatibility, a config with no required label uses a name-only full GET: the metadata and UID/resourceVersion checks prevent credential use, but not transfer of a raced object's bytes. The target API server must support PartialObjectMetadata negotiation. Cross-controller transports should configure a protected required label or supply a stronger claim relation; matching user-writable metadata is not issuer authentication. |
| `subResource` | the platform issues credentials on request (Gardener's `/adminkubeconfig`). Supports `validity` and `renewalBefore`, and the operator re-issues before expiry. |
| `inline` | the kubeconfig is a field of the resource itself; a JSONPath locates it. |

The result is written to Secret `<sc-name>-kubeconfig` in the SimpleCluster's
namespace, and mirrored into `status.kubeconfig` so a portal detail view that can
only read the SimpleCluster itself can still offer a download.

## Platform Mesh integration

The stack surfaces in the Platform Mesh portal without the core operator knowing
about any specific provider:

- **`simple-cluster-pm-integration`** publishes a KCP `APIExport`
  (`simpleclusters.showroom.apeirora.eu`), a `ContentConfiguration` for the Luigi
  micro-frontend, and a marketplace `ProviderMetadata` tile.
- **`simple-cluster-sync-agent`** runs an api-syncagent `PublishedResource` that
  projects `SimpleCluster` between KCP and the service cluster and mirrors the
  kubeconfig Secret back into the account workspace.
- **`bind-watcher-operator`** watches KCP `APIBinding`s through APIExport virtual
  workspaces and annotates each account namespace with the bound export names
  under `bind.simple-cluster.apeirora.eu/exports`. Bind-only providers read that
  annotation to emit their SCPC into an account; the UI additionally discovers
  installed providers from the workspace GraphQL schema, with a static fallback
  list.

## Cluster extensions

A provisioned cluster can have extensions installed on it, and the create dialog
offers one checkbox per extension the landscape has. Neither this repository nor
the UI knows which extensions exist — **each extension announces itself**, and the
operator aggregates those announcements into one document the dialog reads.
Adding an extension is installing its chart; nothing here changes.

```
    extension's chart                 simple-cluster-operator            UI pod
 ┌────────────────────────┐        ┌────────────────────────┐    ┌────────────────────┐
 │ ConfigMap kube-system/ │ list   │ AddonCatalog runnable  │    │ /addons/simple-    │
 │ cluster-addon-<name>   │──────► │ every 2m, uncached     │───►│ cluster-addons.json│
 │ label: …/cluster-addon │        │ → ConfigMap            │ mnt│ (directory mount)  │
 └────────────────────────┘        │   simple-cluster-addons│    └─────────┬──────────┘
                                   └────────────────────────┘              │ fetch
                                                                           ▼
                                                            create dialog: one checkbox
                                                            per entry; ticking it adds
                                                            the entry's label to the new
                                                            SimpleCluster
```

**1. An extension publishes a catalogue entry** — a ConfigMap in `kube-system`
labelled `showroom.apeirora.eu/cluster-addon: "true"`, with the entry under
`addon.yaml`:

```yaml
id: crypto-broker                                # addon id
label: showroom.apeirora.eu/crypto-broker        # setting this on a cluster installs it
displayName: Crypto Broker
description: Key management and crypto services, installed on the cluster.
bindName: crypto-broker                          # APIBinding an entitled account has
entitlement:                                     # fallback: a Kind only that account sees
  group: crypto-broker.apeirora.eu
  version: v1alpha1
  listField: CryptoBrokers
  namespaced: true
targetKinds:                                     # where the checkbox may appear
  - {apiVersion: showroom.apeirora.eu/v1alpha1, kind: SimpleCluster}
```

`id` and `label` are required; everything else is optional. An entry missing
either is skipped with a log line — one broken extension does not cost the others
their checkbox. The key set is **closed**: a key an extension invents is dropped,
so growing the contract means a field in `CatalogEntry` and a release of this
operator.

**2. The operator aggregates.** `AddonCatalog` is a leader-elected runnable that
lists those ConfigMaps and writes their union into `simple-cluster-addons`,
entries sorted by `id` so an unchanged catalogue is not rewritten. It **polls
rather than watches**, deliberately: watching would put ConfigMaps into the
manager cache, and this operator creates ConfigMaps as ordinary downstream
resources — a label-filtered cache would silently stop returning those, and an
unfiltered one would hold every ConfigMap in the cluster.

**3. The UI chart mounts the result** at `/usr/share/nginx/html/addons` as a
directory mount **without `subPath`** — a subPath mount is a copy taken at
start-up and would never pick up a newly installed extension.

**4. The dialog offers and writes.** The UI keeps the entries whose `targetKinds`
include the kind being created and asks KCP whether the account is entitled to
each. Unentitled entries are rendered **disabled with the reason as a tooltip**
rather than hidden, so "why is this not offered?" has an answer on screen.
Ticking a box adds the entry's `label` to the new SimpleCluster — that is the
entire install action.

Entitlement is asked two ways, and the order matters: APIBindings are listed once
for all extensions and matched against `bindName` exactly or by prefix (kcp
appends a generated suffix). A match is conclusive; a miss falls through to a
per-extension probe of the `entitlement` Kind, and only both saying no makes it a
no. Unlike provider discovery this **fails closed**: an inconclusive answer leaves
the box disabled, because an offered checkbox is a promise.

## Components

| Component | Kind it owns | Role |
|---|---|---|
| `simple-cluster-operator` | `SimpleCluster`, `SimpleClusterProviderConfig` | the core reconciler described above |
| `simple-cluster-operator-kind` | `KindCluster` (`kind.apeirora.eu`) | KinD clusters via Docker-out-of-Docker, for local demos |
| `simple-cluster-operator-openstack` | `OpenStackVM` (`cloudferro.eu`) | OpenStack VMs from an application credential |
| `simple-cluster-operator-ubuntu` | `UbuntuCluster` (`ubuntu.apeirora.eu`) | k3s over SSH on existing Ubuntu hosts |
| `sshkey-operator` | `SSHKey` (`sshkeys.apeirora.eu`) | generates and stores SSH keypairs used by the VM providers |
| `bind-watcher-operator` | — | KCP bind discovery, annotates account namespaces |
| `ubuntu-machines` | `UbuntuMachine`, `UbuntuMachineClaim` (`ubuntumachines.apeirora.eu`) | a pool of pre-existing SSH hosts that can be claimed |
| `ui` | — | React + UI5 micro-frontend for the portal |

Branded cloud providers (Dati, SAP, Arsys, CloudFerro) live in their own
repositories and plug in through the same extension points — see the
[provider contract](provider-contract.md).

## Further reading

- [API reference](api-reference.md) — every field of both CRDs
- [Provider contract](provider-contract.md) — what a provider must supply
- [Provider implementation guide](provider-guide.md) — how to build one
- [Operations](operations.md) — running the stack
- [Troubleshooting](troubleshooting.md) — when it does not work
