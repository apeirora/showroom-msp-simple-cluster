export interface Condition {
  type: string;
  status: 'True' | 'False' | 'Unknown' | string;
  reason?: string;
  message?: string;
  lastTransitionTime?: string;
}

export interface ResolvedFlavor {
  flavor?: string;
  cpu?: number;
  memory?: string;
  flavorDescription?: string;
}

export interface OpenStackVMStatus {
  phase?: string;
  internalAddress?: string;
  externalAddress?: string;
  novaStatus?: string;
  resolvedFlavor?: ResolvedFlavor;
  observedGeneration?: number;
  conditions?: Condition[];
}

// One Nova server managed by the CloudFerro provider. Projected CLUSTER-SCOPED
// into the KCP workspace (APIResourceSchema scope: Cluster), so the gateway
// exposes it WITHOUT a namespace argument.
export interface OpenStackVM {
  metadata: {
    name: string;
    creationTimestamp?: string;
    resourceVersion?: string;
    uid?: string;
  };
  status?: OpenStackVMStatus;
}
