import type { ArsysUbuntuMachine } from '../types';

export const fixturesEnabled = (): boolean => {
  const env = (import.meta as any).env ?? {};
  return env.VITE_FIXTURES === 'true';
};

export const fixtureMachines = (): ArsysUbuntuMachine[] => [
  {
    metadata: {
      name: 'demo-cp-0',
      creationTimestamp: new Date(Date.now() - 36 * 3600_000).toISOString(),
      uid: 'fixture-arsysmachine-1'
    },
    status: {
      phase: 'Ready',
      address: '203.0.113.21',
      provisioningPercent: 100,
      serverName: 'demo-cp-0',
      serverID: 'srv-1001',
      machineRef: 'demo-cp-0'
    }
  },
  {
    metadata: {
      name: 'demo-worker-0',
      creationTimestamp: new Date(Date.now() - 15 * 60_000).toISOString(),
      uid: 'fixture-arsysmachine-2'
    },
    status: {
      phase: 'Provisioning',
      provisioningPercent: 40,
      serverName: 'demo-worker-0',
      serverID: 'srv-1002'
    }
  }
];
