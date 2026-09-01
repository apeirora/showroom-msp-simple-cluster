# Installing with Helm

Charts are published as OCI artefacts under `oci://ghcr.io/apeirora/charts`, all
at the same version as the operator images they reference.

## The umbrella chart

`simple-cluster-system` installs the core operator plus the in-repo provider
operators (KinD, OpenStack, Ubuntu, SSH key) as subcharts.

```sh
helm upgrade --install simple-cluster-system \
  oci://ghcr.io/apeirora/charts/simple-cluster-system \
  --namespace simple-cluster-system --create-namespace \
  --version <X.Y.Z>
```

From a checkout, to install the working tree instead:

```sh
helm upgrade --install simple-cluster-system charts/simple-cluster-system \
  --namespace simple-cluster-system --create-namespace
```

## The other charts

| Chart | Purpose |
|---|---|
| `simple-cluster-system` | umbrella: core operator + KinD / OpenStack / Ubuntu / SSH-key operators |
| `simple-cluster-ui` | the portal micro-frontend |
| `simple-cluster-pm-integration` | Platform Mesh metadata — `APIExport`, `ProviderMetadata`, `ContentConfiguration` |
| `simple-cluster-sync-agent` | KCP api-syncagent and its `PublishedResource` definitions |
| `ubuntu-machines-operator` | the SSH-host-pool operator |
| `sshkey-operator` | standalone SSH-key operator, when not using the umbrella |
| `bind-watcher-operator` | KCP bind discovery |

`simple-cluster-scpc-config` is **not published**. It renders provider configs
for in-repo providers only, and is superseded by providers shipping their own
SCPC emitters.

## Verifying

```sh
kubectl -n simple-cluster-system get deploy
kubectl get crd | grep apeirora
kubectl get scpc -A
```

An SCPC's `status.reachable` tells you whether its target cluster can be reached;
`status.resources[]` lists a resolved GroupVersionKind per templated resource. If
a GVK is missing, the target cluster does not serve that type.

## Configuration

See [Configuration](configuration.md) for every value the charts expose.

## Upgrading

Images and charts always ship in lockstep at one version, so upgrading is a
single `helm upgrade` per chart to the same new version. Chart versions committed
in `Chart.yaml` are development placeholders — the published version is always
the release version, which is why GitOps `HelmRelease`s pin a range rather than
an exact version.
