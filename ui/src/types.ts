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

export interface ClusterConfig {
  providers: string[];
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
