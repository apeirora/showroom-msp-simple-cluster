import type { ProviderShoot, IronCoreVM } from '../types';

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

export const fixtureVMs = (): IronCoreVM[] => [
  {
    metadata: {
      name: 'demo-vm-0',
      namespace: 'default',
      creationTimestamp: new Date(Date.now() - 36 * 3600_000).toISOString(),
      uid: 'fixture-ironcorevm-1'
    },
    status: {
      phase: 'Ready',
      machineState: 'Running',
      internalAddress: '10.0.0.11',
      externalAddress: '203.0.113.11',
      sshSecretRef: { name: 'demo-vm-0-ssh' },
      resolvedMachineClass: {
        name: 'memory-intel1-2cpu',
        cpu: 2,
        memory: '16 GiB',
        description: '2 vCPU / 16 GiB RAM'
      }
    }
  },
  {
    metadata: {
      name: 'demo-vm-1',
      namespace: 'default',
      creationTimestamp: new Date(Date.now() - 15 * 60_000).toISOString(),
      uid: 'fixture-ironcorevm-2'
    },
    status: {
      phase: 'Provisioning',
      machineState: 'Pending',
      internalAddress: '10.0.0.12',
      resolvedMachineClass: {
        name: 'memory-intel1-1cpu',
        cpu: 1,
        memory: '8 GiB',
        description: '1 vCPU / 8 GiB RAM'
      }
    }
  }
];
