# Contributing

## Code of Conduct

All members of the project community must abide by the [SAP Open Source Code of Conduct](CODE_OF_CONDUCT.md).
Only by respecting each other we can develop a productive, collaborative community.
Instances of abusive, harassing, or otherwise unacceptable behavior may be reported by contacting [a project maintainer](.github/CODEOWNERS).

## Engaging in Our Project

We use GitHub to manage reviews of pull requests.

* If you are a new contributor, see: [Steps to Contribute](#steps-to-contribute)

* Before implementing your change, create an issue that describes the problem you would like to solve or the code that should be enhanced. Please note that you are willing to work on that issue.

* The team will review the issue and decide whether it should be implemented as a pull request. In that case, they will assign the issue to you. If the team decides against picking up the issue, the team will post a comment with an explanation.

## Steps to Contribute

Should you wish to work on an issue, please claim it first by commenting on the GitHub issue that you want to work on. This is to prevent duplicated efforts from other contributors on the same issue.

If you have questions about one of the issues, please comment on them, and one of the maintainers will clarify.

## Contributing Code or Documentation

You are welcome to contribute code in order to fix a bug or to implement a new feature that is logged as an issue.

The following rule governs code contributions:

* Contributions must be licensed under the [Apache 2.0 License](./LICENSE)
* Due to legal reasons, contributors will be asked to accept a Developer Certificate of Origin (DCO) when they create the first pull request to this project. This happens in an automated fashion during the submission process. SAP uses [the standard DCO text of the Linux Foundation](https://developercertificate.org/).

## Issues and Planning

* We use GitHub issues to track bugs and enhancement requests.

* Please provide as much context as possible when you open an issue. The information you provide must be comprehensive enough to reproduce that issue for the assignee.

## Developing

Each operator is a separate Go module with its own `Makefile`; there is no
top-level `Makefile`. See [Local development](docs/installation-local.md) for the
per-module workflow, and the
[provider implementation guide](docs/provider-guide.md) if you are adding a way
of provisioning clusters.

Before opening a pull request:

```sh
cd <module> && make build && make vet && make test
```

If you changed dependencies, regenerate the SBOM and the licence audit — CI
fails otherwise:

```sh
./hack/generate-sbom.sh
python3 hack/generate-third-party-licenses.py
bash hack/check-sbom-current.sh
```

Run these commands from the repository root with Docker running and the declared
Go toolchain installed. The generator uses a pinned Syft image and downloads Go
modules so their licences can be read from the module cache.

### Renovate updates

Renovate proposes dependency updates using `renovate.json`. Review updates and
wait for CI before merging; automatic merging is not enabled. Dashboard:
https://github.com/apeirora/showroom-msp-simple-cluster/issues/254

Updates to shipped Go or UI dependencies also need regenerated files in `sbom/`
and `docs/third-party-licenses.md`. Run the commands above on the update branch
and commit those artifacts together with the dependency update. CI verifies the
inventory but does not commit refreshed artifacts for the bot.

Keep both CycloneDX and SPDX files for each changed component consistent.
The freshness gate compares CycloneDX dependency tuples and complete SPDX
documents, ignoring SPDX creation timestamps, document namespaces and ordering.
Run its isolated regression tests without Docker or network access with
`python3 -m unittest discover -s hack/tests -p 'test_sbom_current.py'`.
Unchanged components can retain their existing files only after confirming that
regeneration changed nothing except timestamps, serial numbers and document
namespaces. Do not hand-edit dependency versions or licence classifications in
generated files, and do not bypass compliance gates to merge an update.

The `api-syncagent` chart update is pending an upstream template fix. Its pod
version label currently copies the digest-pinned image tag, which is not a valid
Kubernetes label value. Keep the image digest pin; Helm lint alone does not
detect this API validation failure. Upstream template:
https://github.com/kcp-dev/helm-charts/blob/main/charts/api-syncagent/templates/deployment.yaml

Bind watcher retains controller-runtime and multicluster-runtime 0.24.1 with
Kubernetes libraries 0.36.4. `multicluster-provider` 0.8.0 implements the earlier
event-recorder interface and cannot compile against controller-runtime 0.25.2.
Upgrade this stack together once the provider supports the newer interface;
the other operators use the newer runtime independently.

If you changed operator scaffolding (CRDs, RBAC, the manager deployment), mirror
the change into the chart under `charts/simple-cluster-system/charts/` by hand.
CI does not do it, and `scripts/regen-helm-charts.sh` currently discards
hand-maintained chart values — see
[Local development](docs/installation-local.md#regenerating-the-charts).

## Pull request titles

Titles must follow [Conventional Commits](https://www.conventionalcommits.org/)
— `feat:`, `fix:`, `docs:`, `chore:`, and so on. This is enforced by CI and
drives the version of the next release; see [Versioning](docs/versioning.md).
The repository squash-merges, so the title becomes the commit message.

## What CI requires

Builds and tests for all seven Go modules, the UI and all eight Helm charts, plus
four compliance gates: REUSE licence compliance, a gitleaks secret scan,
`govulncheck` / `npm audit`, and a check that the committed SBOM still matches
the declared dependencies. See [Release flow](docs/release-flow.md).

## Licensing

Contributions are licensed under Apache-2.0. The repository is
[REUSE](https://reuse.software/) compliant: `REUSE.toml` covers the whole tree,
so new files need no per-file header. Third-party material added to the tree
needs its own annotation in `REUSE.toml`.
