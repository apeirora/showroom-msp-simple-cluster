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

// A provider Shoot enables SAP Integration as an extra service in one KCP account
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

// ResolvedMachineClass records the IronCore MachineClass the operator selected.
export interface ResolvedMachineClass {
  name?: string;
  cpu?: number;
  memory?: string;
  description?: string;
}

export interface IronCoreVMStatus {
  phase?: string;
  machineState?: string;
  internalAddress?: string;
  externalAddress?: string;
  // SSHSecretRef names the Secret (same namespace) holding the per-VM ed25519
  // keypair the operator generates: keys ssh-privatekey / ssh-publickey. When
  // unset the operator falls back to `<vm-name>-ssh`.
  sshSecretRef?: { name?: string };
  resolvedMachineClass?: ResolvedMachineClass;
  observedGeneration?: number;
  conditions?: Condition[];
}

// One IronCore Machine managed by simple-cluster-operator-ironcore. Projected
// NAMESPACED into the KCP workspace (APIResourceSchema scope: Namespaced), so
// the gateway exposes it WITH a namespace argument.
export interface IronCoreVM {
  metadata: {
    name: string;
    namespace?: string;
    creationTimestamp?: string;
    resourceVersion?: string;
    uid?: string;
  };
  status?: IronCoreVMStatus;
}
