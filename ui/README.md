# simple-cluster-ui

React + UI5 Web Components micro-frontend for the SimpleCluster provider, embedded
in the Platform Mesh portal as a Luigi micro-frontend.

It replaces the portal's declarative `generic-list-view`/`generic-detail-view` so it
can offer a working **Download kubeconfig** action: the native generic detail view only
downloads the account/KCP kubeconfig, whereas this app fetches the Secret named in
`status.kubeconfigSecretRef`, base64-decodes it, and lets the user download/copy the
provisioned cluster's kubeconfig.

## Runtime context

The portal passes `crdGatewayApiUrl`, a bearer `token`, the workspace path and the
`namespace` through the Luigi context (`src/lib/luigi.ts`). All Kubernetes access goes
through the workspace-aware `kubernetes-graphql-gateway` (`src/lib/graphql.ts`):

- SimpleCluster CRUD + change subscription — `src/lib/simplecluster.ts`
- kubeconfig Secret fetch — `src/lib/secret.ts`

### Status refresh and open details

Subscription events trigger immediate list refreshes. A five-second safety
poll also refreshes the SimpleCluster list when the stream is quiet, unavailable
or stalled. This is an additional GraphQL POST list query per timer tick while
idle, not a claim that stream health is known. Provider discovery and kubeconfig
Secrets are not polled by this timer.

Only one list request runs at a time per active context. Events during a read
queue one follow-up refresh; timer ticks skip the in-flight request. Each list
request has a cancellable 30-second deadline so a stalled read cannot block
future refreshes. This deadline applies to fetching the list, not to cluster
provisioning or deletion. A failed read keeps the last successful inventory and
shows an error; it is not treated as an empty list.

List results and open details are bound to the current gateway, workspace,
namespace and authentication context. Details follow the selected resource UID
in the latest successful list, including new conditions and kubeconfig references.
Disappearance or same-name replacement closes the details instead of selecting
a different resource. If a response lacks a UID, object identity cannot be
verified across refreshes, so reopening details may be necessary. Changing
context hides the old list/details and cancels the outstanding list request.

## Develop

```sh
npm install
npm run dev          # http://localhost:5175
VITE_FIXTURES=true npm run dev   # offline, with sample clusters
```

## Build / image

```sh
npm run build        # tsc + vite → dist/
npm test             # provider form, validation, discovery and GraphQL request tests
docker build -t ghcr.io/apeirora/simple-cluster-ui .
```

The image (nginx serving `dist/`) is published by `.github/workflows/release-please.yml`
and deployed by the `simple-cluster-ui` Helm chart, which mounts `pm-content.json`
(the Luigi node) and `simple-cluster-config.json` (the create-dialog dropdown values).

## Provider discovery and branding

Each `clusterConfig.providerSources` entry provides discovery metadata and optional
presentation for its exact `providerConfigName`. A source must first be confirmed
by live discovery; unreachable or incomplete discovery never activates the static
fallback list. Static `providers` are used only when `providerSources` is empty.
A bound resource type establishes API availability in the workspace; it does not
prove the provider's backing runtime is healthy.

The provider deployment can supply the following metadata without rebuilding the UI:

```yaml
group: example.test
version: v1
listField: Claims
namespaced: true
providerConfigName: example
label: Example Cloud
logoUrl: /logos/example.svg
```

All providers use the ordinary cluster name, worker count or autoscaling range,
CPU/memory and control-plane controls from the shared configuration. The create
request contains the provider reference and these generic resources. Provider
implementation settings belong in the provider-owned SCPC; the UI neither renders
nor submits `spec.inputs`. Deprecated per-source `inputs` and `sizingPresets`
are ignored if an older ConfigMap remains mounted during rollout. A provider form
schema generator is not part of this contract.

Generic count and dropdown validation prevents malformed UI selections. Provider
controllers remain responsible for supported machine profiles, capacity and
provider-specific constraints; displaying a size is not proof of backend support.
Control-plane fields retain the standard shared form behavior; providers decide
how their SCPC uses them.

Serve the provider's existing logo at a deployed relative path or HTTP(S) URL,
or use a self-contained `data:image/svg+xml;base64,...` URL from its SVG asset.
Other data URL formats are not accepted.

UI tests cover form behavior, discovery and request construction for the five
existing provider sources and Telekom. Real gateway serialization, authorization,
provider lifecycle and cleanup still require environment acceptance.
