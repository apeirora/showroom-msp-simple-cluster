# Configuration

Every value the Helm charts expose, and the operator flags they render.

## Umbrella chart — `charts/simple-cluster-system`

| Value | Meaning |
|---|---|
| `global.imageTag` | One tag for **all** operator images. Empty means `latest`; the publish workflow stamps the release version. |
| `<subchart>.controllerManager.container.image.repository` | Override an individual operator's image. |
| `<subchart>.metrics.enable` | Expose the controller-runtime metrics endpoint. |

Subcharts: `simple-cluster-operator`, `simple-cluster-operator-kind`,
`simple-cluster-operator-openstack`, `simple-cluster-operator-ubuntu`,
`sshkey-operator`.

### Provider-config resolution

`--scpc-fallback-namespace` sets the namespace searched when a `SimpleCluster`'s
`providerConfigRefs` do not resolve in its own namespace. That is how an
administrator publishes a shared catalogue centrally while still letting a tenant
override an individual configuration locally.

### Extension catalogue

Rendered from `charts/simple-cluster-system/charts/simple-cluster-operator/values.yaml`
under `addonCatalog.*`:

| Value | Flag | Default | Meaning |
|---|---|---|---|
| `addonCatalog.enable` | — | `false` | Turns the aggregator on. Leave off for a landscape without the portal. |
| `addonCatalog.namespace` | `--addon-catalog-namespace` | release namespace | Where the aggregate ConfigMap is written. Must be the namespace the UI runs in. An empty flag value disables the aggregator entirely. |
| `addonCatalog.configMapName` | `--addon-catalog-configmap` | `simple-cluster-addons` | Name of the aggregate. Must match the UI's `addonCatalog.configMapName`. |
| `addonCatalog.sourceNamespace` | `--addon-catalog-source-namespace` | `kube-system` | Where extensions publish their catalogue entries. |
| `addonCatalog.interval` | `--addon-catalog-interval` | `2m` | Poll interval. |

## UI chart — `charts/simple-cluster-ui`

Values are rendered into `simple-cluster-config.json` and served with the
micro-frontend.

| Value | Meaning |
|---|---|
| `PUBLIC_HOST`, `PUBLIC_SCHEME` | Public address of the portal. **Must match** the values given to `simple-cluster-pm-integration`. |
| `tls.secretName` | TLS Secret for the ingress. |
| `graphql.url` | The workspace GraphQL endpoint used for provider discovery and entitlement checks. |
| `branding.sidebar.title` | Displayed title. |
| `clusterConfig.providerSources[]` | The live provider dropdown. Each entry is `{group, version, listField, label, providerConfigName, namespaced}`. |
| `clusterConfig.providers[]` | Static fallback list, used when `providerSources` is empty and for bind-only providers that schema discovery cannot see. |
| `clusterConfig.cpu[]`, `clusterConfig.memory[]` | The resource choices offered in the create dialog. |
| `clusterConfig.controlPlane{}`, `clusterConfig.workers{}` | Node-count bounds and defaults. |
| `addonCatalog.enabled` | Mounts the aggregated extension catalogue at `/addons/`. |
| `addonCatalog.configMapName` | Must match the operator's `--addon-catalog-configmap`, and the operator must write into the UI's namespace. |

The catalogue is mounted as a **directory** without `subPath`. A subPath mount is
a copy taken at start-up and would never pick up a newly installed extension. The
volume is `optional: true`, so the pod starts whether or not the aggregate exists
yet.

## Platform Mesh integration — `charts/simple-cluster-pm-integration`

Publishes the KCP `APIExport`, the marketplace `ProviderMetadata` tile and the
`ContentConfiguration` for the Luigi micro-frontend. `PUBLIC_HOST` and
`PUBLIC_SCHEME` must agree with the UI chart — the `ContentConfiguration` points
the portal at the micro-frontend by URL.

## Sync agent — `charts/simple-cluster-sync-agent`

Runs the api-syncagent with `PublishedResource` definitions that project
`SimpleCluster` between KCP and the service cluster and mirror the kubeconfig
Secret into the account workspace. It bundles the upstream
[kcp api-syncagent](https://github.com/kcp-dev/helm-charts) chart as a
dependency, and must be installed after `simple-cluster-pm-integration`.

## Provider configs — `charts/simple-cluster-scpc-config`

Per-provider `enable.*` flags render `SimpleClusterProviderConfig` resources.
This chart is **not published** and is being superseded: branded providers ship
their own SCPC emitters, and only the in-repo `kind` and `ubuntu-machines`
configurations remain templated here.
