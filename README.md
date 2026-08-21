# showroom-msp-simple-cluster

Provider-agnostic Kubernetes-cluster provisioning for the Platform Mesh / MSP
showroom. A user asks for a cluster with a small, uniform request; the core
operator turns that request into whatever concrete resource a chosen provider
needs — on the local management cluster or a remote one — and syncs the resulting
cluster's kubeconfig back to the user.

This monorepo holds the core operator, the in-repo building-block provider
operators, the Platform Mesh portal integration, the UI micro-frontend, and the
Helm charts that deploy the whole stack. Branded cloud providers (Dati, SAP,
Arsys, CloudFerro) live in their own repos and plug in through the same extension
points.

## Purpose and aggregator architecture

### Core model

Two CRDs (group `showroom.apeirora.eu/v1alpha1`), reconciled by
`simple-cluster-operator`:

- **`SimpleClusterProviderConfig` (SCPC)** — cluster-scoped, prepared by an
  administrator. Declares the resource(s) `R` to create (arbitrary
  `apiVersion`/`kind` + a Go `text/template` body), an optional remote-target
  kubeconfig, and how to fetch the resulting cluster's kubeconfig.
- **`SimpleCluster` (SC)** — namespaced, created by a user. References one or more
  candidate SCPCs and a worker count.

Reconcile flow (`simple-cluster-operator/internal/controller`):

1. Select an SCPC from the SC's `providerConfigRefs` and lock it into SC status.
2. Render the SCPC's `spec.resources[]` (Go `text/template` + sprig, fed by SC
   fields and previously-rendered resources) into concrete resource `R`.
3. Apply `R` with a **dynamic client** (its type need not be compiled in) to the
   local management cluster, or to a remote cluster when the SCPC carries a target
   kubeconfig Secret.
4. A provider operator turns `R` into a new cluster `NC`.
5. Once `R` is Ready, fetch `NC`'s kubeconfig (per `spec.kubeconfigSource`) and
   write it as Secret `<sc-name>-kubeconfig` in the SC namespace; SC status records
   the reference. Optional SC `spec.resources` are then applied into `NC` and node
   status is collected.

### Portal aggregation

The stack surfaces in the Platform Mesh portal without the core operator knowing
about any specific provider:

- **`simple-cluster-pm-integration`** publishes a KCP `APIExport`
  (`simpleclusters.showroom.apeirora.eu`) plus a `ContentConfiguration` (Luigi
  micro-frontend node) and a marketplace `ProviderMetadata` tile.
- **`simple-cluster-sync-agent`** runs an api-syncagent `PublishedResource` that
  projects `SimpleCluster` between KCP and the service cluster and **mirrors the
  kubeconfig Secret** back into the account workspace.
- **`bind-watcher-operator`** watches KCP `APIBinding`s through APIExport virtual
  workspaces and annotates each account namespace with the bound export names under
  `bind.simple-cluster.apeirora.eu/exports` (sorted, comma-separated). Bind-only
  providers read that annotation to emit their SCPC into an account; the UI
  additionally discovers installed providers from the workspace GraphQL schema,
  with a static fallback list.

### Providers

**In-repo building-block operators** — deployed by the umbrella chart, run on the
service cluster; their kinds are what provider SCPCs template into:

| Operator | Kind | API group |
|---|---|---|
| `simple-cluster-operator-kind` | `KindCluster` | `kind.apeirora.eu` |
| `simple-cluster-operator-openstack` | `OpenStackVM` | `cloudferro.eu` |
| `simple-cluster-operator-ubuntu` | `UbuntuCluster` | `ubuntu.apeirora.eu` |
| `sshkey-operator` | `SSHKey` | `sshkeys.apeirora.eu` |

**External branded provider repos** — each ships its own operator (or a bind-only
`NamespaceReconciler`) + pm-integration (own APIExport) + sync-agent + SCPC emitter:

| Provider (portal name) | Kind(s) | Portal API group |
|---|---|---|
| Dati Hosting | `Shoot` | `dati.eu` |
| SAP Hosting | `OpenStackShoot`, `IronCoreShoot`, `IronCoreVM` | `sap.eu` |
| Arsys Hosting | `UbuntuMachine` | `arsys.eu` |
| CloudFerro Hosting | `OpenStackVM` | `cloudferro.eu` |

