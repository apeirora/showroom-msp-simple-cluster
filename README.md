<h1 align="center">Simple Cluster</h1>

<p align="center">
  <strong>One small request, any provider, a real Kubernetes cluster — and its kubeconfig handed back to you.</strong>
</p>

<p align="center">
  <a href="https://github.com/apeirora/showroom-msp-simple-cluster/releases"><img src="https://img.shields.io/github/v/release/apeirora/showroom-msp-simple-cluster?style=flat-square&color=blue" alt="Release"></a>
  <a href="simple-cluster-operator/go.mod"><img src="https://img.shields.io/github/go-mod/go-version/apeirora/showroom-msp-simple-cluster?filename=simple-cluster-operator%2Fgo.mod&style=flat-square" alt="Go Version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-green?style=flat-square" alt="License"></a>
  <a href="https://github.com/apeirora/showroom-msp-simple-cluster/actions"><img src="https://img.shields.io/github/actions/workflow/status/apeirora/showroom-msp-simple-cluster/ci.yml?style=flat-square&label=CI" alt="CI"></a>
  <a href="https://api.reuse.software/info/github.com/apeirora/showroom-msp-simple-cluster"><img src="https://api.reuse.software/badge/github.com/apeirora/showroom-msp-simple-cluster" alt="REUSE"></a>
</p>

<p align="center">
  Part of the <a href="https://apeirora.eu/">ApeiroRA</a> Platform Mesh ecosystem
</p>

---

## What is Simple Cluster?

Simple Cluster is a Kubernetes operator that turns a small, uniform cluster
request into whatever concrete resource a chosen provider actually needs — a
Gardener `Shoot`, an OpenStack VM fleet, a KinD cluster — on this cluster or a
remote one, and syncs the resulting cluster's kubeconfig back to the requester.

The core operator knows nothing about any specific platform. Everything
provider-specific lives in data, and is applied with a dynamic client, so a
provider's resource type never has to be compiled in. **Adding a provider is
writing a YAML document, not a controller.**

**Ask for a cluster:**

```yaml
apiVersion: showroom.apeirora.eu/v1alpha1
kind: SimpleCluster
metadata:
  name: demo
spec:
  providerConfigRefs: [gardener-openstack, kind-local]
  workers:
    autoscaling: {min: 2, max: 6}
    requested: {cpu: "8", memory: 32Gi}
```

The operator picks a provider config, renders it into the platform's own
resources, waits for the cluster, fetches its kubeconfig and writes it as a
Secret next to your request.

## Key features

