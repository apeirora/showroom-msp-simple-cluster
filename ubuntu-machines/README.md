# ubuntu-machines

A generic SSH-machine pool operator. Admins register pre-existing Ubuntu hosts as `UbuntuMachine` resources; consumers ask for N of them via `UbuntuMachineClaim` (PV/PVC pattern). The operator is intentionally **decoupled** from any cluster technology — bound machines can be driven by anything reachable over SSH (k3s, Docker, plain VMs for testing, …).

API group: `ubuntumachines.apeirora.eu/v1alpha1` both kinds **namespace-scoped**.

## CRDs

### `UbuntuMachine` (admin-managed)

One host. The operator never opens an SSH connection — it only tracks ownership.

```yaml
apiVersion: ubuntumachines.apeirora.eu/v1alpha1
kind: UbuntuMachine
metadata:
  name: cp-0
  labels:                             # matched by claim selectors
    role: cp
    site: msp-lab
spec:
  address: 10.128.0.10                # internal IP — required
  externalAddress: 34.58.121.214      # public IP — optional, required for claims with externalAddressRequired=true
  ssh:
    user: ubuntu
    privateKeySecretRef: { name: lab-key }   # Secret in same namespace, key `ssh-privatekey`
  capacity:                                 # zero/omitted = unconstrained
    cpu: "2"
    memory: 4Gi
```

`status.phase` is `Available` or `Bound`; `status.boundClaimRef` points at the owning claim when bound.

### `UbuntuMachineClaim` (consumer-managed)

"Give me one machine matching these labels and resources." Mirrors PVC: one claim binds exactly one `UbuntuMachine`. To request N machines, create N claims.

```yaml
apiVersion: ubuntumachines.apeirora.eu/v1alpha1
kind: UbuntuMachineClaim
metadata:
  name: cp-0
spec:
  externalAddressRequired: true       # only machines with a non-empty externalAddress qualify
  required:                           # capacity.cpu/memory ≥ required.cpu/memory
    cpu: 2
    memory: 2Gi
```

`status.phase` is `Pending` or `Ready`. On `Ready`, `status.machine` carries the embedded snapshot of the bound machine (name, address, externalAddress, ssh).

The claim publishes a `Ready` Condition under `status.conditions` — `True` when bound, `False` with `reason: NoEligibleMachine | BindConflict` while pending. Matches `simple-cluster-operator`'s default SCPC readiness probe, so a `ResourceSpec` referencing an `UbuntuMachineClaim` doesn't need an explicit `readiness:` block.

The binder picks the first eligible machine sorted by name — no selector, no label filtering. Differentiate claims via `externalAddressRequired` and `required.cpu/memory` only. If you need finer routing (e.g. "GPU only"), reintroducing a selector later is the path.
