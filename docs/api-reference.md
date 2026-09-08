# API reference

API group `showroom.apeirora.eu`, version `v1alpha1`.

Both resources are namespaced. Short names: `simc` for `SimpleCluster`, `scpc`
for `SimpleClusterProviderConfig`.

---

## SimpleCluster

The user-facing cluster request.

```yaml
apiVersion: showroom.apeirora.eu/v1alpha1
kind: SimpleCluster
metadata:
  name: demo
  namespace: team-a
spec:
  providerConfigRefs: [gardener-openstack, kind-local]
  workers:
    autoscaling: {min: 2, max: 6}
    requested: {cpu: "8", memory: 32Gi}
  controlPlane:
    nodes: 3
    requested: {cpu: "4", memory: 16Gi}
  inputs:
    region: eu-de-1
```

### `spec`

| Field | Type | Required | Description |
|---|---|---|---|
| `providerConfigRefs` | `[]string` | yes | Ordered list of candidate `SimpleClusterProviderConfig` names. The operator picks one and records it in `status.providerConfigRef`; the choice is then locked against later spec edits. Resolution looks in this namespace first, then in the operator's fallback namespace. |
| `workers` | `WorkersSpec` | yes | Worker pool sizing and per-worker resource ask. |
| `controlPlane` | `ControlPlaneSpec` | no | Control-plane pool. Providers that manage the control plane themselves (Gardener) ignore it. When omitted, templates see one node with the worker resource ask. |
| `inputs` | `map[string]string` | no | Per-cluster overrides. Every key **must** be declared in the chosen SCPC's `spec.inputs`; an unknown key is rejected. Unset keys fall back to the declaration's default. |
| `kubeconfigValidity` | `duration` | no | Overrides the SCPC's `kubeconfigSource.validity` for this cluster. |
| `kubeconfigRenewalBefore` | `duration` | no | Overrides the SCPC's `kubeconfigSource.renewalBefore` for this cluster. |
| `resources` | `[]ClusterResource` | no | Manifests applied **into** the provisioned cluster once it is Ready. Best-effort: each outcome is a `Resource-<name>` condition, and a failure never blocks `Phase=Ready`. |

#### `WorkersSpec`

Exactly one of `nodes` or `autoscaling` must be set.

| Field | Type | Description |
|---|---|---|
| `nodes` | `int32` (1–100) | Fixed worker count. |
| `autoscaling.min` | `int32` (1–100) | Lower bound of an autoscaled pool. |
| `autoscaling.max` | `int32` (1–100) | Upper bound; must be ≥ `min`. |
| `requested.cpu` | `Quantity` | Per-worker CPU ask, e.g. `"8"`, `"500m"`. |
| `requested.memory` | `Quantity` | Per-worker memory ask, e.g. `32Gi`. |

Only providers with a native autoscaler honour the range. Others provision `min`
and report the limitation through the `AutoscalingSupported` condition — the
request is not rejected.

#### `ControlPlaneSpec`

| Field | Type | Description |
|---|---|---|
| `nodes` | `int32` (1–10) | Control-plane node count. 1 for development, 3 for HA. |
| `requested.cpu`, `requested.memory` | `Quantity` | Per-node resource ask. |

#### `ClusterResource`

| Field | Type | Description |
|---|---|---|
| `name` | `string` | DNS-label-safe identifier; becomes the suffix of the `Resource-<name>` condition. |
| `content` | `string` | One Kubernetes object as YAML. `apiVersion`, `kind` and `metadata.name` are required. |

### `status`

