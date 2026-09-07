# Troubleshooting

Start here:

```sh
kubectl describe simc <name> -n <namespace>
kubectl get simc <name> -n <namespace> -o jsonpath='{.status.resources}' | jq
```

`status.resources[].reason` and `.message` name the node of the DAG that is
blocking and why. `lastOperation` and `lastError` are mirrored from the
provider's own resource.

## A SimpleCluster stays in `Pending`

| Check | Meaning |
|---|---|
| `status.providerConfigRef` is empty | None of `spec.providerConfigRefs` resolved. The names are looked up in the cluster's own namespace first, then in `--scpc-fallback-namespace`. |
| `Error` condition, reason `SCPCReferenceAmbiguous` | A legacy name-only lock matches both tenant and fallback SCPCs. Remove the ambiguity only after identifying which exact config owns the targets; the operator will not guess. |
| `Error` condition, reason `SCPCIdentityMismatch` | The locked SCPC name/namespace now has a different UID. Restore the original object/contract or perform verified manual cleanup; replacement objects are not followed. |
| Deleting with reason `SCPCNotFound` | The exact locked SCPC is missing. Restore it so cleanup can use its target connection and resource contract. The finalizer is intentionally retained. |
| `Error` condition, reason `TargetOwnershipConflict` | A deterministic target name is occupied by an object without this SimpleCluster's ownership annotations. The operator neither adopts nor deletes it; resolve the collision explicitly. |
| `Error` condition, reason `UnknownInput` | `spec.inputs` has a key the chosen config does not declare. Inputs are a closed set. |
| `Error` condition, reason `BadInput` | A value does not parse against its declared type. The message names the key, the type and the value. |
| `Error` condition, reason `ProvisioningDisabled` | The selected SCPC is retired and at least one target is missing. The cluster is `Failed`/`Ready=False`; observed targets remain deletable, but the operator will not recreate the missing one. |

## Phase is `NoHardware`

A `from: profile` input has no override and no entry in the config's
`spec.profiles` is large enough for the request. Nothing is created — this is a
refusal, not a failure.

Either shrink `spec.workers.requested` / `spec.controlPlane.requested`, or add a
larger profile to the provider config. Check the platform actually offers that
machine type first: a profile that does not exist upstream converts a clean
refusal into a failed provisioning run.

Remember the profile is matched against the **larger** of the worker and
control-plane asks.

## Phase is `Provisioning` and nothing moves

Read `status.resources[]`:

- **`reason: WaitingForDependencies`** — a `dependsOn` predecessor is not Ready.
  `message` names it. Follow the chain to the first resource that is not
  progressing.
- **`reason: Creating`** — the object exists on the target cluster but has not
  reported readiness. Look at it directly:

  ```sh
  kubectl get <kind> <targetName> -n <targetNamespace> --kubeconfig <target>
  ```

- **`reason: Provisioning`** with an empty `lastOperation` — the provider config
  has no `lastOperationPath`, so there is nothing to show. Add one.

If a resource looks Ready to you but the operator disagrees, the `readiness`
block is reading the wrong path. Without one, the operator expects
`status.phase` with the value `Ready`.

## A resource is never created

Check the provider config resolved its types:

```sh
kubectl get scpc <name> -o jsonpath='{.status.resources}' | jq
```

A missing entry means the target cluster does not serve that
`apiVersion`/`kind` — the CRD is not installed, or the credential lacks
permission to see it.

If `status.reachable` is false, the Secret in `spec.target.kubeconfigSecretRef`
is missing, malformed, or its credential has expired.

## Template errors

Rendering runs with `missingkey=error`, so a typo fails loudly rather than
producing an empty value. The error names the offending path.

Common causes:

- referencing `.Resources.<name>` for a resource **not** listed in this
  resource's `dependsOn` — predecessors are only in scope when declared;
- expecting `.Resources.<name>` to be an object when that entry uses `repeat` —
  it is a list;
- using `env` or `expandenv`. Both are deliberately removed from the sprig
  function map so a template cannot read the operator pod's environment.