### Extensions offered at creation time

A cluster can have extensions installed on it — Crypto Broker is the first — and the
create dialog offers one checkbox per extension the landscape has. Neither this repo nor
the UI knows which extensions exist: **each extension announces itself**, and the
operator collects those announcements into one document the dialog reads. Adding an
extension is installing its chart; nothing here changes.

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

**1. An extension publishes a catalogue entry.** A ConfigMap in `kube-system` labelled
`showroom.apeirora.eu/cluster-addon: "true"`, with the entry under `addon.yaml`:

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

`id` and `label` are required; everything else is optional (`displayName` defaults to
`id`). An entry missing either is skipped with a log line — one broken extension does not
cost the others their checkbox. The set of keys is **closed**: a key an extension invents
is dropped on the way through, so growing the contract means a field in `CatalogEntry` and
a release of this operator.

The extension should render this from the **same values its own operator watches**, so the
label a dialog offers and the label that triggers the install cannot drift apart.

**2. The operator aggregates.** `AddonCatalog` (`simple-cluster-operator/internal/controller/addoncatalog.go`)
is a leader-elected runnable that lists those ConfigMaps and writes their union into
`simple-cluster-addons` (key `simple-cluster-addons.json`), entries sorted by `id` so an
unchanged catalogue is not rewritten. A malformed entry is logged and skipped — one
broken extension must not cost the others their checkbox.

It **polls rather than watches**, deliberately: watching would put ConfigMaps into the
manager cache, and this operator creates ConfigMaps as ordinary downstream resources — a
label-filtered cache would silently stop returning those, an unfiltered one would hold
every ConfigMap in the cluster. Flags (see *Configuration*): `--addon-catalog-namespace`
(empty ⇒ aggregator off), `--addon-catalog-configmap`, `--addon-catalog-source-namespace`,
`--addon-catalog-interval`.

**3. The UI chart mounts the result** at `/usr/share/nginx/html/addons` as a **directory
mount, without `subPath`** — a subPath mount is a copy taken at start-up and would never
pick up a newly installed extension. The volume is `optional: true`, so the pod starts
whether or not the aggregate exists yet.

**4. The dialog offers and writes.** `ui/src/lib/addons.ts` fetches the file, keeps the
entries whose `targetKinds` include the kind being created, and asks KCP whether this
account is entitled to each. Every entry is rendered; unentitled ones are **disabled with
the reason as a tooltip** rather than hidden, so "why is Crypto Broker not offered?" has
an answer on screen. Ticking a box adds its `label` to `metadata.labels` of the
`SimpleCluster` being created — that is the entire install action.

**Entitlement** is asked in two ways, and the order matters: APIBindings are listed once
for all extensions and matched against `bindName` exactly or by prefix (kcp appends a
generated suffix — a real binding is `crypto-broker-crypto-broker-pzmzw`). A **match is
conclusive**; a miss falls through to a per-extension probe of the `entitlement` Kind, and
only both saying no makes it a no. Unlike provider discovery this **fails closed**: an
inconclusive answer leaves the box disabled, because an offered checkbox is a promise.

**What happens next is the extension's own business.** The addon operator sees the label
and installs onto the cluster once it is ready; switching an extension off, its version
and its install status live in the extension's own micro-frontend, not here. The checkbox
is opt-in at creation time only.

#### Troubleshooting

| Symptom | Likely cause |
|---|---|
| No Extensions section at all | Catalogue empty — no extension is installed, or the aggregator never ran. The browser console distinguishes the two: a 404 is logged as a deployment fault, `{"addons":[]}` is the normal empty answer |
| Section says the catalogue could not be read | The `simple-cluster-addons` ConfigMap is missing in the UI's namespace: check `--addon-catalog-namespace` matches where the UI runs, and the operator's RBAC on ConfigMaps |
| An extension is installed but not listed | Its ConfigMap is missing the marker label, is not in `--addon-catalog-source-namespace` (default `kube-system`), or its `addon.yaml` has no `id`/`label` — the operator logs the entry it skipped |
| Listed but always disabled | Entitlement said no or was inconclusive. A probe for a **namespaced** Kind without `namespaced: true` is a schema error that reads exactly like "not bound" |
| Box ticked, nothing installs | The catalogue's `label` and the addon operator's watched label have drifted apart, or the addon operator is not running |

