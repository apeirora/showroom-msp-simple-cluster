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

export interface EncryptionProviderSpec {
  /** Key management provider type, e.g. "OpenBao". */
  type: string;
  /** Go duration, at least 1h. Absent means no automatic key rotation. */
  keyRotationPeriod?: string;
}

/**
 * Opt-in to encryption at rest of the cluster's Secrets, from the first write.
 * Immutable: the API refuses to add, remove or change it after creation.
 */
export interface EncryptionSpec {
  provider: EncryptionProviderSpec;
}

export interface SimpleClusterSpec {
  providerConfigRefs?: string[];
  controlPlane?: NodePool;
  workers?: WorkersPool;
  encryption?: EncryptionSpec;
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
    deletionTimestamp?: string;
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
  // Kubernetes API group, e.g. "dati.eu".
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

export interface KeyStoreInfo {
  // Display name of the store; the provider type name is used when absent.
  name?: string;
  // Address of the key store, e.g. https://openbao.cc-d2.showroom.apeirora.eu
  address?: string;
  // Tenancy namespace inside the store that holds this platform's keys.
  namespace?: string;
  // How keys are laid out, e.g. "one Transit key per cluster, created with
  // the cluster and removed with it".
  keys?: string;
}

export interface ClusterConfig {
  // Static fallback provider list, used only when no providerSources are
  // configured (legacy deployments, offline/fixtures).
  providers: string[];
  // Live provider discovery sources. When non-empty, the create dialog lists
  // providers discovered from these CRs instead of the static list.
  providerSources: ProviderSource[];
  // Key management providers each SimpleClusterProviderConfig admits for
  // spec.encryption, keyed by provider config name (the value of
  // providerConfigRefs). Mirrors SimpleClusterProviderConfig
  // .spec.encryptionProviders, which lives on the MSP cluster where the portal
  // cannot read it, so it is deployment configuration. A provider without an
  // entry gets no encryption control. The operator still refuses a request the
  // provider config does not admit, so this only decides what is offered.
  encryptionProviders: Record<string, string[]>;
  // Where the keys of each key management provider type live, keyed by the
  // type name (OpenBao). Shown read-only next to the encryption control and in
  // the detail view so "where are the keys stored?" has an answer on screen.
  // Deployment configuration like encryptionProviders: the portal cannot read
  // the operator's OpenBao settings. Missing entries show a generic sentence.
  encryptionKeyStores: Record<string, KeyStoreInfo>;
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

/**
 * One installable extension, as its own chart published it. Mirrors the ConfigMap the
 * operator aggregates: the two live in different repositories, so the contract is the
 * document, not a shared type.
 */
export interface AddonCatalogEntry {
  /** Addon id, e.g. "crypto-broker". */
  id: string;
  /** The label to set on the cluster object. Setting it is the whole install action. */
  label: string;
  displayName?: string;
  description?: string;
  /** APIBinding an account needs for this extension. */
  bindName?: string;
  /** Fallback: a Kind that only resolves for an account that has the extension. */
  entitlement?: {
    group: string;
    version: string;
    listField: string;
    namespaced?: boolean;
  };
  /** Cluster kinds that may carry the label. Absent means "any". */
  targetKinds?: Array<{ apiVersion: string; kind: string }>;
}

/** 'unknown' is not 'not-entitled': one is a failed check, the other a real answer. */
export type EntitlementState = 'entitled' | 'not-entitled' | 'unknown';

export interface AddonEntitlement {
  state: EntitlementState;
  /** Why the box is disabled, shown as its tooltip. Empty when it is not. */
  detail: string;
}
