# Account child destination

SimpleCluster creates each provider child in the KCP account workspace that owns the parent.
No child kind allowlist applies.
The controller never uses the provider cluster as a child destination.
`spec.target.kcpAccount` is deprecated and has no effect.
A target kubeconfig reference returns `MigrationRequired`.

Configure `--kcp-kubeconfig` with the provider credential and `--kcp-api-export` with the SimpleCluster export name.
The chart requires `kcpAccount.enabled: true` and mounts the existing provider credential.
The controller does not obtain consumer credentials.

## Provider requirements

- Bind the child APIExport in the account.
- Publish the exact child kind through the provider sync agent, or reconcile that kind directly in KCP.
- Add the child permission claim to the SimpleCluster APIExport.
- Accept the claim in the account SimpleCluster APIBinding.
- Grant the required verbs and a selector that matches each complete child object.
- Permit read access to account APIBinding metadata.
- Return status and related Secrets to the account.

The controller checks the child binding phase, conditions and API identity hash.
It checks requested, accepted and applied permission claims.
It checks label or resource selectors against the complete object.
It reads the provider export identity with the provider credential.
It checks authorization again immediately before each create, patch and delete.
The API server also checks each operation.
Resources from the SimpleCluster export itself do not need a cross-export claim.
Native resources, such as Secrets, require explicit permission claims.

## Graph preflight and credentials

Before any child write, the controller renders and checks every independently resolvable instance.
It uses recorded predecessor objects to check dependent instances when their outputs are available.
An unresolved dependency can proceed only when both accepted and applied selectors permit every object.
A restricted selector with unresolved predecessor data stops the graph before any child write.
After predecessor outputs arrive, the controller checks each complete dependent object before its write.
It checks current permission claims again before each mutation.

Credential subresources require the parent resource's create claim.
KCP v1alpha2 does not permit a slash in permission claim resource names.
The account must advertise the exact parent/subresource with create in discovery.
A missing subresource stops graph preflight before any child write.
Immediately before the credential POST, the controller rereads the parent and checks its UID, ownership, API identity and current selectors.
It checks requested, accepted and applied create permissions again.

The SCPC CRD advertises `showroom.apeirora.eu/account-resource-contract: "1"`.
Providers that require the universal destination check this API contract before they write an SCPC.
If SCPC is exported through KCP, carry the annotation onto its bound APIResourceSchema.
Deploy the core release before the hosting release.

## Namespace and ownership

Namespaced children use the parent account namespace when no namespace is specified.
Cluster-scoped children have no namespace.
`.Account.namespace` and `.Account.cluster` identify the consumer destination.
`.SC.namespace` retains its previous meaning to preserve existing resource name hashes.
An explicit namespace must pass both the account permission claim and the Secret policy where applicable.

Children carry the parent cluster, namespace, name and UID annotations.
They have no cross-scope owner reference.
`showroom.apeirora.eu/creation-id` identifies one persisted create intent.

## Durable child records

`status.accountTarget` pins the account parent UID and `resourceTrackingVersion: 1`.
`status.accountResources` stores each child identity before its create request.
The record includes the group, version, kind, resource, namespace, name, API identity, dependencies and deletion annotations.
The controller records the returned child UID before it continues.
A lost create response can recover only an object with the same parent and creation ID.
An existing object without a matching record is never adopted implicitly.
A missing recorded child or a changed child UID requires an explicit handoff.

Deletion uses the recorded graph, including when the SCPC is absent.
It deletes dependents before predecessors and waits for their finalizers.
Metadata patches use UID and resourceVersion preconditions.
Delete requests use the recorded UID precondition.
Revoked claims, changed API identities and replacement objects retain the parent finalizer.
The controller never removes provider finalizers.

Leaf scale-down removes only excess instances.
A graph dependent blocks scale-down until an explicit provider handoff removes its dependency safely.
This rule prevents a worker-count change from deleting an UbuntuCluster bootstrap object.
Scale-up creates only the additional instances.
A removed graph resource or changed dependency requires an explicit handoff.

## Existing clusters

Existing local, remote or old account records return `MigrationRequired`.
The controller does not change their destination or create replacement infrastructure.
Pause the controllers before a provider-specific handoff.
Record the physical backend UID and the workload credential hashes.
Adopt the existing backend with an explicit UID check.
Write the reviewed account records and ownership annotations.
Orphan-delete the old custom resources after the new owner reports Ready.
Recheck the backend UID, workloads and credentials before resume.
Use the migration journal to restore the old owner if a check fails.
Never clear status to force the new route.

For hosting, the backend invariant is the Gardener Shoot UID.
The physical and KCP provider custom resource UIDs can change during the group handoff.
