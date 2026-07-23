export type ClusterPhase =
  | 'Pending'
  | 'Provisioning'
  | 'Ready'
  | 'Failed'
  | 'Deleting'
  | string;

export interface Condition {
  type: string;
  status: 'True' | 'False' | 'Unknown' | string;
  reason?: string;
  message?: string;
  lastTransitionTime?: string;
}

export interface LocalObjectReference {
  name: string;
}

export interface ResourceRequest {
  cpu?: string | number;
  memory?: string | number;
}

export interface NodePool {
  nodes?: number;
  requested?: ResourceRequest;
}

export interface WorkerAutoscaling {
  min?: number;
  max?: number;
}

export interface WorkersPool extends NodePool {
  autoscaling?: WorkerAutoscaling;
}

export interface SimpleClusterSpec {
  providerConfigRefs?: string[];
  controlPlane?: NodePool;
  workers?: WorkersPool;
}

export interface ResourceStatus {
  name?: string;
  phase?: string;
  message?: string;
}

export interface NodeStatus {
  name: string;
  role?: string;
  externalIP?: string;
  internalIP?: string;
  ready?: boolean;
}

export interface SimpleClusterStatus {
  phase?: ClusterPhase;
  cp?: string;
  wkr?: string;
  providerConfigRef?: LocalObjectReference;
  targetCluster?: string;
  kubeconfigSecretRef?: LocalObjectReference;
  kubeconfigExpiresAt?: string;
  kubeconfigIssuedAt?: string;
  observedGeneration?: number;
  resources?: ResourceStatus[];
  conditions?: Condition[];
  nodes?: NodeStatus[];
}

export interface SimpleCluster {
  metadata: {
    name: string;
    namespace?: string;
    creationTimestamp?: string;
    resourceVersion?: string;
    uid?: string;
  };
  spec: SimpleClusterSpec;
  status?: SimpleClusterStatus;
}

// A provider CR family whose instances, once installed in the workspace,
// advertise the SimpleClusterProviderConfig names a SimpleCluster may target
// via status.providerConfigs. The create dialog discovers the live provider
// list by querying every configured source.
export interface ProviderSource {
  // Kubernetes API group, e.g. "dati.gardener".
  group: string;
  // API version, e.g. "v1alpha1".
  version: string;
  // GraphQL list field for the kind, e.g. "Shoots".
  listField: string;
  // Whether the resource is namespaced in the KCP workspace. Provider CRs are
  // projected cluster-scoped by design (projection.scope: Cluster), so this
  // defaults to false — the gateway then exposes the list field WITHOUT a
  // namespace argument, and querying with one is a schema error.
  namespaced?: boolean;
  // Optional human-facing label for the provider family.
  label?: string;
  // SimpleClusterProviderConfig name to offer once this source's type resolves
  // in the gateway schema (i.e. its APIExport is bound), independent of
  // whether any CR instance exists yet. This is what lets a freshly-installed
  // provider with zero instances still show up in the create dialog.
  providerConfigName?: string;
}

export interface ClusterConfig {
  // Static fallback provider list, used only when no providerSources are
  // configured (legacy deployments, offline/fixtures).
  providers: string[];
  // Live provider discovery sources. When non-empty, the create dialog lists
  // providers discovered from these CRs instead of the static list.
  providerSources: ProviderSource[];
  cpu: string[];
  memory: string[];
  controlPlane: {
    min: number;
    max: number;
    default: number;
  };
  workers: {
    min: number;
    max: number;
    default: number;
  };
}