## Repository layout

Monorepo of 7 Go modules — each with its own `go.mod` and `Makefile`; **there is no
top-level Makefile**.

- `simple-cluster-operator/` — core SC/SCPC operator.
- `simple-cluster-operator-kind/`, `-openstack/`, `-ubuntu/` — in-repo provider operators.
- `sshkey-operator/` — `SSHKey` operator.
- `bind-watcher-operator/` — KCP bind discovery + account-namespace annotation.
- `ubuntu-machines/` — generic SSH-host-pool operator (currently unwired).
- `ui/` — React + UI5 portal micro-frontend.
- `charts/` — Helm charts (umbrella + portal/sync/ui/etc.).
- `scripts/regen-helm-charts.sh`, `quick-start-kind.sh` — tooling.

## Local development and testing

Work per module (`cd` into it). The full kubebuilder Makefiles
(`simple-cluster-operator`, `sshkey-operator`) expose:

```sh
make build               # go build -o bin/manager cmd/main.go
make run                 # run against the current kubecontext
make test                # unit/integration via envtest (downloads setup-envtest)
make test-e2e            # Kind-based Ginkgo e2e (creates/destroys a kind cluster)
make lint                # golangci-lint
make manifests generate  # controller-gen (CRDs, RBAC, deepcopy)
make docker-build docker-push IMG=<registry>/<image>:<tag>
```

The reduced modules (`-kind`, `-openstack`, `-ubuntu`, `ubuntu-machines`) expose only
`build`/`fmt`/`vet`/`docker-build`/`install`/`deploy`; `bind-watcher-operator` uses
`build`/`test`/`lint`/`build-image`.

Whole-stack local environment:

```sh
./quick-start-kind.sh                        # kind + KubeVirt + operators (image built & side-loaded)
kind delete cluster --name simple-cluster    # tear down
```

CI (`.github/workflows/ci.yml`) runs `make build` + `make test` for
`simple-cluster-operator`, `sshkey-operator`, `bind-watcher-operator`, plus a
golangci-lint job.

