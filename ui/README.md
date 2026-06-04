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
