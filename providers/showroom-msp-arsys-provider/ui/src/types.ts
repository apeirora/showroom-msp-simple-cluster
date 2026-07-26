export interface Condition {
  type: string;
  status: 'True' | 'False' | 'Unknown' | string;
  reason?: string;
  message?: string;
  lastTransitionTime?: string;
}

export interface ArsysUbuntuMachineStatus {
  phase?: string;
  address?: string;
  provisioningPercent?: number;
  serverName?: string;
  serverID?: string;
  machineRef?: string;
  baremetalModelID?: string;
  observedGeneration?: number;
  conditions?: Condition[];
}

// One dedicated/cloud server ordered from Arsys. Projected NAMESPACED into the
// KCP workspace (APIResourceSchema scope: Namespaced) even though the CRD itself
// is cluster-scoped on the service cluster, so the gateway exposes it WITH a
// namespace argument.
export interface ArsysUbuntuMachine {
  metadata: {
    name: string;
    namespace?: string;
    creationTimestamp?: string;
    resourceVersion?: string;
    uid?: string;
  };
  status?: ArsysUbuntuMachineStatus;
}