**Toolchain:** Go 1.25 (`ubuntu-machines` is 1.24.5); controller-gen `v0.19.0`,
kustomize `v5.7.1`, golangci-lint `v2.4.0` (auto-installed into each module's `bin/`).

## Installation

### Local (Helm)

```sh
helm install -n simple-cluster-system --create-namespace \
  simple-cluster-system charts/simple-cluster-system
```

### Production (GitOps / Flux)

Charts are published as OCI artifacts at `oci://ghcr.io/apeirora/charts` and deployed
by Flux `HelmRelease`s in `showroom-msp-cluster-infra` under `apps/simple-cluster/`:

| HelmRelease | Chart | Notes |
|---|---|---|
| `operator-system-helm.yaml` | `simple-cluster-system` | umbrella: core + kind/openstack/ubuntu/sshkey operators |
| `ui-helm.yaml` | `simple-cluster-ui` | portal micro-frontend |
| `ubuntu-machines-helm.yaml` | `ubuntu-machines` | SSH-host-pool operator |
| `bind-watcher-helm.yaml` | `bind-watcher-operator` | bind discovery |
| `pm-integration-helm.yaml` | `simple-cluster-pm-integration` | applied into the Platform-Mesh cluster (`pm-kubeconfig`) |
| `sync-agent-helm.yaml` | `simple-cluster-sync-agent` | `dependsOn` pm-integration |

All pin `version: ">=0.2.0 <1.0.0"` against the shared `HelmRepository` and pull
images with the `ghcr-showroom-secret`. Environment is selected by
`apps/simple-cluster/overlays/{dev,prod}`, wired per cluster (e.g.
`clusters/cc-d2/msp03` → dev, `clusters/cc-two/msp03` → prod).

## Configuration

**Umbrella** (`charts/simple-cluster-system/values.yaml`):

- `global.imageTag` — single tag for **all** operator images (empty ⇒ `latest`; CI
  stamps the release version at publish time).
- `<subchart>.controllerManager.container.image.repository`, `<subchart>.metrics.enable`.

**UI** (`charts/simple-cluster-ui/values.yaml`, rendered into `simple-cluster-config.json`):

- `PUBLIC_HOST`, `PUBLIC_SCHEME` (must match pm-integration), `tls.secretName`,
  ingress/traefik settings, `graphql.url`, `branding.sidebar.title`.
- `clusterConfig.providerSources[]` — the live provider dropdown; each entry is
  `{group, version, listField, label, providerConfigName, namespaced}`.
- `clusterConfig.providers[]` — static fallback list (used when `providerSources` is
  empty / for bind-only providers invisible to schema discovery).
- `clusterConfig.cpu[]`, `memory[]`, `controlPlane{}`, `workers{}`.
- `addonCatalog.enabled`, `addonCatalog.configMapName` — mounts the aggregated extension
  catalogue at `/addons/`. The name must match the operator's `--addon-catalog-configmap`,
  and the operator must write into the UI's namespace.

**Extension catalogue** (`charts/simple-cluster-system/charts/simple-cluster-operator/values.yaml`,
`addonCatalog.*`): `enable`, `namespace` (empty ⇒ the release namespace, which is where the
UI runs), `configMapName`, `sourceNamespace` (default `kube-system`), `interval` (default
`2m`). These render the operator's `--addon-catalog-*` flags; leaving `enable: false` turns
the aggregator off entirely, which is right for a landscape without the portal.

In GitOps the UI values come from per-env overlays:
`overlays/<env>/ui-values.yaml` (host/TLS/ingress) and
`overlays/<env>/ui-providers.yaml` (which providers are enabled and their groups).

**SCPCs / provider configs** (`charts/simple-cluster-scpc-config`): `enable.*` flags
per provider render `SimpleClusterProviderConfig` CRs. This chart is **not published**
and is being superseded — branded providers now ship their own SCPC emitters; only the
in-repo `kind`/`ubuntu-machines` SCPCs remain templated here.

## Release procedure

release-please + a publish workflow; all charts and images ship **in lockstep at one
version**.

1. Merge PRs to `main` with Conventional-Commit titles (enforced by
   `.github/workflows/semantic-pr.yml`).
2. `.github/workflows/release-please.yml` maintains a release PR — one tracked package
   (repo root, `release-type: go`, plain `vX.Y.Z` tags; version in
   `.release-please-manifest.json`). Auth uses `RELEASE_PLEASE_TOKEN` (PAT) or the
   default `GITHUB_TOKEN` — **no GitHub App token**.
3. Merging the release PR creates the tag + GitHub Release and triggers
   `.github/workflows/publish.yml`.
4. **`publish-images`** (matrix) builds & pushes 8 images to
   `ghcr.io/apeirora/<image>:X.Y.Z` (+ `:latest`): `simple-cluster-operator`,
   `kind-cluster-operator`, `simple-cluster-operator-openstack`,
   `simple-cluster-operator-ubuntu`, `ubuntu-machines`, `sshkey-operator`,
   `bind-watcher-operator`, `simple-cluster-ui`.
5. **`publish-charts`** (after images) stamps `global.imageTag=X.Y.Z`, runs
   `helm dependency build`, then packages & pushes 7 charts at `--version`/`--app-version
   X.Y.Z` to `oci://ghcr.io/apeirora/charts`: `simple-cluster-system`,
   `ubuntu-machines-operator`, `sshkey-operator`, `bind-watcher-operator`,
   `simple-cluster-pm-integration`, `simple-cluster-ui`, `simple-cluster-sync-agent`.
   `simple-cluster-scpc-config` is excluded.

Notes:

- Committed `Chart.yaml` versions are dev-only; the published version is always the
  release version (which is why the HelmReleases pin `>=0.2.0 <1.0.0`).
- When operator scaffolding (CRDs/RBAC/manager) changes, regenerate the
  kubebuilder-derived charts locally with `scripts/regen-helm-charts.sh` before
  committing (it is not run in CI).
- `publish.yml` can be re-run via `workflow_dispatch` with an explicit version.

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
