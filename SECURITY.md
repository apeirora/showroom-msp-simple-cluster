# Security Policy

## About this project

This repository contains a **demo stack** built for the
[ApeiroRA](https://apeirora.eu) architecture. It exists to showcase Platform Mesh
capabilities and is **not designed for production use**.

## Reporting vulnerabilities

If you discover a security issue, open a GitHub issue or contact the maintainers
directly. Please read the rest of this page first.

## No guarantee of security fixes

This project is provided on a best-effort basis. **No security fixes are
guaranteed.** Because this is a demonstration stack, vulnerabilities may not be
triaged, patched, or released on any particular timeline — or at all.

Use this software at your own risk and do not deploy it in environments where
security is a concern.

## What this software handles

Two things are worth knowing before you run it anywhere real:

- **Cluster credentials.** The operator reads kubeconfigs for remote target
  clusters from Secrets you supply, and writes the kubeconfigs of the clusters it
  provisions into Secrets in the requesting namespace. It also mirrors the latter
  into `SimpleCluster.status.kubeconfig`, so **anyone who can `get` a
  `SimpleCluster` can read its cluster's credentials** — the same audience that
  can already read the Secret in that namespace. If that is not the boundary you
  want, restrict `get` on `simpleclusters`.
- **Templates run with the operator's permissions.** A
  `SimpleClusterProviderConfig` can create arbitrary resource types on the target
  cluster. It is an administrator-facing resource and should be treated as
  privileged configuration. Users are constrained to the closed input set a
  config declares, and sprig's `env` and `expandenv` are removed so a template
  cannot read the operator pod's environment — but the config itself is trusted.

## What is scanned

Every pull request runs a secret scan (gitleaks), a vulnerability scan
(`govulncheck` for each Go module, `npm audit` for the UI), a REUSE licence check
and a dependency-licence gate. The dependency inventory is committed under
[`sbom/`](sbom) and audited in
[`docs/third-party-licenses.md`](docs/third-party-licenses.md).
