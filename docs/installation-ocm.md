# OCM delivery

Every release also publishes an [Open Component Model](https://ocm.software/)
component version that names every image and chart of that release, so the whole
stack can be transported into an air-gapped or mirrored environment as one unit.

## The component

```
showroom.apeirora.eu/simple-cluster : <X.Y.Z>
```

published to `oci://ghcr.io/apeirora/ocm`.

It references, all at the same version:

- eight `ociImage` resources — the seven operator images and the UI image;
- seven `helmChart` resources — every published chart.

The component descriptor is [`.ocm/component-constructor.yaml`](../.ocm/component-constructor.yaml).
`${VERSION}` and `${GITHUB_REPOSITORY_OWNER}` are substituted by the OCM CLI at
publish time, which is what guarantees a component version can never mix
artefacts from different releases.

## Inspecting a component version

```sh
ocm get componentversion \
  ghcr.io/apeirora/ocm//showroom.apeirora.eu/simple-cluster:<X.Y.Z> -o yaml
```

## Transporting into another registry

```sh
ocm transfer componentversion \
  ghcr.io/apeirora/ocm//showroom.apeirora.eu/simple-cluster:<X.Y.Z> \
  oci://my-registry.example.com/ocm \
  --copy-resources --recursive
```

`--copy-resources` copies the referenced images and charts as well as the
descriptor, which is what makes the target registry self-sufficient.

## Building it locally

```sh
ocm add componentversions --create --file dist/ctf \
  .ocm/component-constructor.yaml \
  -- VERSION=0.0.0-dev GITHUB_REPOSITORY_OWNER=apeirora
ocm get componentversion ./dist/ctf -o yaml
```

## Adding a component to the descriptor

A new image or chart must be added in three places to stay version-consistent:
the relevant matrix in `.github/workflows/publish.yml`, the chart list in the
same workflow, and `.ocm/component-constructor.yaml`. See the
[provider implementation guide](provider-guide.md#wire-it-into-the-build).
