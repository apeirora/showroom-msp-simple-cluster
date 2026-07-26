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
  // SSHSecretRef names the Secret (same namespace) holding the per-VM ed25519
  // keypair the operator generates: keys ssh-privatekey / ssh-publickey.
  sshSecretRef?: { name?: string };
  resolvedFlavor?: ResolvedFlavor;
  observedGeneration?: number;
  conditions?: Condition[];
}

// One Nova server managed by the CloudFerro provider. Projected NAMESPACED into
// the KCP workspace (APIResourceSchema scope: Namespaced), so the gateway
// exposes it WITH a namespace argument.
export interface OpenStackVM {
  metadata: {
    name: string;
    namespace?: string;
    creationTimestamp?: string;
    resourceVersion?: string;
    uid?: string;
  };
  status?: OpenStackVMStatus;
}
