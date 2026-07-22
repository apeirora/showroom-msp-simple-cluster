# bind-watcher-operator

Watches KCP `APIBinding` objects through APIExport virtual workspaces and
reflects them onto the local MSP cluster: for every KCP logical cluster
(workspace) it maintains a label on the namespace that "owns" that cluster,
listing which APIExports are currently bound.

This lets the Simple Cluster UI answer *"which providers are installed for this
account?"* by reading a single namespace label instead of guessing.

## How it works

* One [`apiexport`](https://github.com/kcp-dev/multicluster-provider) provider is
  registered per endpoint in `KCP_API_EXPORT_ENDPOINTS`. Through an APIExport
  virtual workspace, multicluster-runtime *engages* exactly the workspaces that
  have **bound** that export — so a bind engages a cluster and an unbind cancels
  it. Each provider therefore observes its own export only; deploy/point it at
  every provider export you want represented.
* On every `APIBinding` change the reconciler updates an in-memory index
  (`realCluster → provider → binding → export`), then re-renders the exports
  annotation on the MSP namespace(s) selected by
  `metadata.labels[<CLUSTER_LABEL_KEY>] == <logical cluster>`.
* **Unbind** is handled explicitly: the controller is also registered as a
  cluster-aware runnable, so when a workspace disengages (the last binding of an
  export is removed) it drops that provider's entries and clears the annotation.
  Operator shutdown is distinguished from a real disengage, so stopping the
  operator does not wipe annotations.
* Because the same workspace may bind several exports through several providers,
  the index is keyed per provider and annotation writes use an optimistic-lock
  patch with conflict retry, so concurrent providers cannot lose an update.
* Namespace annotation uses a direct client to the local (in-cluster) MSP API
  server. The manager itself only talks to KCP.

## Namespace mapping & annotation

* **Mapping:** the operator finds the MSP namespace whose **label**
  `kcp.io/cluster` equals the KCP logical cluster name (a label is used here
  because it is a server-side selector). Configurable via `CLUSTER_LABEL_KEY`.
* **Annotation written:** `bind.simple-cluster.apeirora.eu/exports` = sorted,
  comma-separated list of bound exports, e.g.
  `bind.simple-cluster.apeirora.eu/exports: simplecluster,dati`.

The list is stored as an **annotation** (not a label) so it can contain commas
and is not limited to 63 characters. When no exports remain the annotation is
removed.

## Configuration (env)

| Variable                   | Required | Default                                      | Meaning                                              |
|----------------------------|----------|----------------------------------------------|------------------------------------------------------|
| `KCP_KUBECONFIG`           | yes      | —                                            | Path to kubeconfig with access to the export VWs     |
| `KCP_API_EXPORT_ENDPOINTS` | yes      | —                                            | Comma-separated APIExport virtual-workspace URLs     |
| `CLUSTER_LABEL_KEY`        | no       | `kcp.io/cluster`                             | Namespace label whose value is the logical cluster   |
| `EXPORTS_ANNOTATION_KEY`   | no       | `bind.simple-cluster.apeirora.eu/exports`    | Annotation written with the bound-export list        |
| `EXPORTS_SEPARATOR`        | no       | `,`                                          | Separator joining export names in the annotation     |

The in-cluster (MSP) API server is used for namespace access via the standard
controller-runtime config resolution.
