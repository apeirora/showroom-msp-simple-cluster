import type { OpenStackVM } from '../types';

export const fixturesEnabled = (): boolean => {
  const env = (import.meta as any).env ?? {};
  return env.VITE_FIXTURES === 'true';
};

export const fixtureVMs = (): OpenStackVM[] => [
  {
    metadata: {
      name: 'demo-cp-0',
      creationTimestamp: new Date(Date.now() - 36 * 3600_000).toISOString(),
      uid: 'fixture-openstackvm-1'
    },
    status: {
      phase: 'Ready',
      novaStatus: 'ACTIVE',
      internalAddress: '10.0.0.11',
      externalAddress: '203.0.113.11',
      resolvedFlavor: { flavor: 'eo1a.medium', cpu: 2, memory: '4 GiB' }
    }
  },
  {
    metadata: {
      name: 'demo-worker-0',
      creationTimestamp: new Date(Date.now() - 15 * 60_000).toISOString(),
      uid: 'fixture-openstackvm-2'
    },
    status: {
      phase: 'Provisioning',
      novaStatus: 'BUILD',
      internalAddress: '10.0.0.12',
      resolvedFlavor: { flavor: 'eo1a.large', cpu: 4, memory: '8 GiB' }
    }
  }
];
