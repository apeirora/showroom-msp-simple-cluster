# Release flow

Everything ships in lockstep at one version: all operator images, the UI image
and all Helm charts.

## The pipeline

1. **Merge to `main`** with a Conventional-Commit PR title, enforced by
   `.github/workflows/semantic-pr.yml`.
2. **`release-please.yml`** maintains a release pull request — one tracked package
   (the repository root, `release-type: go`, plain `vX.Y.Z` tags, version in
   `.release-please-manifest.json`). It pushes the tag as the
   `apeirora-showroom-bot` GitHub App, because the tag ruleset restricts tag
   creation to that App.
3. **Merging the release PR** creates the tag and the GitHub release, and calls
   `publish.yml` with the new version.
4. **`publish-images`** builds and pushes eight images to
   `ghcr.io/apeirora/<image>:X.Y.Z` and `:latest`:
   `simple-cluster-operator`, `kind-cluster-operator`,
   `simple-cluster-operator-openstack`, `simple-cluster-operator-ubuntu`,
   `ubuntu-machines`, `sshkey-operator`, `bind-watcher-operator`,
   `simple-cluster-ui`.
5. **`publish-charts`** runs after the images exist. It stamps
   `global.imageTag=X.Y.Z`, resolves chart dependencies, then packages and pushes
   seven charts at `--version` and `--app-version X.Y.Z` to
   `oci://ghcr.io/apeirora/charts`. A chart at `X.Y.Z` therefore always pulls
   images at `X.Y.Z`. `simple-cluster-scpc-config` is not published.
6. **`publish-sbom`** regenerates the SBOM set at the release version and attaches
   it to the GitHub release.
7. **`publish-ocm`** publishes the OCM component version naming every image and
   chart of that release — see [OCM delivery](installation-ocm.md).

`publish.yml` can also be run manually via `workflow_dispatch` with an explicit
version, to re-publish a release.

## CI gates

Every pull request must pass:

| Job | Checks |
|---|---|
| `go` (matrix) | `make build`, `make vet`, `make test` for all seven Go modules |
| `lint` | golangci-lint on modules carrying a configuration |
| `ui` | `npm ci`, type-check, ORD validation, build |
| `helm` (matrix) | dependency update, lint, template and package for all eight charts |
| `charts-ord` | ORD chart wiring |
| `reuse` | REUSE / SPDX compliance |
| `secrets` | gitleaks |
| `vulnerabilities` (matrix) | `govulncheck` per Go module |
| `vulnerabilities-ui` | `npm audit --omit=dev --audit-level=high` |
| `dependencies` | the committed SBOM and licence audit still match the declared dependencies |

## Before a release

- **Mirror scaffolding changes into the charts** by hand when CRDs, RBAC or the
  manager deployment changed. CI does not do it, and
  `scripts/regen-helm-charts.sh` currently discards hand-maintained chart values
  — see [Local development](installation-local.md#regenerating-the-charts).
- **Regenerate the SBOM** when dependencies changed:
  `./hack/generate-sbom.sh && python3 hack/generate-third-party-licenses.py`.
  The `dependencies` gate fails otherwise.

## Versioning policy

See [Versioning](versioning.md).
