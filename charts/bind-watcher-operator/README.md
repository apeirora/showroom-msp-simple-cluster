# KCP credentials

The chart mounts the whole `KCP.kubeconfig.secretName` Secret at `/etc/kcp`.
The mount is read-only and uses mode `0444`.
`KCP_KUBECONFIG` points to `/etc/kcp/kubeconfig`.

The Secret must contain `kubeconfig`, `tls.crt`, and `tls.key`.
Use these paths in the kubeconfig:

```yaml
client-certificate: /etc/kcp/tls.crt
client-key: /etc/kcp/tls.key
```

Keep the certificate authority data embedded in the kubeconfig.
The client reloads the certificate files after the Secret changes.
Embedded client credentials require a process restart.

Use `imagePullSecrets` for registry credentials:

```yaml
imagePullSecrets:
  - name: registry-auth
```

See the [consumer contract](https://github.com/apeirora/showroom/blob/main/common/msp/identities/CONSUMER.md).
