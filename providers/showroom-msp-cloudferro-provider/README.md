# showroom-msp-cloudferro-provider

CloudFerro provider for simple-cluster: a standalone, bind-triggered stack that
materialises the `cloudferro` SimpleClusterProviderConfig.

## Description

This provider ships no API types of its own. Its only controller is a
`NamespaceReconciler` that watches account namespaces: when the bind-watcher
annotation `bind.simple-cluster.apeirora.eu/exports` shows that both the
SimpleCluster and the `cloudferro` APIExports are bound to an account, it creates
the namespaced `cloudferro` SimpleClusterProviderConfig (owned by the account
namespace, so it is garbage-collected on unbind). The SCPC provisions OpenStack
VMs on CloudFerro and joins them into an Ubuntu-based k3s cluster on the local
(management) cluster — there is no remote target kubeconfig.

## Getting Started

### Prerequisites
- go version v1.25.0+
- docker version 17.03+.
- Access to a Kubernetes cluster.

### Build and test

```sh
make build
make test
```

### Run against the configured cluster

```sh
make run
```

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
