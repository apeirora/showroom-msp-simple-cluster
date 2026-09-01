# Open-source readiness — scan report

Record of the pre-publication review of this repository: what was scanned, what
was found, and how each finding was resolved. Kept in the repository so the
result is auditable after publication.

## Scope and method

| | |
|---|---|
| Reviewed tree | `open-source` branch, based on `main` at `99a631e9` (v0.37.1) |
| History | all 497 commits reachable from all refs, back to `86a457aa` (2026-05-19) |
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
| F-03 | Medium | AI-assistant attribution trailer in 3 commit messages (`29ab665c`, `271f5b15`, `d2949503`) | **Resolved** by history rewrite before publication. |
| F-04 | Medium | Internal landscape identifiers `cc-d2` / `cc-two`, Gardener project namespace `garden-pm-cc-d2` | **Resolved.** Replaced with neutral placeholders across `README.md`, `kcp/`, `charts/simple-cluster-pm-integration/Chart.yaml` and the Gardener samples. |
| F-05 | Medium | Real public IPv4 addresses of showroom hosts (`34.58.121.214`, `46.60.23.143`, `46.60.23.242`, `46.60.18.73/.74`) | **Resolved.** Replaced with RFC 5737 documentation addresses (`203.0.113.0/24`) in samples, docs and unit tests. |
| F-06 | Medium | CloudFerro tenant network identifier `cloud_080745_1` used as a sample default | **Resolved.** Replaced with `<your-neutron-network>`. |
| F-07 | Low | Commit message `29ab665c` names an internal availability zone, a live shoot (`sh-c8900d38`) and an issue in the private `apeirora/showroom` repository | **Resolved** by history rewrite before publication. |
| F-08 | Low | Developer-local kubeconfig path and personal GHCR namespace in `bind-watcher-operator/hack/{experiment.sh,deploy.yaml}` | **Resolved.** Defaults now point at `$HOME/.kube/` and `ghcr.io/apeirora/`. |
| F-09 | Low | Internal `TODO.md` at the repository root | **Resolved.** Removed; the items were tracked work, not user-facing documentation. |
| F-10 | Low | Repository `.gitignore` did not exclude `.claude/`, `.notes/`, `*.env`, `*openrc*` or key files — those paths stayed untracked only because of a maintainer's personal global gitignore | **Resolved.** All added to the repository `.gitignore`. |
| F-11 | Info | OpenStack application-credential files present in the untracked working tree (`.claude/openstack-cf/.env`, `*-openrc-app-credentials.sh`) | **Never committed** — verified with `--diff-filter=A` across all refs. Credentials to be revoked in the OpenStack project as a precaution; see *Open handoff items*. |
| F-12 | Info | A live CloudFerro application credential (`OS_APPLICATION_CREDENTIAL_ID` / `_SECRET`) sits in a maintainer's working copy at `samples/openstack/openstack-creds-secret.yaml` | **Never committed** — the file is excluded by `samples/openstack/.gitignore`, and `--diff-filter=A` across all refs confirms it was never added. The credential is to be revoked; see *Open handoff items*. |
| F-13 | Info | `imports/` holds real Gardener kubeconfigs with live bearer tokens in a maintainer's working copy | **Never committed** — `imports/` has been in `.gitignore` since it was created, confirmed by `--diff-filter=A` across all refs. Tokens to be rotated; see *Open handoff items*. |

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

## Open handoff items

These cannot be closed from inside the repository and are tracked with the
publication ticket:

1. **Revoke credentials** that existed outside the repository but alongside it:
   the OpenStack application credentials in F-11 and F-12, the Gardener
   kubeconfig tokens in F-13, and the two demo SSH keys in F-01/F-02.
2. **Enable GitHub secret scanning and push protection** on the repository
   (available once it is public).
3. **Make the CI gates required** — REUSE, secret scan, vulnerability scan,
   dependency licence check — through the branch-protection ruleset.
4. **Four-eye peer review** of this change set, with the outcome recorded in the
   publication ticket.
5. **Flip the repository to public**, after 1–4 are done.
