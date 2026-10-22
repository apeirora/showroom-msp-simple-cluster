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

## Develop

```sh
npm install
npm run dev          # http://localhost:5175
VITE_FIXTURES=true npm run dev   # offline, with sample clusters
```

## Build / image

```sh
npm run build        # tsc + vite → dist/
docker build -t ghcr.io/apeirora/simple-cluster-ui .
```

The image (nginx serving `dist/`) is published by `.github/workflows/release-please.yml`
and deployed by the `simple-cluster-ui` Helm chart, which mounts `pm-content.json`
(the Luigi node) and `simple-cluster-config.json` (the create-dialog dropdown values).

## Encryption at rest

The create dialog offers **Encrypt Kubernetes Secrets with OpenBao (KMS)** only for a
provider whose name appears in `encryptionProviders` of `simple-cluster-config.json`
(chart value `clusterConfig.encryptionProviders`, empty by default):

```json
{ "encryptionProviders": { "cloudferro": ["OpenBao"] } }
```

The map mirrors `SimpleClusterProviderConfig.spec.encryptionProviders`, which lives on
the MSP cluster where the portal cannot read it. Ticking the box sends
`spec.encryption.provider.type` (and the optional key rotation period, a Go duration of
at least `1h`) on the new SimpleCluster; nothing is sent otherwise. The field is
immutable, so there is no edit path. The list shows an **Encryption** column and the
detail view an **Encryption at rest** row, both driven by the operator's
`EncryptionReady` condition: pending until the operator has verified the cluster,
negative when the provider config refuses the request or the provider cannot confirm
it. See [docs/openbao-kms.md](../docs/openbao-kms.md).

Only provider types the SimpleCluster API admits (today `OpenBao`) survive the config
load; anything else is dropped with a console warning.

If the gateway schema predates `spec.encryption`, the list falls back to a query without
the field (once per gateway, with a console warning; the request still shows as
"Requested" from the operator's conditions) and a create with the box ticked fails with
the gateway's error instead of creating an unencrypted cluster.
