# Versioning

## Semantic versioning

Releases follow [SemVer](https://semver.org/). The project is pre-1.0: the API is
`v1alpha1` and breaking changes may land in a minor release, though they are
called out in the changelog.

One version covers everything. Every operator image, the UI image and every Helm
chart in a release carry the same `X.Y.Z`, which is what makes a chart at version
`X.Y.Z` guaranteed to pull images at `X.Y.Z`.

The chart versions committed in `Chart.yaml` are development placeholders. The
published version is always the release version — which is why GitOps
`HelmRelease` resources pin a range (`>=0.2.0 <1.0.0`) rather than an exact
version.

## Conventional commits

Pull request titles must follow
[Conventional Commits](https://www.conventionalcommits.org/); this is enforced by
`.github/workflows/semantic-pr.yml`.

| Prefix | Effect on the next version |
|---|---|
| `fix:` | patch |
| `feat:` | minor |
| `feat!:` / `BREAKING CHANGE:` in the body | major |
| `chore:`, `docs:`, `ci:`, `refactor:`, `test:` | none |

Because the repository squash-merges, the PR title becomes the commit message,
which is what release-please reads.

## API versioning

`showroom.apeirora.eu/v1alpha1`. Alpha means fields may be added, renamed or
removed between minor releases. When a change is not backwards compatible it is
listed in the changelog under a breaking-change heading, and the release notes
say what to do.

Provider configurations are data, not code: a field added to
`SimpleClusterProviderConfig` is a new optional key that existing configs ignore.
That is the main reason the API surface has stayed small while the provider set
has grown.
