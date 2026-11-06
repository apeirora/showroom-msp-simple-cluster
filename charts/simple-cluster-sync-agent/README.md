# KCP credentials

The chart mounts the whole `pm-kubeconfig` Secret at `/etc/kcp`.
The mount is read-only and uses mode `0444`.
The final `--kcp-kubeconfig` flag selects `/etc/kcp/kubeconfig`.
The upstream chart also mounts its default kubeconfig directory.

The Secret must contain `kubeconfig`, `tls.crt`, and `tls.key`.
The kubeconfig must reference `/etc/kcp/tls.crt` and `/etc/kcp/tls.key`.
Keep the certificate authority data embedded in the kubeconfig.

Set both Secret names when you use a different Secret:

```yaml
syncAgentOperator:
  kcpKubeconfig: custom-kcp
  extraVolumes:
    - name: kcp-credentials
      secret:
        secretName: custom-kcp
        defaultMode: 0444
```

Keep the default `extraVolumeMounts` and `extraFlags` entries when you add values.
Use `global.imagePullSecrets` for registry credentials:

```yaml
global:
  imagePullSecrets:
    - registry-auth
```

See the [consumer contract](https://github.com/apeirora/showroom/blob/main/common/msp/identities/CONSUMER.md).

## Agent version

The default image is api-syncagent v0.7.0 by tag. The subchart writes the tag into the `app.kubernetes.io/version` pod label, so a digest cannot be part of it; the matching digest is noted in `values.yaml`.
This matches the Showroom deployment.
The Helm dependency supplies the deployment template.
The image value selects the agent version.

## Account child APIs

The same APIExport includes SimpleCluster, SSHKey, and UbuntuCluster.
Sync Agent generates the APIResourceSchemas from the installed CRDs.
Both child APIs remain namespaced in KCP.
Their status subresources return provider status to the account.
SSHKey returns its public key in `status.publicKey`.
The existing top-level `publicKey` remains available on the provider object.
The private key returns through the related Secret only.

Physical namespaces use the following names:

- SSHKey: `sk-<first 40 hex characters of SHA256(cluster/namespace)>`.
- UbuntuCluster: `uc-<first 40 hex characters of SHA256(cluster/namespace)>`.

Here, `cluster` is the KCP logical cluster name.
The `namespace` is the resource namespace in that account, normally `default`.
Object and Secret names stay the same.
The separate namespaces prevent a credential copy from changing its source Secret.

Each related Secret uses an explicit `metadata.namespace` map on both sides.
SSHKey exports its key Secret and imports its optional passphrase Secret.
UbuntuCluster imports the SSH credential and each host password Secret.
UbuntuCluster exports its kubeconfig Secret.
Credential references must omit the namespace or use the resource namespace in the account.
The provider rejects other namespaces before a Secret read or SSH connection.
The provider also verifies the namespace against the metadata from Sync Agent.
Local objects retain the existing Secret policy.

These rules do not adopt existing provider resources.
Move existing resources only through a separate, reviewed migration.
