export interface Condition {
  type: string;
  status: 'True' | 'False' | 'Unknown' | string;
  reason?: string;
  message?: string;
  lastTransitionTime?: string;
}

export interface ShootStatus {
  // SimpleClusterProviderConfig names materialised for this workspace from the
  // Shoot's spec (empty spec => the operator's built-in default template).
  providerConfigs?: string[];
  observedGeneration?: number;
  conditions?: Condition[];
}

// A provider Shoot enables SAP Gardener as an extra service in one KCP account
// workspace. Its spec is the Gardener Shoot manifest, as free text, used
// verbatim as the SimpleClusterProviderConfig resource template. Empty spec =>
// built-in default. Projected cluster-scoped in KCP (namespaced per account on
// the service cluster), so the gateway exposes it WITHOUT a namespace argument.
// Both OpenStackShoot and IronCoreShoot share this exact shape.
export interface ProviderShoot {
  metadata: {
    name: string;
    creationTimestamp?: string;
    resourceVersion?: string;
    uid?: string;
  };
  spec?: string;
  status?: ShootStatus;
}
