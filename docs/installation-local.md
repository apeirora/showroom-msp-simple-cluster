# Local development environment

A complete stack on a KinD cluster: the core operator, the KinD provider
operator, KubeVirt, and the sample provider configurations. Everything is built
locally and side-loaded — nothing is pushed to a registry.

## Prerequisites

| Tool | Notes |
|---|---|
| [KinD](https://kind.sigs.k8s.io/) | creates the management cluster |
| `kubectl` | |
| Docker | images are built locally and loaded into KinD |
| Go | version from each module's `go.mod` (currently 1.25) |
| Helm | only for the chart-based install |

## One command

```sh
./quick-start-kind.sh
```

This boots a KinD cluster named `simple-cluster`, installs KubeVirt, builds and
side-loads `simple-cluster-operator:dev` and `kind-cluster-operator:dev`, and
waits for both deployments to become Available.

Override the defaults with environment variables:

```sh
CLUSTER_NAME=demo IMG=simple-cluster-operator:local ./quick-start-kind.sh
```

`KUBEVIRT_VERSION` pins KubeVirt; by default the latest release is used.

When it finishes:

```sh
kubectl get scpc,simc,kindclusters -A
kubectl apply -k samples/kind          # a KinD SCPC plus a SimpleCluster using it
kubectl get simc -w
```

Tear down:

```sh
kind delete cluster --name simple-cluster
```

## Running an operator from source

Each operator is a separate Go module with its own `Makefile`; there is **no
top-level Makefile**. Work inside the module directory.

```sh
cd simple-cluster-operator
make build               # go build -o bin/manager cmd/main.go
make run                 # run against the current kubecontext
make test                # unit and envtest integration tests
make lint                # golangci-lint
make manifests generate  # controller-gen: CRDs, RBAC, deepcopy
```

`simple-cluster-operator` and `sshkey-operator` are full kubebuilder projects and
expose the whole set, including `make test-e2e` (a Ginkgo suite that creates and
destroys its own KinD cluster). The reduced modules —
`simple-cluster-operator-kind`, `-openstack`, `-ubuntu`, `ubuntu-machines` —
expose `build`, `fmt`, `vet`, `test`, `docker-build`, `install`, `deploy`.
`bind-watcher-operator` adds `lint` and `build-image`.

To run the core operator against a cluster where the CRDs are not yet installed:

```sh
cd simple-cluster-operator
make install     # apply the CRDs
make run
```

## The UI

```sh
cd ui
npm ci
npm run dev        # vite dev server
npm run typecheck
npm run test:ord   # validates the ORD documents and the renderer
npm run build
```

## Toolchain versions

Go 1.25 for every module; controller-gen `v0.19.0`, kustomize `v5.7.1` and
golangci-lint `v2.4.0`, all auto-installed into each module's `bin/` on first
use.

## Regenerating the charts

The operator charts under `charts/simple-cluster-system/charts/` are derived from
each module's kubebuilder scaffolding. After changing CRDs, RBAC or the manager
deployment:

```sh
./scripts/regen-helm-charts.sh
```

This is **not** run in CI — regenerate and commit the result yourself.

## Regenerating the SBOM

After changing any dependency:

```sh
./hack/generate-sbom.sh
python3 hack/generate-third-party-licenses.py
```

CI fails if `sbom/` or `docs/third-party-licenses.md` has drifted from the
declared dependencies. Requires Docker.
