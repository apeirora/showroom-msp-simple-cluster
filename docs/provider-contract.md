# Provider contract

A *provider* is anything that can turn a `SimpleCluster` into a real cluster.
This page is the contract: everything a provider must supply, and everything the
core operator promises in return. It is deliberately small — most of it is data,
not code.

Providers come in two shapes, and both satisfy the same contract:

| Shape | What it is | Example |
|---|---|---|
| **Config-only** | A `SimpleClusterProviderConfig` that templates a resource type some *other* controller already reconciles. No new code at all. | A Gardener `Shoot` on an existing Gardener landscape |
| **Config + operator** | An SCPC plus a controller in this or another repository that reconciles the resource the SCPC creates. | `KindCluster`, `OpenStackVM`, `UbuntuCluster` |

## What a provider must supply

### 1. A `SimpleClusterProviderConfig`

This is the whole contract for a config-only provider. It must declare:

- **`spec.target`** — where the resources are created. Omit
  `kubeconfigSecretRef` for the management cluster; supply a Secret reference to
  build on a remote cluster.
- **`spec.resources[]`** — at least one resource, each with `apiVersion`, `kind`
  and a `template`. The resource type does not need to be known to the core
  operator: it is applied with a dynamic client.
- **`spec.kubeconfigSource`** — how to obtain the finished cluster's kubeconfig.
  This is the deliverable; a provider that cannot produce a kubeconfig is not a
  provider.

and should declare:

- **`spec.inputs[]`** — the closed set of knobs a user may turn. Anything not
  declared here cannot be set, which is what makes the SCPC safe to expose.
- **`spec.profiles[]`** — the machine types offered, so an abstract resource ask
  resolves to a concrete machine type via a `from: profile` input.
- **`spec.autoscaling`** — `true` only if the templates honour
  `.SC.workers.{min,max}` *and* the platform actually scales the pool. Claiming
  it falsely means users get a fixed pool while the status says otherwise.

### 2. Readiness that the operator can read

For each resource, either:

- follow the **default convention** — expose `status.phase` with the values
  `Ready`, `Failed`/`Error`, or empty while pending; or
- declare a **`readiness` block** mapping the resource's own status onto the
  universal phase:

  ```yaml
  readiness:
    phasePath: status.lastOperation.state
    phaseReadyValue: Succeeded
    phaseFailedValues: [Failed, Error]
    lastOperationPath: status.lastOperation.description
    lastErrorPath: status.lastError.description
  ```

`lastOperationPath` and `lastErrorPath` are optional but strongly recommended:
they are what a user sees in `kubectl describe simc` while waiting, and their
absence is the difference between "Provisioning" and "Provisioning, image
download 40%".

### 3. Correct deletion behaviour

- If the controller requires a confirmation to delete, declare it in
  `deletionAnnotations` — e.g. Gardener's
  `confirmation.gardener.cloud/deletion: "true"`.
- Set `forceCleanupOnCascade: true` **only** for resources with no external state
  worth reclaiming, whose graceful teardown depends on infrastructure that is
  disappearing in the same cascade. A k3s uninstall over SSH against VMs that are
  being deleted qualifies. A Gardener `Shoot` or an `OpenStackVM` does not —
  setting it there leaks real infrastructure.

### 4. For a provider with its own operator

The controller must:

- **Own its resource type** and reconcile it to a working cluster.
- **Publish a phase** its SCPC's `readiness` block can read (see above).
- **Make the kubeconfig reachable** by one of the three retrieval strategies —
  a Secret, a subresource, or a field on the resource itself.
- **Honour deletion**: remove the underlying infrastructure and drop its
  finalizers, so the `SimpleCluster` cascade can complete.
- **Not depend on the core operator.** The dependency runs one way. The core
  operator applies a manifest and reads a status; it never calls a provider API.

## What the core operator guarantees

- **The resource type is never compiled in.** Anything with an `apiVersion` and a
  `kind` that the target cluster serves can be created.
- **Inputs are validated before rendering.** Unknown keys are rejected; declared
  keys are parsed against their declared type; nothing reaches a template
  half-resolved. `missingkey=error` means a template typo fails loudly.
- **Names and namespaces are enforced, not suggested.** After rendering, the
  operator forces `apiVersion`, `kind`, `metadata.name` and `metadata.namespace`
  to the declared values. A template cannot retarget itself into another
  namespace or another kind.
- **The SCPC choice is locked** at first reconcile. Editing a `SimpleCluster`
  does not migrate a cluster that is already building.
- **Predecessors are live.** A `dependsOn` predecessor is re-read from the target
  cluster on every reconcile, so `.Resources.<name>.status.…` always reflects
  current state.
- **Ownership is stamped.** Every created resource carries annotations naming the
  owning `SimpleCluster` and the SCPC that produced it.
- **`env` and `expandenv` are unavailable** in templates. The operator pod's
  environment is not template-author-reachable.

## Portal integration (optional)

A provider that should appear in the Platform Mesh portal additionally ships:

| Chart | Contents |
|---|---|
| `<provider>-pm-integration` | KCP `APIExport`, `ProviderMetadata` marketplace tile, `ContentConfiguration` for the micro-frontend |
| `<provider>-sync-agent` | api-syncagent `PublishedResource` definitions projecting the provider's resources between KCP and the service cluster |
| `<provider>-ui` | The provider's own micro-frontend, when it needs one |

Bind-only providers — those with no operator of their own — emit their SCPC into
an account namespace by reacting to the
`bind.simple-cluster.apeirora.eu/exports` annotation that `bind-watcher-operator`
maintains.

## Providers in this repository

| SCPC | Mechanism | Resources created | Autoscaling |
|---|---|---|---|
| `kind` | cluster | `KindCluster` | no |
| `ubuntu-machines` | VM → cluster | `UbuntuMachineClaim` (from a pre-provisioned pool) → `UbuntuCluster` | no |
| `openstack` | VM → cluster | `SSHKey` + `OpenStackVM` → `UbuntuCluster` | no |
| `gardener` samples | cluster | Gardener `Shoot` + `AdminKubeconfigRequest` | yes |

Only Gardener-backed configurations set `autoscaling: true` — Gardener scales the
worker pool natively through `minimum`/`maximum` on the Shoot's worker pool.
Everything else provisions `workers.min`.

Branded cloud providers (Dati, SAP, Arsys, CloudFerro) live in their own
repositories and consume the shared building-block operators from this one.

## Next

[Provider implementation guide](provider-guide.md) walks through building one.
