# GitOps deployment with Flux

The production deployment is driven by Flux `HelmRelease` resources pointing at
the published OCI charts.

## Layout

The infrastructure repository holds one `HelmRelease` per chart under
`apps/simple-cluster/`:

| HelmRelease | Chart | Notes |
|---|---|---|
| `operator-system-helm.yaml` | `simple-cluster-system` | umbrella: core + KinD / OpenStack / Ubuntu / SSH-key operators |
| `ui-helm.yaml` | `simple-cluster-ui` | portal micro-frontend |
| `ubuntu-machines-helm.yaml` | `ubuntu-machines-operator` | SSH-host-pool operator |
| `bind-watcher-helm.yaml` | `bind-watcher-operator` | KCP bind discovery |
| `pm-integration-helm.yaml` | `simple-cluster-pm-integration` | applied into the Platform Mesh cluster via a kubeconfig Secret |
| `sync-agent-helm.yaml` | `simple-cluster-sync-agent` | `dependsOn` pm-integration |

All of them pin a range — `version: ">=0.2.0 <1.0.0"` — against a shared
`HelmRepository`, and pull images with an image-pull Secret. Because every
release stamps one version into every chart and image, a range is enough to keep
the whole stack consistent.

## Environments

Environment is selected by Kustomize overlays,
`apps/simple-cluster/overlays/{dev,prod}`, wired per cluster — for example
`clusters/<dev-landscape>/<cluster>` → dev and
`clusters/<prod-landscape>/<cluster>` → prod.

Per-environment UI values live in the overlay:

- `overlays/<env>/ui-values.yaml` — public host, TLS, ingress
- `overlays/<env>/ui-providers.yaml` — which providers are enabled and their API
  groups

## Ordering

`pm-integration` publishes the `APIExport` the sync agent binds to, so the
sync-agent release must declare `dependsOn` on it. Everything else is
order-independent: the operators reconcile whatever SCPCs exist whenever they
appear.

## Remote targets

An SCPC that builds on a remote cluster needs a Secret in the management cluster
whose `kubeconfig` key holds credentials for that cluster, referenced from
`spec.target.kubeconfigSecretRef`. Manage those Secrets the way you manage any
other cluster credential — an external secret store rather than the Git
repository.
