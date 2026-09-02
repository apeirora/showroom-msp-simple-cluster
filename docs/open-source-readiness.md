# Open-source readiness — scan report

Record of the pre-publication review of this repository: what was scanned, what
was found, and how each finding was resolved. Kept in the repository so the
result is auditable after publication.

## Scope and method

| | |
|---|---|
| Reviewed tree | `open-source` branch, based on `main` at `99a631e9` (v0.37.1) |
| History | every commit reachable from every ref at scan time — 497 then, 534 once this branch was added — back to `86a457aa` (2026-05-19) |
| Date | 2026-09-01 |

Passes run:

1. **`gitleaks`** (`--log-opts="--all --full-history"`) over the complete object
   graph — 400 MB scanned, 25 raw hits, triaged below.
2. **Path archaeology** — `git log --all --diff-filter=A` for every path that
   could ever have carried credentials (`imports/`, `.claude/`, `.notes/`,
   `*kubeconfig*`, `*.env`, `*openrc*`, `*.pem`, `*.key`, `id_rsa*`,
   `id_ed25519*`, `*.p12`, `*.pfx`, `*secret*`, `*credential*`), including every
   historical revision of each file that matched.
3. **Targeted greps** over the working tree for public IPv4 addresses, internal
   landscape and tenant identifiers, personal e-mail addresses, developer-local
   filesystem paths and AI-assistant artefacts.
4. **Commit-message scan** across all refs for the same classes of content.
5. **`reuse lint`** for licensing and SPDX coverage, and **`syft`** for the
   dependency inventory behind [third-party-licenses.md](third-party-licenses.md).

## Findings

| # | Severity | Finding | Resolution |
|---|---|---|---|
| F-01 | High | Real OpenSSH ed25519 **private key** committed in `samples/openstack/openstack-sample-ssh-secret.yaml` | **Resolved.** Key material removed from the working tree; the sample now carries a placeholder and an `ssh-keygen` recipe. Redacted from history in the publication rewrite. Key was demo-only and never granted access to anything beyond throwaway showroom VMs; it is treated as compromised and must not be reused. |
| F-02 | High | Real OpenSSH ed25519 **private key** committed in `samples/ubuntu-cluster-standalone/sshkey-sample-prv-key.yaml` (historic paths `samples/ubuntu/`, `samples/ubuntu-machines-cluster/`) | **Resolved.** Same treatment as F-01. |
| F-03 | Medium | AI-assistant attribution trailer in 3 commit messages (`29ab665c`, `271f5b15`, `d2949503`) | **Resolved** by the history rewrite in [`hack/history-rewrite/`](../hack/history-rewrite/README.md). |
| F-04 | Medium | Internal landscape identifiers `cc-d2` / `cc-two`, Gardener project namespace `garden-pm-cc-d2` | **Resolved.** Replaced with neutral placeholders across `README.md`, `kcp/`, `charts/simple-cluster-pm-integration/Chart.yaml` and the Gardener samples. |
| F-05 | Medium | Real public IPv4 addresses of showroom hosts (`34.58.121.214`, `46.60.23.143`, `46.60.23.242`, `46.60.18.73/.74`) | **Resolved.** Replaced with RFC 5737 documentation addresses (`203.0.113.0/24`) in samples, docs and unit tests. |
| F-06 | Medium | CloudFerro tenant network identifier `cloud_080745_1` used as a sample default | **Resolved.** Replaced with `<your-neutron-network>`. |
| F-07 | Low | Commit message `29ab665c` names a live shoot (`sh-c8900d38`), the cluster it belonged to, and issues in the private `apeirora/showroom` repository | **Resolved** by the history rewrite: those names are replaced with placeholders. The availability-zone names stay — they are ordinary Gardener sample defaults that also appear in the tree. |
| F-08 | Low | Developer-local kubeconfig path and personal GHCR namespace in `bind-watcher-operator/hack/{experiment.sh,deploy.yaml}` | **Resolved.** Defaults now point at `$HOME/.kube/` and `ghcr.io/apeirora/`. |
| F-09 | Low | Internal `TODO.md` at the repository root | **Resolved.** Removed; the items were tracked work, not user-facing documentation. |
| F-10 | Low | Repository `.gitignore` did not exclude `.claude/`, `.notes/`, `*.env`, `*openrc*` or key files — those paths stayed untracked only because of a maintainer's personal global gitignore | **Resolved.** All added to the repository `.gitignore`. |
| F-11 | Info | OpenStack application-credential files present in the untracked working tree (`.claude/openstack-cf/.env`, `*-openrc-app-credentials.sh`) | **Never committed** — verified with `--diff-filter=A` across all refs. Credentials to be revoked in the OpenStack project as a precaution; see *Open handoff items*. |
| F-12 | Info | A live CloudFerro application credential (`OS_APPLICATION_CREDENTIAL_ID` / `_SECRET`) sits in a maintainer's working copy at `samples/openstack/openstack-creds-secret.yaml` | **Never committed** — the file is excluded by `samples/openstack/.gitignore`, and `--diff-filter=A` across all refs confirms it was never added. The credential is to be revoked; see *Open handoff items*. |
| F-13 | Info | `imports/` holds real Gardener kubeconfigs with live bearer tokens in a maintainer's working copy | **Never committed** — `imports/` has been in `.gitignore` since it was created, confirmed by `--diff-filter=A` across all refs. Tokens to be rotated; see *Open handoff items*. |
| F-14 | Info | `govulncheck` reported known CVEs in every Go module (`GO-2026-5970`, `-5026`, `-5019`, `-5020`, `-6061`) | **Resolved.** `golang.org/x/text`, `x/net`, `x/crypto` and `google.golang.org/grpc` bumped to their fixed versions across all seven modules; `govulncheck` is now clean everywhere. |
| F-15 | Medium | The history rewrite changes every commit SHA, so every `commit/<sha>` and `compare/<sha>...<sha>` link in the changelogs would break | **Resolved.** All 141 links in `CHANGELOG.md` and `simple-cluster-operator/CHANGELOG.md` were rewritten against the rewrite's SHA translation map, which is committed at [`hack/history-rewrite/commit-map.txt`](../hack/history-rewrite/commit-map.txt). Every SHA the rewrite touched resolves again. |
| F-16 | Low | Four commit links in `simple-cluster-operator/CHANGELOG.md` point at SHAs that never existed in this repository | **Pre-existing, not caused by the rewrite** — verified with `git cat-file` against the history as it stood before. Left as is: the changelog text is accurate and the entries are reachable by their release tag. |

