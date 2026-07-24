import type { ProviderShoot } from '../types';

export type ShootFlavor = 'openstack' | 'ironcore';

export const fixturesEnabled = (): boolean => {
  const env = (import.meta as any).env ?? {};
  return env.VITE_FIXTURES === 'true';
};

export const fixtureShoots = (flavor: ShootFlavor): ProviderShoot[] => [
  {
    metadata: {
      name: flavor,
      creationTimestamp: new Date(Date.now() - 36 * 3600_000).toISOString(),
      uid: `fixture-${flavor}-1`
    },
    status: {
      providerConfigs: [flavor],
      observedGeneration: 1,
      conditions: [
        {
          type: 'Ready',
          status: 'True',
          reason: 'Reconciled',
          message: `SimpleClusterProviderConfig "${flavor}" materialised.`,
          lastTransitionTime: new Date(Date.now() - 35 * 3600_000).toISOString()
        }
      ]
    }
  },
  {
    metadata: {
      name: `${flavor}-large`,
      creationTimestamp: new Date(Date.now() - 15 * 60_000).toISOString(),
      uid: `fixture-${flavor}-2`
    },
    spec: `apiVersion: core.gardener.cloud/v1beta1\nkind: Shoot\nspec:\n  provider:\n    type: ${flavor}\n`,
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
