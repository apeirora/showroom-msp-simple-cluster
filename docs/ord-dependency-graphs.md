# ORD dependency graphs

Simple Cluster publishes descriptive metadata for each installed `SimpleClusterProviderConfig` (SCPC). Simple Cluster still controls resource creation, ordering, status, and deletion.

The operator reads all SCPC resources. It groups equivalent resources by `metadata.name` and publishes one provider option for each name.

## Discovery endpoints

The Simple Cluster UI serves these public endpoints:

- `/.well-known/open-resource-discovery`
- `/ord/simple-cluster.json`
- `/ord/provider-config-<name>.json`
- `/ui-extensions/ord/`

`ProviderMetadata.spec.data.ord.configUrl` points to the well-known endpoint. `ProviderMetadata.spec.detailViewExtensions` points to the renderer.

The Marketplace hosts the renderer through the generic Luigi extension contract. Platform Mesh does not contain ORD-specific code.

## Experimental mapping

| Simple Cluster concept | ORD 1.16.3 concept |
|---|---|
| Simple Cluster service | Product and package |
| `SimpleCluster` lifecycle API | API resource |
| Provider choice | Capability |
| SCPC readiness | Capability `disabled` value |
| SCPC resource list and DAG | Custom capability definition |
| Resource API contract | API resource plus Kubernetes `apiVersion` and `kind` |
| `dependsOn` edge | `dependsOn` entry in the definition |

The capability type is `apeirora.eu:simple-cluster-provider-config:v1`. The definition type is `apeirora.eu:simple-cluster-provider-config-definition:v1`.

The operator copies only these SCPC fields into public metadata:

- the provider configuration name;
- each resource name;
- each resource `apiVersion` and `kind`;
- each resource `dependsOn` list;
- the resource that supplies the kubeconfig;
- the aggregated readiness state.

The operator does not publish templates, inputs, credentials, endpoints, or customer resource names.

## Availability

Every observed provider configuration has an explicit `disabled` value. A provider is available when at least one equivalent SCPC reports `Ready=True` and remains reachable.

This value describes the provider installation on the MSP cluster. It does not describe account entitlement or provider capacity.

The same provider can create one SCPC in each account namespace. The operator publishes one option when all copies contain the same resource graph.

If copies contain different graphs, publication stops and the last valid ORD document remains available. Simple Cluster provisioning continues without a dependency on ORD.

## ORD and KRM limits

ORD 1.16.3 has no native model for a Kubernetes Custom Resource Definition (CRD). It also has no native model for a KRM dependency graph.

This integration uses an ORD capability and a custom definition. The definition preserves the graph without assigning standard ORD dependency semantics to KRM ordering.

The integration does not use `integrationDependencies` for SCPC edges. That concept describes integration with an external system, not controller execution order.

Provider API contracts use one ORD API resource for each `apiVersion` and `kind` pair. A future contract can add public OpenAPI schema URLs.

## Provider onboarding

A provider does not add ORD files. The provider creates its SCPC through its normal bind flow.

The SCPC must contain stable resource names, valid `dependsOn` references, and a valid `kubeconfigSource.resourceName`. The existing SCPC controller supplies readiness status.

The ORD publisher detects the SCPC during its next interval. The renderer then displays the provider option and its graph.

## Failure behavior

ORD publication runs as a separate manager runnable. A publication error does not stop the SCPC controller or the `SimpleCluster` controller.

The UI mounts the ORD ConfigMap as optional. Simple Cluster discovery and provisioning continue when the ConfigMap is absent.

The renderer limits document count, response size, fetch duration, protocols, and redirects. Invalid or unsupported documents produce an isolated extension error.

## Automated checks

The Go tests cover graph preservation, availability, duplicate SCPC copies, inconsistent graphs, empty catalogs, and deterministic updates.

The UI tests cover URL parsing, ORD loading, graph layout, invalid versions, and fallback behavior. The ORD fixture passes the official 1.16.3 schema.
