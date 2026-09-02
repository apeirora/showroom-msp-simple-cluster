# Provider implementation guide

How to add a new way of provisioning clusters. Read the
[provider contract](provider-contract.md) first — this page is the practical
walk-through of satisfying it.

Start by deciding which of the two you are building:

- **Config-only** — some controller already reconciles a resource that produces a
  cluster (a Gardener `Shoot`, a Cluster API `Cluster`, a vendor CRD). You write
  a `SimpleClusterProviderConfig` and nothing else. Go to
  [Part 1](#part-1--write-the-scpc).
- **Config + operator** — nothing reconciles what you need, so you also write a
  controller. Do [Part 1](#part-1--write-the-scpc) anyway; the SCPC is what
  defines your controller's API surface. Then [Part 2](#part-2--write-the-operator).

---

## Part 1 — write the SCPC

### Step 1: name the resources

List the objects that must exist, in dependency order. A Gardener provider needs
one (`Shoot`). A VM-based provider typically needs three:

```
sshkey ──► vms ──► cluster
```

Each becomes an entry in `spec.resources[]`, with `dependsOn` naming its
predecessors. The operator renders and creates them in dependency order and only
proceeds past a resource once it is Ready.

### Step 2: decide the target

```yaml
spec:
  target:
    namespace: my-project          # default namespace for namespaced resources
```

That creates everything on the management cluster. To build on a remote cluster,
add a Secret reference:

```yaml
spec:
  target:
    namespace: garden-my-project
    kubeconfigSecretRef:
      name: gardener-kubeconfig
      namespace: default
```

The Secret's `kubeconfig` key holds credentials for the target. The operator
smoke-tests the connection and reports the outcome in `status.reachable`, so a
broken credential is visible on the SCPC itself rather than only on the first
cluster that tries to use it.

`namespace` may be a template — `{{ .SC.namespace }}` co-locates the created
resources with the request that produced them.

### Step 3: declare the inputs

Inputs are the only knobs a user can turn. Everything else is fixed by you.

```yaml
inputs:
  - name: region
    type: enum
    default: eu-de-1
    description: Data-centre region
    values:
      - {key: eu-de-1, label: Frankfurt}
      - {key: eu-nl-1, label: Amsterdam}
  - name: kubernetesVersion
    type: string
    default: "1.31"
  - name: machineType
    from: profile
    description: Resolved from spec.profiles to fit the requested resources
```

Rules that matter:

- Every declaration needs a `default` **unless** it sets `from`.
- `type` is enforced on both the default and any user override — a `duration`
  input rejects `"soon"` with an error naming the key.
- `enum` inputs need a non-empty `values` list; the default and any override must
  match a `key`.
- A user cannot introduce a key you did not declare. That is what makes an SCPC
  safe to hand to a tenant.

### Step 4: declare the profiles

A `SimpleCluster` asks for "8 CPU, 32 GiB per node", not for `m5.2xlarge`.
Profiles are how that becomes concrete:

```yaml
profiles:
  - {name: m5.large,   cpu: "2", memory: 8Gi}
  - {name: m5.xlarge,  cpu: "4", memory: 16Gi}
  - {name: m5.2xlarge, cpu: "8", memory: 32Gi}
```

A `from: profile` input resolves to the **smallest** profile that satisfies the
request. If nothing fits, the `SimpleCluster` parks in `Phase=NoHardware` and
creates nothing — a deliberate refusal rather than a silent downgrade.

List only machine types the platform actually offers. A profile that does not
exist upstream turns a clean rejection into a failed provisioning run.

### Step 5: write the templates

Go `text/template` plus [sprig](https://masterminds.github.io/sprig/), with the
context documented in [Architecture § Templating](architecture.md#templating).
Write the `spec` (and any extra metadata); the operator supplies `apiVersion`,
`kind` and `metadata.name`.

```yaml
resources:
  - name: cluster
    apiVersion: core.gardener.cloud/v1beta1
    kind: Shoot
    template: |
      spec:
        cloudProfileName: {{ .Inputs.cloudProfile }}
        region: {{ .Inputs.region }}
        kubernetes:
          version: "{{ .Inputs.kubernetesVersion }}"
        provider:
          type: openstack
          workers:
            - name: worker
              machine:
                type: {{ .Inputs.machineType }}
              minimum: {{ .SC.workers.min }}
              maximum: {{ .SC.workers.max }}
```

Practical notes:

- Read `.SC.workers.min` and `.SC.workers.max`. `.SC.workers.nodes` is the same
  as `min` and exists for fixed-size templates.
- Reach a predecessor's live state through `.Resources.<name>` — but only for
  names in this resource's `dependsOn`.
- `missingkey=error` is on. A typo in an input name fails the render instead of
  producing an empty value.
- `env` and `expandenv` are removed from the sprig function map.

To create N objects from one entry, use `repeat`:

```yaml
  - name: workers
    apiVersion: cloudferro.eu/v1alpha1
    kind: OpenStackVM
    dependsOn: [sshkey]
    repeat: "{{ .SC.workers.min }}"
    template: |
      spec:
        flavor: {{ .Inputs.machineType }}
        keyName: {{ (index .Resources.sshkey "status").keyName }}
        hostname: {{ .SC.name }}-worker-{{ .Index }}
```

Siblings then see `.Resources.workers` as a **list**.

### Step 6: declare readiness

If your resource follows the `status.phase` convention with `Ready` / `Failed`,
you can omit this. Otherwise map its status onto the universal phase:

```yaml
    readiness:
      phasePath: status.lastOperation.state
      phaseReadyValue: Succeeded
      phaseFailedValues: [Failed, Error]
      lastOperationPath: status.lastOperation.description
      lastErrorPath: status.lastError.description
```

Fill in `lastOperationPath` and `lastErrorPath` whenever the resource offers
them. They are what turns "Provisioning" into an answer to "provisioning what?".

### Step 7: declare the kubeconfig source

This is the deliverable. Pick the strategy that matches how your platform hands
out credentials.

**A subresource** — the platform issues credentials on demand:

```yaml
kubeconfigSource:
  resourceName: cluster
  subResource:
    apiVersion: core.gardener.cloud/v1beta1
    kind: Shoot
    subResource: adminkubeconfig
    kubeconfigPath: status.kubeconfig
  validity: 24h
  renewalBefore: 6h
```

The operator re-issues before expiry and tracks the window in
`status.kubeconfigIssuedAt` / `status.kubeconfigExpiresAt`.

**A Secret** — the platform writes the kubeconfig somewhere:

```yaml
kubeconfigSource:
  resourceName: cluster
  secretRef:
    name: "{{ .Target.name }}-kubeconfig"
    namespace: "{{ .Target.namespace }}"
    dataKey: value
```

Name and namespace are templates, with the live resource in scope.

**Inline** — the kubeconfig is a field of the resource:

```yaml
kubeconfigSource:
  resourceName: cluster
  inline:
    kubeconfigPath: status.kubeconfig
```

### Step 8: handle deletion

```yaml
    deletionAnnotations:
      confirmation.gardener.cloud/deletion: "true"
```

Only set `forceCleanupOnCascade: true` where the resource owns **no external
state** — see the [contract](provider-contract.md#3-correct-deletion-behaviour).
Getting this wrong leaks infrastructure.

### Step 9: try it

```sh
kubectl apply -f my-scpc.yaml
kubectl get scpc my-provider -o jsonpath='{.status}' | jq
```

`status.reachable` must be true and `status.resources[]` must list a resolved GVK
for every entry. A missing GVK means the target cluster does not serve that type.

Then create a `SimpleCluster` against it and watch:

```sh
kubectl get simc -w
kubectl describe simc demo
```

`status.resources[].reason` and `.message` tell you which node of the DAG is
blocking and why.

---

## Part 2 — write the operator

Only if no controller reconciles what your SCPC creates.

### Scaffold

The in-repo provider operators are kubebuilder projects. Use the smallest one,
`simple-cluster-operator-kind`, as the model:

```
simple-cluster-operator-<name>/
├── api/v1alpha1/           # the CRD types
├── cmd/main.go             # manager entrypoint
├── internal/controller/    # the reconciler
├── config/crd/bases/       # generated CRDs
├── Dockerfile
├── Makefile                # build, fmt, vet, test, docker-build
└── go.mod                  # module github.com/apeirora/showroom-msp-simple-cluster/<dir>
```

Each operator is its own Go module. There is no top-level `Makefile`; you work
inside the module directory.

### Design the CRD around the SCPC

Your CRD's spec is exactly what your SCPC template needs to write. Resist adding
fields the SCPC cannot fill — they will never be set in production. Keep the spec
flat and declarative; the SimpleCluster's abstractions (profiles, autoscaling
ranges) have already been resolved to concrete values by the time your template
renders.

### Expose a phase the SCPC can read

The cheapest contract is the default convention:

```go
type MyClusterStatus struct {
    // +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Failed
    Phase string `json:"phase,omitempty"`
    // Human-readable progress, surfaced to the user through the SimpleCluster.
    LastOperation string `json:"lastOperation,omitempty"`
    LastError     string `json:"lastError,omitempty"`
}
```

Then the SCPC needs no `readiness` block at all, and `lastOperationPath` /
`lastErrorPath` point at your two strings.

### Make the kubeconfig available

Write it into a Secret whose name your SCPC can compute, or expose it on the
status for the `inline` strategy. Whichever you pick, make it available **only
once the cluster is genuinely usable** — the core operator treats the appearance
of a kubeconfig as the signal that provisioning is done.

### Honour deletion

Take a finalizer, tear the infrastructure down, drop the finalizer. A provider
that hangs on delete hangs the whole `SimpleCluster` cascade.

### Wire it into the build

1. Add the module to the `go` and `vulnerabilities` matrices in
   `.github/workflows/ci.yml`.
2. Add an image entry to the `publish-images` matrix in
   `.github/workflows/publish.yml`.
3. Add a subchart under `charts/simple-cluster-system/charts/`, or a standalone
   chart under `charts/`, and add it to the `helm` matrix in `ci.yml` and to
   `publish-charts` in `publish.yml`.
4. Add the component to `COMPONENTS` in `hack/generate-sbom.sh`, then regenerate:

   ```sh
   ./hack/generate-sbom.sh
   python3 hack/generate-third-party-licenses.py
   ```

5. Add a sample under `samples/<name>/` — an SCPC plus a `SimpleCluster` that
   uses it.

When operator scaffolding changes (CRDs, RBAC, the manager deployment), mirror
the change into the chart under `charts/simple-cluster-system/charts/` by hand.
CI does not do it for you, and `scripts/regen-helm-charts.sh` currently discards
hand-maintained chart values — see
[Local development](installation-local.md#regenerating-the-charts).

---

## Publishing a provider in its own repository

Branded providers live outside this repository and depend on the shared
building-block operators here at runtime. Such a repository ships:

- its own operator module and CRDs, if it has any;
- `charts/<name>-operator`, `<name>-pm-integration`, `<name>-sync-agent`, and a
  `<name>-ui` chart when it has its own micro-frontend;
- an SCPC emitter — either a chart template or a small controller that reacts to
  the `bind.simple-cluster.apeirora.eu/exports` annotation
  `bind-watcher-operator` maintains on account namespaces;
- its own release-please and publish workflows, publishing to
  `ghcr.io/apeirora/`.

The dependency direction is one-way: a provider repository consumes this one's
CRDs and images. Nothing here imports a provider.
