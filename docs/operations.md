# Operations

Running the stack day to day. For problems, see
[Troubleshooting](troubleshooting.md).

## What runs where

Everything in this repository runs on the **management cluster** — the cluster
holding the `SimpleCluster` and `SimpleClusterProviderConfig` resources. The
clusters it provisions are elsewhere, and the operator reaches them only through
the kubeconfigs it synced.

| Workload | Namespace (default) | Leader-elected |
|---|---|---|
| `simple-cluster-operator` | `simple-cluster-system` | yes |
| provider operators (KinD, OpenStack, Ubuntu, SSH key) | `simple-cluster-system` | yes |
| `ubuntu-machines` | its own release namespace | yes |
| `bind-watcher-operator` | its own release namespace | yes |
| `simple-cluster-ui` | its own release namespace | n/a (stateless nginx) |

## Publishing a provider config

An administrator creates `SimpleClusterProviderConfig` resources; users never do.
Publish centrally into the fallback namespace
(`--scpc-fallback-namespace`) so every tenant can reference them, or into a
specific namespace to scope a configuration to one tenant.

Check a new config before announcing it:

```sh
kubectl get scpc my-provider -o jsonpath='{.status}' | jq
```

- `reachable: true` — the target cluster's credentials work. Absent means the
  target is the local cluster.
- `resources[]` — one entry per templated resource with its resolved
  GroupVersionKind. A missing entry means the target cluster does not serve that
  type, and every `SimpleCluster` using this config will fail.

## Retiring a provider config

Set `spec.provisioningEnabled: false` before unpublishing or removing a provider.
The operator checks this directly from the API server immediately before each
target create, while continuing to observe and delete existing target resources.
Retired configurations are also reported unavailable in the ORD catalogue.
Because that check and a remote create cannot be one atomic transaction, wait
for controllers to observe retirement before removing any supporting access.

If a retired config is missing a target, the `SimpleCluster` moves to `Failed`
with `Ready=False` and reason `ProvisioningDisabled`; a stale `Ready=True` is
never preserved. The chosen SCPC's name, namespace and UID are persisted before
any target work. Cleanup uses both observed status identities and deterministically
reconstructed identities after a create-before-status crash. Every new candidate
must carry the SimpleCluster name/UID and exact SCPC name/namespace/UID before it
can be deleted, so an unrelated same-name object is left untouched and cleanup
keeps retrying. The allow-delete patch and Delete request are guarded by the
observed target UID (and the patch also by resourceVersion), so a replacement
racing cleanup is not modified or removed.

Crash reconstruction is complete for non-repeated resources and for `repeat`
expressions derived only from `.SC` and `.Inputs`. Deletion does not rebuild the
predecessor context for a `repeat` expression that reads `.Resources`. It deletes
any recorded owned instances but cannot prove that an interrupted reconcile left
no additional unrecorded instance, so it fails closed and retains the
SimpleCluster finalizer. Verify and remove every owned target manually before
manually removing that finalizer.

Targets created by older releases have only name-based ownership annotations.
The operator adds immutable identity stamps only when its status already records
the exact resource/GVK/name/namespace, then re-reads the object on the next
reconcile. Empty or mismatched legacy status GVK fails closed.
An unrecorded legacy create-before-status target cannot be safely distinguished
from a same-name collision; verify and migrate or remove it manually.

Keep the SCPC itself—and its name, target connection, resource names, and deletion
annotations—unchanged until every dependent SimpleCluster has completed its
deletion. If the replacement contract differs, publish it under a new SCPC name.
During an upgrade from a release that recorded only `providerConfigRef.name`, keep
all referenced SCPCs in place until the new controller has backfilled namespace
and UID. With no historical UID, a lone current candidate cannot prove that it was
not replaced before the upgrade; verify that case manually.

## Watching a cluster come up

```sh
kubectl get simc -A
kubectl describe simc demo -n team-a
```

`kubectl get simc` prints the chosen provider config, control-plane and worker
readiness as `<ready>/<declared>`, the phase, the kubeconfig Secret name and its
expiry.

