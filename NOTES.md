# Dev notes

## Go / kubectl toolchain is broken via snap
`/snap/bin/go` and `/snap/bin/kubectl` fail with:
`snap-confine is packaged without necessary permissions ... cap_dac_override not found`.
This is a system-level snap-confine issue, not the build.

Workaround used: standalone toolchain in `/tmp`:
```
curl -sL https://go.dev/dl/go1.24.5.linux-amd64.tar.gz | tar xz -C /tmp
export PATH=/tmp/go/bin:$PATH GOTOOLCHAIN=local GOCACHE=/tmp/gocache
```
For kustomize builds (kubectl broken too): standalone `kustomize` binary in `/tmp`.

## CRDs are hand-curated — do NOT blindly `controller-gen crd`
`config/crd/bases/*.yaml` and the chart `templates/crd/*.yaml` copies carry
trimmed descriptions and a curated layout. Stock `controller-gen` (any version)
re-expands every description from the Go doc comments and reorders status
fields — a huge, lossy diff. The repo's canonical version is
`controller-gen.kubebuilder.io/version: v0.19.0`.
When changing API types, prefer **surgical hand-edits** of the CRD schema to
match, or regenerate with v0.19.0 and re-apply the curation. Note: v0.17.x
rejects the `MinProperties=1` marker on `HostPool` ("must apply minproperties
to an object") — another reason to pin v0.19.0.

Chart CRDs = base CRD wrapped with `{{- if .Values.crd.enable }}` + chart
labels; schema body is byte-identical to the base, so the same edit applies to
both. `scripts/regen-helm-charts.sh` (kubebuilder helm/v1-alpha plugin)
regenerates `dist/chart` → `charts/`; the `dist/chart` copies are scratch.

## UbuntuMachine pool model (PV/PVC)
- `UbuntuMachine` is **cluster-scoped** (global pool, admin-registered).
- `UbuntuMachineClaim` stays **namespaced** (per-tenant), binds one machine.
- Because the machine is cluster-scoped, credential Secret refs are
  `corev1.SecretReference` (name + namespace), and `status.boundClaimRef`
  carries `{name, namespace}` (`ClaimReference`). Binder matches on both.

## SSH auth: key XOR password
Both `UbuntuMachine.spec.ssh` and `UbuntuCluster.spec.ssh` accept exactly one of
`privateKeySecretRef` (kubernetes.io/ssh-auth, `ssh-privatekey`) or
`passwordSecretRef` (kubernetes.io/basic-auth, `password`), enforced by a CEL
`x-kubernetes-validations` rule. The ubuntu-cluster SSH client picks the auth
method via `ssh.Credential`. Caveat: `RunSudo` still uses `sudo -n` (NOPASSWD
assumed) and password SSH needs `PasswordAuthentication yes` on the host.
