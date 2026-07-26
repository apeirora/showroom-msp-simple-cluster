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

// One dedicated/cloud server ordered from Arsys. Cluster-scoped in KCP (the CRD
// itself is cluster-scoped), so the gateway exposes it WITHOUT a namespace
// argument.
export interface ArsysUbuntuMachine {
  metadata: {
    name: string;
    creationTimestamp?: string;
    resourceVersion?: string;
    uid?: string;
  };
  status?: ArsysUbuntuMachineStatus;
}