## Verified clean

- `imports/`, `.claude/`, `.notes/`, `*.env`, `*openrc*`, `*.pem`, `*.key`,
  `id_rsa*`, `id_ed25519*`, `*.p12`, `*.pfx` — **never added in any commit on any
  ref**.
- `samples/gardener/*-kubeconfig-secret.yaml` (3 files, every revision) —
  placeholder text `Gardener Kubeconfig here.`, no credential material.
- `samples/ironcore/ironcore-creds-secret.yaml` — the base64 blob decodes to the
  literal string `# Replace with your IronCore cluster kubeconfig`.
- gitleaks `private-key` hits in `providers/*/ui/src/lib/secret.ts` (history
  only) — test-fixture template literals, no key material.
- gitleaks `generic-api-key` hits (14, history only) in
  `providers/showroom-msp-dati-provider/ui/dist/assets/messagebundle_*.js` — SAP
  UI5 accessibility message-bundle keys in built assets, not credentials.
- No AI-assistant references in any tracked file; no `CLAUDE.md`, `.claude/`,
  `.cursorrules` or `AGENTS.md` was ever committed.
- One e-mail address in tracked content, `ubuntu-cluster@sample.com`, synthetic.
- No copyleft dependencies; see [third-party-licenses.md](third-party-licenses.md).
- A final `gitleaks` pass over a clean `git archive` of the publication tree
  reports **no findings**.

## Deliberate non-findings

- **`simple-cluster-operator-kind` declares module path
  `…/kind-cluster-operator`**, which does not match its directory name. This is
  intentional, not stale: the module path matches the published image
  (`ghcr.io/apeirora/kind-cluster-operator`) and the chart name. Renaming it
  would break that alignment, so it was left alone.

## Known issues found during the review

Not publication blockers, but recorded so a reader of the public repository is
not misled:

- **`scripts/regen-helm-charts.sh` is destructive.** It deletes each chart
  directory and regenerates it from kubebuilder scaffolding, discarding the
  hand-maintained additions those charts have grown (the `addonCatalog` and
  `ordCatalog` values and the manager arguments they render, and the
  `global.imageTag` coalesce the umbrella relies on), and it emits a stray CRD
  for the OpenStack operator. The script and the documentation now say so;
  scaffolding changes must be mirrored into the charts by hand until it is fixed.
- **Committed generated files are slightly stale.** Running `make build` in
  `simple-cluster-operator` reorders `zz_generated.deepcopy.go` and rewraps two
  CRD descriptions. Left unchanged here because the chart's copy of the CRD can
  only be brought back into step through the broken regeneration script above.

## Publication runbook

The remaining steps cannot be completed from inside the repository. **The order
matters** — in particular, no pull request may be open when the history rewrite
is pushed, because a mirror push rewrites every branch and tag and would strand
that pull request's review comments on commits that no longer exist.

Tracked in publication ticket: `<TICKET>` *(fill in before starting)*.

1. **Four-eye peer review** of this change set — the pull request from
   `open-source` into `main` — in a meeting, with the outcome recorded in the
   ticket. Then **merge it**.
2. **Apply the history rewrite** with no pull request open:

   ```sh
   hack/history-rewrite/apply-history-rewrite.sh <this-repo> /tmp/rewrite.git
   ```

   Inspect the result, then push as the script instructs. The tag ruleset
   restricts tag creation to the `apeirora-showroom-bot` App and must be relaxed
   for the duration of the push and restored immediately afterwards. Every clone
   must be re-cloned afterwards.

   The old-to-new SHA translation the rewrite produces is committed at
   [`hack/history-rewrite/commit-map.txt`](../hack/history-rewrite/commit-map.txt);
   the changelog links were already repaired against it (F-15). Re-running the
   rewrite regenerates the map, so refresh that file if the branch has moved on.

   Confirm afterwards that release-please still resolves the latest release and
   that the 77 tags were re-created.
3. **Revoke credentials** that lived alongside the repository but never in it:
   the OpenStack application credentials in F-11 and F-12, the Gardener
   kubeconfig tokens in F-13, and the two demo SSH keys in F-01/F-02.
4. **Flip the repository to public.**
5. **Enable GitHub secret scanning and push protection** — only available once
   the repository is public.
6. **Make the CI gates required** through the branch-protection ruleset: the
   `go`, `helm`, `ui` and `lint` jobs plus the four gates — REUSE compliance,
   secret scan, vulnerability scan and the dependency-licence check.

Two publication jobs, `publish-sbom` and `publish-ocm`, have never executed —
they run only on a release. Everything else in CI was verified locally before
this change set was proposed.
