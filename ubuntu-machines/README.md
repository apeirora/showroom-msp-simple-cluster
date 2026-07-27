# ubuntu-machines

A generic SSH-machine pool operator. Admins register pre-existing Ubuntu hosts as `UbuntuMachine` resources; consumers ask for N of them via `UbuntuMachineClaim` (PV/PVC pattern). The operator is intentionally **decoupled** from any cluster technology — bound machines can be driven by anything reachable over SSH (k3s, Docker, plain VMs for testing, …).

API group: `ubuntumachines.apeirora.eu/v1alpha1` both kinds **namespace-scoped**.

## Status: currently unwired

> ⚠️ This operator is **not connected to any live flow** right now. The active
> providers wire their VM address straight into `UbuntuCluster.spec.hosts`
> (cloudferro → `OpenStackVM.status.address`, arsys → `ArsysUbuntuMachine.status.address`),
> so nothing registers `UbuntuMachine`s and nothing consumes `UbuntuMachineClaim`s
> today — there is no live producer or consumer.
>
> The "bring-your-own-SSH-host pool" SimpleCluster flavor exists only as an
> example (`samples/ubuntu-machines/` and the **unpublished** `simple-cluster-scpc-config`
> chart's `scpc-ubuntu-machines`). It ships no pm-integration / sync-agent, so it
> is **not offered in the marketplace**.
>
> The operator is kept here as a ready-to-use, provider-agnostic SSH-host-pool
> backend for when that flavor is wired up (each provider registering its VM as an
> `UbuntuMachine` and `UbuntuCluster` consuming bound claims).

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