| Field | Type | Description |
|---|---|---|
| `phase` | `Phase` | Aggregate state — see [Phases](#phases). |
| `providerConfigRef` | `LocalObjectReference` | Name of the SCPC locked before target work. |
| `providerConfigNamespace` | `string` | Namespace of the locked SCPC. |
| `providerConfigUID` | `UID` | Kubernetes identity of the locked SCPC; a same-name replacement is rejected. |
| `targetCluster` | `string` | Where the resources live: the SCPC name for a remote target, `local` otherwise. |
| `resources` | `[]ResourceStatus` | Per-resource state of the SCPC's DAG. |
| `kubeconfigSecretRef` | `LocalObjectReference` | The Secret in this namespace holding the new cluster's kubeconfig. |
| `kubeconfigIssuedAt`, `kubeconfigExpiresAt` | `Time` | Lifetime of the embedded credential. |
| `kubeconfig` | `string` | The kubeconfig, mirrored from the Secret so a portal detail view can offer it for download. Readable by anyone who can `get` this SimpleCluster — the same audience that can already read the Secret. |
| `cp`, `wkr` | `string` | `<ready>/<declared>` counts, surfaced as printer columns. |
| `nodes` | `[]NodeStatus` | The provisioned cluster's nodes, read live over the synced kubeconfig. Best-effort; never affects `phase`. |
| `conditions` | `[]Condition` | See [Conditions](#conditions). |

#### `ResourceStatus`

| Field | Description |
|---|---|
| `name` | Matches the SCPC `resources[].name`. |
| `gvk` | Resolved GroupVersionKind of the created object. |
| `targetName`, `targetNamespace` | Where the object lives on the target cluster. Empty for repeated specs — see `instances`. |
| `phase` | This resource's coarse state; aggregated across `instances` when repeated. |
| `reason` | `WaitingForDependencies`, `Creating`, `Provisioning`, `Ready`, `Failed` or `Deleting`. |
| `message` | Human-readable detail, e.g. `waiting on: cp, network`. |
| `lastOperation`, `lastError` | Progress and error strings mirrored from the object itself. |
| `instances` | Per-index state when the spec used `repeat`. |

#### `NodeStatus`

`name`, `role` (`control-plane` / `worker`), `externalIP`, `internalIP`, `ready`.

### Phases

| Phase | Meaning |
|---|---|
| `Pending` | Nothing created yet. |
| `Provisioning` | At least one resource is not Ready. |
| `Ready` | Every resource in the DAG is Ready. The kubeconfig sync runs after this and is reported separately — see `KubeconfigValid`. |
| `Failed` | At least one resource reports a terminal failure. |
| `Deleting` | The SimpleCluster has a deletion timestamp; the cascade is in flight. |
| `NoHardware` | A `from: profile` input has no override and no SCPC profile is large enough for the request. Nothing is created until a fitting profile appears or the request shrinks. |

### Conditions

| Type | Meaning |
|---|---|
| `Ready` | Every resource in the DAG is Ready, i.e. `Phase=Ready`. It does **not** imply that the kubeconfig has been synced yet. |
| `Progressing` | Work is in flight. |
| `Error` | A reconcile-level error, e.g. `UnknownInput`, `BadInput`. |
| `KubeconfigValid` | The synced kubeconfig is present and unexpired. This, together with `status.kubeconfigSecretRef`, is what to wait on before using the cluster. |
| `AutoscalingSupported` | Set only when the request asked for autoscaling: `True` when the chosen SCPC declares support, `False` when the pool stays fixed at `workers.min`. |
| `Resource-<name>` | Outcome of applying one `spec.resources[]` manifest into the provisioned cluster. |

Finalizer: `simplecluster.apeirora.eu/finalizer`.

---

## SimpleClusterProviderConfig

The administrator-facing template. One SCPC describes one way of building a
cluster on one platform.

```yaml
apiVersion: showroom.apeirora.eu/v1alpha1
kind: SimpleClusterProviderConfig
metadata:
  name: gardener-openstack
spec:
  autoscaling: true
  target:
    namespace: garden-<your-gardener-project>
    kubeconfigSecretRef: {name: gardener-kubeconfig, namespace: default}
  inputs:
    - {name: region,       type: string, default: eu-de-1, description: "Gardener region"}
    - {name: machineType,  from: profile,                  description: "Resolved from profiles"}
  profiles:
    - {name: m5.large,   cpu: "2", memory: 8Gi}
    - {name: m5.2xlarge, cpu: "8", memory: 32Gi}
  resources:
    - name: shoot
      apiVersion: core.gardener.cloud/v1beta1
      kind: Shoot
      deletionAnnotations: {confirmation.gardener.cloud/deletion: "true"}
      readiness:
        phasePath: status.lastOperation.state
        phaseReadyValue: Succeeded
        phaseFailedValues: [Failed, Error]
        lastOperationPath: status.lastOperation.description
      template: |
        spec:
          region: {{ .Inputs.region }}
          provider:
            workers:
              - name: worker
                machine: {type: {{ .Inputs.machineType }}}
                minimum: {{ .SC.workers.min }}
                maximum: {{ .SC.workers.max }}
  kubeconfigSource:
    resourceName: shoot
    subResource:
      apiVersion: core.gardener.cloud/v1beta1
      kind: Shoot
      subResource: adminkubeconfig
      kubeconfigPath: status.kubeconfig
    validity: 24h
    renewalBefore: 6h
```

### `spec`

| Field | Type | Required | Description |
|---|---|---|---|
| `provisioningEnabled` | `bool` | no | Defaults to `true`. Set to `false` to stop this SCPC from creating missing target resources while preserving observation and deletion of resources that already exist. |
| `target` | `TargetSpec` | yes | Which cluster the resources are created on, and the default namespace. |
| `resources` | `[]ResourceSpec` | yes (≥1) | The DAG of resources to render and create. |
| `kubeconfigSource` | `KubeconfigSource` | yes | How to obtain the provisioned cluster's kubeconfig. |
| `inputs` | `[]InputDeclaration` | no | The closed set of keys a SimpleCluster may override. |
| `profiles` | `[]Profile` | no | The closed set of machine types this config offers. |
| `autoscaling` | `bool` | no | Declares that the templates honour `.SC.workers.{min,max}` and the platform scales automatically. When false, an autoscaling request is provisioned at `min` and `AutoscalingSupported` is set to False. |

#### `TargetSpec`

| Field | Type | Description |
|---|---|---|
| `namespace` | `string` | Default namespace for namespaced resources. May be a Go template over `.SC` and `.Inputs` — e.g. `{{ .SC.namespace }}` to co-locate resources with the request. |
| `kubeconfigSecretRef` | `SecretReference` | Secret in the management cluster whose `kubeconfig` key holds credentials for the target cluster. When unset, resources are created locally. |

#### `ResourceSpec`

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | `string` | yes | Identity within this SCPC. Referenced as `.Resources.<name>` and from `kubeconfigSource.resourceName`. |
| `apiVersion`, `kind` | `string` | yes | The target resource type. It does not have to be compiled into the operator. |
| `template` | `string` | yes | YAML body, rendered through `text/template` + sprig. |
| `namespace` | `string` | no | Overrides `target.namespace` for this resource. May be a template. |
| `namePrefix` | `string` | no | Prepended to the generated name: `sanitise(<namePrefix><sc-name>-<name>)`. |
| `nameTemplate` | `string` | no | Overrides `namePrefix` and computes the name by rendering this template. Used verbatim. |
| `repeat` | `string` | no | Template rendering to a non-negative integer; fans this entry out to N objects suffixed `-0`, `-1`, … . `.Index` and `.Prev` are in scope inside `template`. Crash cleanup can reconstruct counts derived from `.SC`/`.Inputs`. Deletion cannot rebuild predecessor context for a count derived from `.Resources`; it deletes recorded owned instances, then retains the finalizer for verified manual cleanup. |
| `dependsOn` | `[]string` | no | Sibling names that must be Ready first. Cycles, unknown names and self-references are rejected. |
| `readiness` | `ReadinessCheck` | no | How to project this resource's status onto `Phase`. When omitted, the operator reads `status.phase` and treats `Ready` as ready, `Failed`/`Error` as failed, empty as pending, anything else as provisioning. |
| `deletionAnnotations` | `map[string]string` | no | Applied before deletion. Some controllers require a double opt-in, e.g. Gardener's `confirmation.gardener.cloud/deletion: "true"`. |
| `forceCleanupOnCascade` | `bool` | no | After Delete, strip finalizers following a short grace window. Only for resources with **no external state worth reclaiming** — e.g. a k3s uninstall over SSH against VMs that are disappearing in the same cascade. Setting it on a Gardener Shoot or an OpenStackVM leaks infrastructure. |

#### `ReadinessCheck`

Choose exactly one mapping shape.

Scalar status mapping:

| Field | Description |
|---|---|
| `phasePath` | JSONPath into the resource resolving to a state value, e.g. `status.lastOperation.state`. |
| `phaseReadyValue` | The value that means Ready, e.g. `Succeeded`. |
| `phaseFailedValues` | Values that mean Failed. |
| `lastOperationPath` | Optional JSONPath to a human-readable progress string. |
| `lastErrorPath` | Optional JSONPath to a human-readable error string. |

Kubernetes condition mapping:

| Field | Description |
|---|---|
| `conditions.ready.{type,status}` | Exact condition type and status (`True`, `False` or `Unknown`) that means Ready. |
| `conditions.failures[]` | Exact condition matches that mean Failed. Failure matches take precedence over readiness, and their messages are surfaced in stable type order. |
| `conditions.matchObservedGeneration` | When true, ignores non-zero condition generations that differ from the target object's generation. Leave false for cross-cluster sync paths whose source and target generations differ. |

#### `InputDeclaration`

| Field | Type | Description |
|---|---|---|
| `name` | `string` | Referenced as `.Inputs.<name>`. Must match `^[a-zA-Z][a-zA-Z0-9_]*$`. |
| `type` | `string`\|`int`\|`bool`\|`duration`\|`enum` | Defaults to `string`. Both the default and any override are parsed against it. |
| `default` | `string` | Substituted when the SimpleCluster does not set the key. Required unless `from` is set. |
| `from` | `profile` | Derives the value from `spec.profiles` instead of a default — see below. |
| `description` | `string` | Shown in CRD docs and UI tooling. |
| `allowEmpty` | `bool` | Permits `""` as a meaningful value for `type: string`. |
| `values` | `[]{key, label}` | The allowed choices when `type: enum`. Required and non-empty for enum inputs; the default and any override must match a `key`. |

#### `Profile`

| Field | Description |
|---|---|
| `name` | The machine type substituted into templates, e.g. `m5.large`. |
| `cpu`, `memory` | The profile's capacity per node. |

An input with `from: profile` resolves to the **smallest** profile whose `cpu`
and `memory` both satisfy the request — the larger of the worker and
control-plane asks. An explicit override on the SimpleCluster is passed through
unchanged and is not validated against the profile list. When nothing fits, the
SimpleCluster parks in `Phase=NoHardware`.

#### `KubeconfigSource`

Exactly one of `secretRef`, `subResource` or `inline` must be set.

| Field | Type | Description |
|---|---|---|
| `resourceName` | `string` | Which `resources[]` entry produces the cluster. |
| `secretRef.name`, `.namespace` | `string` (template) | Secret in the target cluster holding the kubeconfig. `namespace` defaults to the resource's namespace. The live resource is in scope as `.Resources.<resourceName>`. |
| `secretRef.dataKey` | `string` | Key inside the Secret; defaults to `kubeconfig`. |
| `secretRef.requiredLabels`, `.requiredAnnotations` | `map[string]string` (templated values) | Optional exact metadata requirements checked through a strict `meta.k8s.io/v1 PartialObjectMetadata` request with no full-object fallback. The server must return canonical `Content-Type: application/json` with an exact `meta.k8s.io/v1 PartialObjectMetadata` envelope. With at least one required label, the subsequent Secret data request is a server-filtered list constrained by namespace, `metadata.name`, and every rendered label; exactly one result with the metadata-read UID and resourceVersion is accepted. Annotation requirements cannot be server-filtered. With no required label, the compatibility path uses a name-only GET: metadata and UID/resourceVersion checks prevent use of a raced object but cannot prevent its bytes from being returned. Protect required keys with provider-side admission/RBAC or a stronger claim relation; matching user-writable metadata is not issuer authentication. |
| `subResource.{apiVersion,kind,subResource,kubeconfigPath}` | `string` | Call a subresource on the object, e.g. Gardener's `adminkubeconfig`, and read the kubeconfig at `kubeconfigPath`. |
| `subResource.expirationPath` | `string` | Optional JSONPath to the credential's expiry. |
| `inline.kubeconfigPath` | `string` | JSONPath locating kubeconfig bytes inside the object itself. |
| `validity` | `duration` | Desired credential lifetime (`subResource` only). |
| `renewalBefore` | `duration` | How long before expiry to re-issue (`subResource` only). |

### `status`

| Field | Description |
|---|---|
| `reachable` | Result of a smoke test against `target.kubeconfigSecretRef`. True when the target is the local cluster. |
| `resources` | Per-entry GVK resolution outcome against the target cluster. Populated once every GVK resolves. |
| `conditions` | Standard conditions. |