`status.resources[]` is the per-resource detail: `reason` says what the operator
is waiting for (`WaitingForDependencies`, `Creating`, `Provisioning`, `Ready`,
`Failed`, `Deleting`) and `message` names the blockers. `lastOperation` and
`lastError` are mirrored from the provider's own resource, so a Gardener Shoot's
progress text shows up here without anyone touching Gardener.

## Handing out a kubeconfig

`Ready` covers the provisioning DAG only; the kubeconfig is fetched afterwards.
Wait for `KubeconfigValid` — or for `status.kubeconfigSecretRef` to appear —
rather than for `Ready`:

```sh
kubectl wait --for=condition=KubeconfigValid simc/demo -n team-a --timeout=15m
```

```sh
SECRET=$(kubectl get simc demo -n team-a -o jsonpath='{.status.kubeconfigSecretRef.name}')
kubectl get secret "$SECRET" -n team-a -o jsonpath='{.data.kubeconfig}' | base64 -d > demo.kubeconfig
```

The same bytes are mirrored into `status.kubeconfig` so the portal detail view
can offer a download. Anyone who can `get` the `SimpleCluster` can read it —
which is the same audience that can already read the Secret in that namespace.
If that is not the boundary you want, restrict `get` on `simpleclusters`.

## Credential rotation

For providers using the `subResource` strategy the operator issues short-lived
credentials and re-issues them before expiry. Tune per config:

```yaml
kubeconfigSource:
  validity: 24h
  renewalBefore: 6h
```

or per cluster, with `spec.kubeconfigValidity` and
`spec.kubeconfigRenewalBefore`. `status.kubeconfigExpiresAt` and the
`KubeconfigValid` condition show the current window.

The `secretRef` and `inline` strategies deliver whatever credential the platform
issued; their lifetime is the platform's business, not this operator's.

## Deleting a cluster

Delete the `SimpleCluster`. The operator sets `Phase=Deleting`, applies each
resource's `deletionAnnotations`, deletes the DAG, and only drops its finalizer
once everything is gone.

```sh
kubectl delete simc demo -n team-a
kubectl get simc demo -n team-a -o jsonpath='{.status.resources}' | jq
```

A delete that does not complete is almost always a provider resource whose own
controller is stuck — see
[Troubleshooting § A cluster will not delete](troubleshooting.md#a-cluster-will-not-delete).

## Capacity and profiles

A `SimpleCluster` asking for more than any profile offers parks in
`Phase=NoHardware` and creates nothing. That is a deliberate refusal, not an
error: adding a bigger entry to `spec.profiles` (and confirming the platform
really offers that machine type) unparks every waiting request on the next
reconcile.

## Scaling

Providers that declare `autoscaling: true` honour `.SC.workers.{min,max}` and let
the platform scale within the range. Everything else provisions `workers.min`,
and the `AutoscalingSupported` condition on the `SimpleCluster` says so. Changing
a worker count on an existing cluster re-renders and re-applies the resource; the
provider decides whether that is an in-place change.

## Metrics and logs

Each operator exposes the standard controller-runtime metrics endpoint when
`metrics.enable` is set in its chart values. Logs are structured; reconcile
errors carry the `SimpleCluster` name and namespace.

## Upgrading

Images and charts ship in lockstep at one version. Upgrade every chart to the
same version together; skipping one leaves an operator reconciling CRDs it does
not fully understand. See [Release flow](release-flow.md).

## Backups

The operator holds no state of its own. What matters is:

- the `SimpleCluster` and `SimpleClusterProviderConfig` resources;
- the kubeconfig Secrets in tenant namespaces, which cannot be regenerated for
  providers whose credential is not re-issuable;
- the target-cluster credential Secrets referenced by
  `spec.target.kubeconfigSecretRef`.

Non-repeated target identities, and repeated identities whose count depends only
on `.SC` or `.Inputs`, are reconstructed by reconciliation. Repeats whose count
depends on predecessor `.Resources` require verified manual crash cleanup; see
[Retiring a provider config](#retiring-a-provider-config).
