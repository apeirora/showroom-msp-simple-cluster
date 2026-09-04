# bind-watcher-operator

Watches KCP `APIBinding` objects through APIExport virtual workspaces and
reflects them onto the local MSP cluster: for every KCP logical cluster
(workspace) it maintains an annotation on the MSP namespace named after that
cluster, listing which APIExports are currently bound.

This lets the Simple Cluster UI answer *"which providers are installed for this
account?"* by reading a single namespace annotation instead of guessing.

## How it works

* **Endpoint discovery** — the operator polls `APIExportEndpointSlice` objects in
  the workspaces listed in `KCP_WORKSPACES` (or, if unset, the kubeconfig's own
  workspace). Each ready `status.endpoints[].url` is an APIExport virtual-workspace
  URL; the operator registers one
  [`apiexport`](https://github.com/kcp-dev/multicluster-provider) provider per
  endpoint and drops it again when the slice disappears. No endpoints are
  hard-coded, and multiple provider workspaces (e.g. simple-cluster and dati) can
  be watched at once. All log lines carry the KCP `workspace` path (from the
  APIBinding `kcp.io/path` annotation) alongside the logical cluster id.
* **Complete inventory before writes** — an `APIBinding` event triggers an
  aggregate snapshot, not an incremental annotation replacement. Every
  configured workspace must have a successful endpoint discovery, every
  discovered slice must have a URL, and every provider's initial APIBinding
  cache must be synchronized before any annotation is changed. This preserves
  existing provider configuration during startup and reconnect; informer event
  order is not an authoritative inventory. A failure in any configured source
  conservatively delays all annotation changes until it recovers.
* **Mapping** — the MSP namespace is named after the KCP logical cluster, so it
  is selected by its well-known name label
  `kubernetes.io/metadata.name == <logical cluster>` (configurable via
  `CLUSTER_LABEL_KEY`).
* **Namespace creation** — the MSP namespace is normally created by the
  sync-agent when the first object is synced. An account that only *bound* a
  provider (no instance yet) has no namespace. When `CREATE_NAMESPACES` is on
  (default), the operator creates it (named after the logical cluster) so the
  binding can be recorded, tagging it with `MANAGED_LABEL_KEY` to tell it apart
  from sync-agent-created namespaces. Creation is idempotent; the operator never
  deletes namespaces.
* **Unbind** requires authoritative absence from a complete inventory. Transport
  cancellation, discovery errors and temporarily unready endpoint slices do not
  delete exports. A successfully observed binding/endpoint deletion still removes
  that export, including the last binding. Periodic discovery also republishes
  the snapshot, covering unbinds while stopped and caches that synchronize after
  their initial events. Stopped providers are retried with generation-specific
  names so a previous provider's asynchronous exit cannot remove its replacement.
* Snapshot publication is serialized and annotation patches retain optimistic
  conflict retries. Duplicate observations of a binding are deduplicated;
  conflicting export identities or missing logical-cluster identity block writes.
* The MSP namespace is written through a direct in-cluster client; the manager
  itself only talks to KCP.

## Annotation

`bind.simple-cluster.apeirora.eu/exports` = sorted, comma-separated list of
bound exports, e.g. `simplecluster,dati`. Stored as an annotation (not a label)
so it can hold commas and is not limited to 63 characters. When no exports
remain the annotation is removed.

## Configuration (env)

| Variable                   | Required | Default                                      | Meaning                                                      |
|----------------------------|----------|----------------------------------------------|--------------------------------------------------------------|
| `KCP_KUBECONFIG`           | yes      | —                                            | Kubeconfig for KCP (auth; host rewritten per workspace)      |
| `KCP_WORKSPACES`           | no       | *(kubeconfig's own)*                         | Comma-separated workspace paths to scan for endpoint slices  |
| `KCP_DISCOVERY_INTERVAL`   | no       | `1m`                                         | How often to re-list `APIExportEndpointSlice`s               |
| `CLUSTER_LABEL_KEY`        | no       | `kubernetes.io/metadata.name`                | Namespace label whose value is the logical cluster name      |
| `EXPORTS_ANNOTATION_KEY`   | no       | `bind.simple-cluster.apeirora.eu/exports`    | Annotation written with the bound-export list                |
| `EXPORTS_SEPARATOR`        | no       | `,`                                          | Separator joining export names in the annotation             |
| `CREATE_NAMESPACES`        | no       | `true`                                       | Create the MSP namespace when bound but none exists          |
| `MANAGED_LABEL_KEY`        | no       | `bind-watcher.apeirora.eu/managed`           | Marker label on namespaces the operator creates              |

The MSP (in-cluster) API server is used for namespace access via the standard
controller-runtime config resolution.

## Access requirements

* **MSP side** (`ServiceAccount` RBAC): `get`/`list`/`watch`/`create`/`update`/`patch`
  on `namespaces`.
* **KCP side** (governed by `KCP_KUBECONFIG`, not by the chart RBAC): `list` on
  `apiexportendpointslices` in the target workspace, plus access to each
  discovered APIExport virtual workspace.

## Assumptions

* The MSP namespace is named exactly the KCP logical cluster id.
* All APIExports of interest are reachable from the single configured workspace
  (their `APIExportEndpointSlice`s live there).