- **Provider-agnostic by construction** — the resource type to create is data, applied dynamically; the operator never imports a provider's API
- **Templating you already know** — Go `text/template` plus [sprig](https://masterminds.github.io/sprig/), the same stack Helm uses
- **Resource DAGs** — declare `dependsOn` and a template reads its predecessor's *live* status; `repeat` fans one entry out to N objects
- **Abstract sizing** — ask for "8 CPU, 32 GiB per node"; profiles resolve it to a concrete machine type, and refuse cleanly when nothing fits
- **Local or remote targets** — build on this cluster, or on any cluster you have a kubeconfig for
- **Kubeconfig lifecycle** — three retrieval strategies, with automatic re-issue before expiry
- **Closed input sets** — a config declares exactly which knobs a user may turn, which is what makes it safe to hand to a tenant
- **Platform Mesh integration** — APIExport, sync agent, marketplace metadata and a portal micro-frontend included
- **Self-announcing cluster extensions** — an extension publishes a ConfigMap and appears as a checkbox in the create dialog; nothing here changes

## Architecture

```
   user                     management cluster                      target cluster
                   ┌───────────────────────────────────┐   ┌──────────────────────────┐
 ┌──────────────┐  │  ┌─────────────────────────────┐  │   │                          │
 │ SimpleCluster│──┼─►│ simple-cluster-operator     │  │   │  ┌────────────────────┐  │
 │  workers     │  │  │                             │  │   │  │ resource R         │  │
 │  providerRefs│  │  │ 1. select an SCPC, lock it  │  │   │  │ (Shoot, KindCluster│  │
 │  inputs      │  │  │ 2. render its resource DAG  │──┼──►│  │  OpenStackVM, …)   │  │
 └──────────────┘  │  │ 3. apply with a dynamic     │  │   │  └─────────┬──────────┘  │
        ▲          │  │    client                   │  │   │            │ provider    │
        │          │  │ 4. watch readiness          │◄─┼───┤            ▼ operator    │
        │          │  │ 5. fetch NC's kubeconfig    │  │   │  ┌────────────────────┐  │
        │          │  └──────────────┬──────────────┘  │   │  │ new cluster NC     │  │
        │          │                 │                 │   │  └────────────────────┘  │
 ┌──────┴───────┐  │                 ▼                 │   └──────────────────────────┘
 │ Secret       │◄─┼──  <sc-name>-kubeconfig           │
 │  kubeconfig  │  │                                   │
 └──────────────┘  └───────────────────────────────────┘
```

Two custom resources in group `showroom.apeirora.eu/v1alpha1`:

- **`SimpleClusterProviderConfig`** (`scpc`) — written by an administrator.
  Declares the resources to create (arbitrary `apiVersion`/`kind` plus a Go
  template body), an optional remote target, and how to fetch the resulting
  cluster's kubeconfig.
- **`SimpleCluster`** (`simc`) — written by a user. References candidate provider
  configs, a worker pool sizing and a per-node resource ask.

Full detail in [Architecture](docs/architecture.md).

## Quick start

A complete local stack on KinD — operator, KinD provider, KubeVirt, samples:

```sh
./quick-start-kind.sh
kubectl apply -k samples/kind
kubectl get simc -w
```

Or install from the published charts:

```sh
helm upgrade --install simple-cluster-system \
  oci://ghcr.io/apeirora/charts/simple-cluster-system \
  --namespace simple-cluster-system --create-namespace
```

Then fetch a cluster's kubeconfig:

```sh
SECRET=$(kubectl get simc demo -o jsonpath='{.status.kubeconfigSecretRef.name}')
kubectl get secret "$SECRET" -o jsonpath='{.data.kubeconfig}' | base64 -d > demo.kubeconfig
kubectl --kubeconfig demo.kubeconfig get nodes
```

## Documentation

| Guide | Description |
|---|---|
| [Architecture](docs/architecture.md) | System design, the resource DAG, templating, Platform Mesh integration |
| [API reference](docs/api-reference.md) | Every field of both CRDs |
| [Provider contract](docs/provider-contract.md) | What a provider must supply, what the operator guarantees |
| [Provider implementation guide](docs/provider-guide.md) | Building a new provider, step by step |
| [Local development](docs/installation-local.md) | KinD setup, per-module build and test |
| [Helm installation](docs/installation-helm.md) | Installing the published charts |
| [GitOps deployment](docs/installation-gitops.md) | Flux HelmReleases and environment overlays |
| [OCM delivery](docs/installation-ocm.md) | Open Component Model transport |
| [Configuration](docs/configuration.md) | Every chart value and operator flag |
| [Operations](docs/operations.md) | Running the stack day to day |
| [Troubleshooting](docs/troubleshooting.md) | When it does not work |
| [Release flow](docs/release-flow.md) | CI gates, publication pipeline |
| [Versioning](docs/versioning.md) | SemVer and Conventional Commits policy |
| [ORD dependency graphs](docs/ord-dependency-graphs.md) | Provider metadata and onboarding |
| [Third-party licences](docs/third-party-licenses.md) | Dependency licence audit |
| [Contributing](CONTRIBUTING.md) | How to contribute |
| [Security](SECURITY.md) | Security policy |

## Components

| Component | Kind it owns | Role |
|---|---|---|
| `simple-cluster-operator` | `SimpleCluster`, `SimpleClusterProviderConfig` | the core reconciler |
| `simple-cluster-operator-kind` | `KindCluster` | KinD clusters via Docker-out-of-Docker |
| `simple-cluster-operator-openstack` | `OpenStackVM` | OpenStack VMs from an application credential |
| `simple-cluster-operator-ubuntu` | `UbuntuCluster` | k3s over SSH on existing Ubuntu hosts |
| `sshkey-operator` | `SSHKey` | generates and stores the keypairs the VM providers use |
| `bind-watcher-operator` | — | KCP bind discovery, annotates account namespaces |
| `ubuntu-machines` | `UbuntuMachine`, `UbuntuMachineClaim` | a claimable pool of pre-existing SSH hosts |
| `ui` | — | React + UI5 portal micro-frontend |

Branded cloud providers (Dati, SAP, Arsys, CloudFerro) live in their own
repositories and plug in through the same extension points.

## Helm charts

| Chart | Purpose | Registry |
|---|---|---|
| `simple-cluster-system` | umbrella: core operator + the in-repo provider operators | `oci://ghcr.io/apeirora/charts/simple-cluster-system` |
| `simple-cluster-ui` | portal micro-frontend | `oci://ghcr.io/apeirora/charts/simple-cluster-ui` |
| `simple-cluster-pm-integration` | APIExport, ProviderMetadata, ContentConfiguration | `oci://ghcr.io/apeirora/charts/simple-cluster-pm-integration` |
| `simple-cluster-sync-agent` | KCP api-syncagent and its PublishedResources | `oci://ghcr.io/apeirora/charts/simple-cluster-sync-agent` |
| `ubuntu-machines-operator` | SSH-host-pool operator | `oci://ghcr.io/apeirora/charts/ubuntu-machines-operator` |
| `sshkey-operator` | standalone SSH-key operator | `oci://ghcr.io/apeirora/charts/sshkey-operator` |
| `bind-watcher-operator` | KCP bind discovery | `oci://ghcr.io/apeirora/charts/bind-watcher-operator` |

## Repository layout

Monorepo of seven Go modules, each with its own `go.mod` and `Makefile`. There is
**no top-level Makefile** — work inside a module directory.

```
.
├── simple-cluster-operator/          # core SC/SCPC operator
├── simple-cluster-operator-kind/     # KindCluster provider
├── simple-cluster-operator-openstack/# OpenStackVM provider
├── simple-cluster-operator-ubuntu/   # UbuntuCluster provider (k3s over SSH)
├── sshkey-operator/                  # SSHKey operator
├── bind-watcher-operator/            # KCP bind discovery
├── ubuntu-machines/                  # SSH-host-pool operator
├── ui/                               # React + UI5 micro-frontend
├── charts/                           # Helm charts
├── samples/                          # provider configs and example requests
├── docs/                             # documentation
├── sbom/                             # CycloneDX + SPDX SBOM per component
├── hack/, scripts/                   # tooling
└── .ocm/                             # OCM component descriptor
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Pull request titles must follow
[Conventional Commits](https://www.conventionalcommits.org/) — this is enforced
by CI and drives versioning.

## Security

See [SECURITY.md](SECURITY.md). This is a demonstration project and is not
intended for production use.

## License

Licensed under the [Apache License 2.0](LICENSE). Copyright 2026 SAP SE or an SAP
affiliate company and showroom-msp-simple-cluster contributors. The repository is
[REUSE](https://reuse.software/) compliant; see [REUSE.toml](REUSE.toml) and
[docs/third-party-licenses.md](docs/third-party-licenses.md).

---

<p align="center">
  <sub>Built with care by <a href="https://apeirora.eu/">ApeiroRA</a></sub>
</p>

<p align="center">
  <a href="https://apeirora.eu/">
    <img src="docs/assets/eu-funded.svg" alt="Funded by the European Union -- NextGenerationEU. Supported by the Federal Ministry for Economic Affairs and Energy on the basis of a decision by the German Bundestag." width="400" />
  </a>
</p>
