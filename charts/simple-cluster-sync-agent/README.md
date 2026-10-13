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