## The kubeconfig never appears

The kubeconfig is fetched only after the resource named by
`kubeconfigSource.resourceName` reports Ready — so `Phase=Ready` on the
SimpleCluster is not itself a guarantee that the Secret exists. `KubeconfigValid`
is the condition that answers this; when it is `False` its reason and message say
why. If the source resource is Ready and the Secret still does not appear:

- **`secretRef`** — the templated name or namespace does not match reality.
  Both are templates with the live resource in scope; render them mentally
  against `status.resources[]`. Check `dataKey` too — it defaults to
  `kubeconfig`. A `406`, unexpected metadata `Content-Type`, or unexpected
  response kind means the target API server did not satisfy the strict
  `meta.k8s.io/v1 PartialObjectMetadata` contract. A zero-result data query
  means the Secret was removed or no longer has every configured required
  label; retry only after verifying its ownership metadata.
- **`subResource`** — the credential lacks permission to call the subresource, or
  `kubeconfigPath` points at the wrong field.
- **`inline`** — `kubeconfigPath` does not resolve on the object.

## The kubeconfig expired

`status.kubeconfigExpiresAt` is in the past and `KubeconfigValid` is False. Only
the `subResource` strategy can re-issue; `secretRef` and `inline` deliver
whatever the platform minted. Shorten `renewalBefore` if renewal is running too
late.

## A cluster will not delete

`Phase=Deleting` persists because a provider resource is not going away.

```sh
kubectl get simc <name> -o jsonpath='{.status.resources}' | jq
kubectl get <kind> <targetName> -n <targetNamespace> -o yaml
```

- The provider's controller may require a confirmation the config does not
  declare — e.g. Gardener needs
  `confirmation.gardener.cloud/deletion: "true"` in `deletionAnnotations`.
- The provider's controller may be down, so its finalizer is never removed.
- A resource whose teardown depends on infrastructure disappearing in the same
  cascade can hang forever. That is what `forceCleanupOnCascade: true` is for —
  but only for resources with **no external state worth reclaiming**. Setting it
  on a Gardener Shoot or an OpenStackVM leaks real infrastructure.

## `spec.resources` did not apply into the new cluster

These are best-effort add-ons applied after the cluster is Ready. Each has a
`Resource-<name>` condition carrying the outcome, and a failure never blocks
`Phase=Ready` — a Ready cluster with a failed add-on is the expected shape, not a
contradiction.

## Extensions

| Symptom | Likely cause |
|---|---|
| No Extensions section at all | The catalogue was read successfully but contains no extension for `SimpleCluster`. `{"addons":[]}` is the normal empty answer. |
| "Catalogue could not be read" | The aggregate is unavailable — the aggregator has not run, the ConfigMap is missing in the UI's namespace, or the UI cannot read it. Check `--addon-catalog-namespace` matches where the UI runs and that the operator has ConfigMap RBAC. |
| An extension is installed but not listed | Its ConfigMap is missing the marker label, is not in `--addon-catalog-source-namespace` (default `kube-system`), or its `addon.yaml` has no `id`/`label`. The operator logs each entry it skips. |
| Listed but always disabled | Entitlement said no, or was inconclusive — entitlement fails closed. A probe for a **namespaced** Kind without `namespaced: true` is a schema error that reads exactly like "not bound". |
| Box ticked, nothing installs | The catalogue's `label` and the addon operator's watched label have drifted apart, or the addon operator is not running. |

## The portal shows no providers

The UI discovers providers from the workspace GraphQL schema and falls back to
the static `clusterConfig.providers[]` list. Unlike entitlement, discovery
**fails open** — an empty dropdown means both the schema query and the fallback
list came back empty. Check `graphql.url`, then the overlay's
`ui-providers.yaml`.

Bind-only providers are invisible to schema discovery by design and must be in
the static list.

## Getting more detail

```sh
kubectl -n simple-cluster-system logs deploy/simple-cluster-operator-controller-manager -f
kubectl get events -n <namespace> --field-selector involvedObject.name=<simplecluster>
```
