# Platform Mesh integration

Three charts expose `SimpleCluster` in the Platform Mesh portal. The operators
(`charts/simple-cluster-system`) are unchanged and keep running on the MSP
service cluster.

| Chart | Target cluster | Purpose |
|---|---|---|
| `simple-cluster-pm-integration` | **cc-d2** (KCP control plane) | `APIExport` + `APIResourceSchema` + `ProviderMetadata` (marketplace descriptor) + `ContentConfiguration` (registers the UI) |
| `simple-cluster-ui` | **msp03** | nginx serving `pm-content.json` (the portal micro-frontend config) |
| `simple-cluster-sync-agent` | **msp03** | `api-syncagent` + `PublishedResource` — bridges `SimpleCluster` CRs and the kubeconfig Secret between the account workspace and the operators |

Provider identity is `simpleclusters.showroom.apeirora.eu` (the `APIExport`
name, matched verbatim by `ProviderMetadata.name` and the ContentConfiguration
`content-for` label).

## UI capabilities (declarative `generic-list-view`, no custom frontend)

- **List** — `metadata.name`, provider, CP, workers, phase, kubeconfig secret, expiry (the `kubectl get simpleclusters` columns)
- **Create** — name, **provider dropdown** (populated from available `SimpleClusterProviderConfig`s via GraphQL), control-plane nodes + CPU/memory, worker nodes + CPU/memory
- **Delete** — built into `generic-list-view`
- **Download kubeconfig** — `detailView.showDownloadKubeconfig: true`; works because the sync-agent mirrors `status.kubeconfigSecretRef` back to the workspace

## Publishing (CI)

On each release-please release, `.github/workflows/release-please.yml` publishes
to `ghcr.io/apeirora` at the release version:

- **Images** (`publish-images` matrix) — `simple-cluster-operator` (multi-arch),
  `kind-cluster-operator`, `simple-cluster-operator-openstack`,
  `simple-cluster-operator-ubuntu`, `ubuntu-machines` (amd64-only; their
  Dockerfiles hard-code `GOARCH=amd64`).
- **Charts** (`publish-charts`) — packaged at the release version and pushed to
  `oci://ghcr.io/apeirora/charts`: `simple-cluster-system`,
  `ubuntu-machines-operator` (chart name `ubuntu-machines`),
  `simple-cluster-pm-integration`, `simple-cluster-ui`,
  `simple-cluster-sync-agent`.

(The old single-operator `install-manifest` artifact step was dropped — the OCI
charts are now the install path.)

## Manual install (local charts)

```sh
# 1) Control plane (cc-d2) — into the provider's KCP workspace
helm upgrade --install simple-cluster-pm-integration \
  charts/simple-cluster-pm-integration \
  --set PUBLIC_HOST=simple-cluster.msp03.showroom.apeirora.eu \
  --set PUBLIC_SCHEME=https

# 2) UI (msp03)
helm upgrade --install -n simple-cluster-system simple-cluster-ui \
  charts/simple-cluster-ui \
  --set PUBLIC_HOST=simple-cluster.msp03.showroom.apeirora.eu \
  --set PUBLIC_SCHEME=https \
  --set traefik.ingressClass.name=traefik \
  --set tls.secretName=simple-cluster-ui-tls

# 3) Sync agent (msp03)
helm dependency build charts/simple-cluster-sync-agent
helm upgrade --install -n api-syncagent --create-namespace simple-cluster-sync-agent \
  charts/simple-cluster-sync-agent \
  --set syncAgentOperator.kcpKubeconfig=pm-kubeconfig
```

`PUBLIC_HOST`/`PUBLIC_SCHEME` must be identical on charts 1 and 2 — the
ContentConfiguration on cc-d2 points the portal at the UI's `pm-content.json`.

## Flux deployment (next step — not yet wired)

Target: existing `clusters/cc-d2/msp03` in `workspace-infra/showroom-msp-cluster-infra`
(no new cluster). Template to copy: `apps/simple-shoot/` (the dati deployment).

Planned `apps/simple-cluster/`:
- `base/` — namespace `simple-cluster-system`, `ghcr-secret` + `pm-kubeconfig`
  ExternalSecrets (vault property `simple-cluster`), and HelmReleases:
  - `simple-cluster-system` (msp03) — override each operator image
    `controllerManager.manager.image.repository` to `ghcr.io/apeirora/<image>`
    (`simple-cluster-operator`, `kind-cluster-operator`,
    `simple-cluster-operator-openstack`, `simple-cluster-operator-ubuntu`)
  - `ubuntu-machines` (msp03, optional) — image `ghcr.io/apeirora/ubuntu-machines`
  - `simple-cluster-pm-integration` (cc-d2 via `kubeConfig: pm-kubeconfig`,
    with the same `driftDetection.ignore` APIExport paths as simple-shoot)
  - `simple-cluster-sync-agent` (msp03), `dependsOn` pm-integration
  - `simple-cluster-ui` (msp03)
- `overlays/dev/` — ConfigMap-generated values; UI host
  `simple-cluster.msp03.dev.showroom.apeirora.eu` with gardener DNS/cert
  annotations.
- Add `../../../apps/simple-cluster/overlays/dev` to
  `clusters/cc-d2/msp03/kustomization.yaml`.

All charts referenced from `helm-showroom-repository`
(`oci://ghcr.io/apeirora/charts`) at `>=<version> <next-major>`.

## Notes / open items

- **Hostnames** above are placeholders — replace with the real msp03 DNS and the
  cc-d2 KCP workspace path.
- **Provider dropdown** lists *all* `SimpleClusterProviderConfig`s. True
  "available only" filtering needs a readiness field on SCPC status to filter on;
  revisit once SCPC exposes one.
- **APIResourceSchema** (`templates/api-resource-schemas.yaml`) is a hand-conversion
  of `simple-cluster-operator/config/crd/bases/showroom.apeirora.eu_simpleclusters.yaml`.
  Re-sync it if that CRD's schema or printer columns change.
- `APIResourceSchema` objects are immutable in KCP — bumping the schema requires a
  new `metadata.name` (e.g. a content hash suffix) and updating the `APIExport`
  `schema:` reference.
