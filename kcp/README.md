# KCP manifests

Objects that belong in KCP rather than on the MSP cluster, so they are not installed by
any chart in `charts/`. Apply them with a kubeconfig pointing at the workspace named below.

## `child-delete-guard.yaml`

Refuses `DELETE` of anything the simple-cluster-operator rendered for a SimpleCluster —
every such object carries `showroom.apeirora.eu/simple-cluster=<name>`. Without it a user
can delete a Shoot or a VM straight out of the portal: the object leaves KCP while the real
infrastructure on MSP keeps running, and the mirror reappears hours later when the agent
next reconciles its source.

Apply one copy into each workspace that owns an APIExport whose resources the operator
renders:

```sh
for ws in dati sap arsys cloudferro; do
  kubectl --server "$KCP/clusters/root:providers:$ws" apply -f kcp/child-delete-guard.yaml
done
```

One copy per export covers every account that binds it, now and in the future, because kcp
resolves policies for a bound resource from its APIExport's workspace. Do not apply it to
`root:providers:cluster-releases`: removing a ClusterRelease is how a user turns an addon
off, and that is a legitimate delete.

Deletion stays possible for the SimpleCluster itself — it comes from a different export and
carries no such label — and for anything annotated
`showroom.apeirora.eu/allow-delete: "true"`. That annotation is how the cascade gets
through: the operator stamps a child before deleting it on MSP, and showroom-sync-agent
carries the stamp onto the KCP mirror. A human can set it by hand to break glass.

The `not-the-apiserver-itself` match condition is not decoration. kcp tears down namespaces
and collects owner-referenced objects under its own loopback identity; refusing it leaves a
namespace `Terminating` forever, measured on cc-d2.
