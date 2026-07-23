import type { Shoot } from '../types';

export const fixturesEnabled = (): boolean => {
  const env = (import.meta as any).env ?? {};
  return env.VITE_FIXTURES === 'true';
};

export const fixtureShoots = (): Shoot[] => [
  {
    metadata: {
      name: 'dati',
      creationTimestamp: new Date(Date.now() - 36 * 3600_000).toISOString(),
      uid: 'fixture-1'
    },
    status: {
      providerConfigs: ['dati'],
      observedGeneration: 1,
      conditions: [
        {
          type: 'Ready',
          status: 'True',
          reason: 'Reconciled',
          message: 'SimpleClusterProviderConfig "dati" materialised.',
          lastTransitionTime: new Date(Date.now() - 35 * 3600_000).toISOString()
        }
      ]
    }
  },
  {
    metadata: {
      name: 'dati-large',
      creationTimestamp: new Date(Date.now() - 15 * 60_000).toISOString(),
      uid: 'fixture-2'
    },
    spec: 'apiVersion: core.gardener.cloud/v1beta1\nkind: Shoot\nspec:\n  provider:\n    type: openstack\n',
    status: {
      observedGeneration: 1,
      conditions: [
        {
          type: 'Ready',
          status: 'False',
          reason: 'Reconciling',
          message: 'Building SimpleClusterProviderConfig.',
          lastTransitionTime: new Date(Date.now() - 10 * 60_000).toISOString()
        }
      ]
    }
  }
];
