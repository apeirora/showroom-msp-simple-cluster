# simple-cluster-operator

Core operator of the simple-cluster stack: reconciles the `SimpleCluster` (SC) and
`SimpleClusterProviderConfig` (SCPC) CRDs (group `showroom.apeirora.eu/v1alpha1`),
templating an SC⊕SCPC into an arbitrary downstream resource on a local or remote
cluster and syncing the resulting cluster's kubeconfig back to the SC namespace.

> For the whole-stack picture — architecture, providers, installation,
> configuration and release procedure — see the [top-level README](../README.md).

## Getting Started

### Prerequisites
- go version v1.24.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/simple-cluster-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/simple-cluster-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the repo-root `samples/`:

```sh
kubectl apply -k ../samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k ../samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/simple-cluster-operator:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/simple-cluster-operator/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v1-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

## Contributing
Contributions go via pull requests with Conventional-Commit titles (release-please
derives the changelog and version bump from them). Before opening a PR, run
`make test` and `make lint`, and regenerate manifests with `make manifests generate`
if you changed API types.

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.



## KCP account targets

Set `SimpleClusterProviderConfig.spec.target.kcpAccount: true` to create the provider child in the request's KCP account.
This target supports cluster-scoped IronCoreShoots in `sap.eu/v1alpha1` and `apeiro.eu/v1alpha1`.
Keep `target.namespace` empty and omit `target.kubeconfigSecretRef`.
Set `kubeconfigSource.secretRef.namespace: default`.

The operator uses the existing SimpleCluster provider credential and discovers its own APIExport endpoint.
It scopes each client to the account recorded by api-syncagent on the physical SimpleCluster.
It reads the account parent UID before it creates the child.
It records the account route and child name in status before each external create.
A retry uses the same child name.
An existing child with different parent metadata causes an error.
The child has explicit parent annotations and no Kubernetes ownerReference.

The SimpleCluster APIExport needs an identity-bound permission claim for `sap.eu/ironcoreshoots`.
The account must bind the SAP export and accept the SimpleCluster export's claim.
The provider role needs `get`, `list`, `watch`, `create`, `patch`, and `delete` on `ironcoreshoots`.
It does not need SAP status writes or a credential for each account.
The patch permission supports the existing child delete guard.

Enable the operator chart with these values (inside `simple-cluster-operator` for the system chart):

```yaml
kcpAccount:
  enabled: true
  secretName: pm-kubeconfig
  apiExportName: simpleclusters.showroom.apeirora.eu
  legacyParentUIDs: []
```

The chart mounts the complete provider Secret under `/etc/kcp`, without `subPath`.
Its kubeconfig must use the mounted certificate files so certificate renewal remains effective.
The equivalent flags are `--kcp-kubeconfig`, `--kcp-api-export`, and `--kcp-legacy-parent-uids`.
The last flag accepts a comma-separated list and defaults to empty.

SAP Hosting remains responsible for the physical child and Garden Shoot.
Its sync agent returns child status and mirrors the generated access Secret to account namespace `default`.
Configure that related Secret with explicit service-to-account namespace mapping.
SimpleCluster reads this account Secret and retains its existing `<request>-kubeconfig` consumer Secret.

Deletion starts at the account child.
The parent finalizer waits until the SAP sync agent completes physical child cleanup.
SAP Hosting must finish Garden Shoot deletion before that cleanup can finish.
The operator checks parent ownership before it patches or deletes the account child.
It does not remove that child's finalizers by force.

### Temporary local compatibility

Use `legacyParentUIDs` only for existing account parent UIDs that must retain their local resources.
An existing account UID requires an account lookup on its first use.
To recover an existing local request without KCP, add its physical SimpleCluster UID instead.
This entry applies only when status records the local route, provider, and child targets in that physical namespace.
Requests without those local records still use KCP.
After a local route is recorded, reconciliation and credential renewal do not use the account client.
It records `LegacyLocal` mode and retains that route for deletion, including after configuration changes.
A recreated account parent has a different UID and uses the account target.
This feature does not adopt or migrate existing resources.

Remove each compatibility UID after the user deletes its old request and the Garden Shoot is absent.
Then recreate the request through GitOps.
Do not remove the old infrastructure by deleting an account child that SAP has already synchronized.
That deletion can delete the physical shoot.


The provider endpoint slice can advertise a shard host that differs from the credential's front-proxy host.
The operator uses the matching export's advertised endpoint with the existing provider credential and TLS configuration.
The shard host certificate must pass validation with the certificate authority (CA) from the provider credential.
It retains the URL scheme check and rejects relative URLs, embedded credentials, queries, and fragments.
