import type { SimpleCluster } from '../types';

export const fixturesEnabled = (): boolean => {
  const env = (import.meta as any).env ?? {};
  return env.VITE_FIXTURES === 'true';
};

export const fixtureSimpleClusters = (): SimpleCluster[] => [
  {
    metadata: {
      name: 'demo-playground',
      namespace: 'default',
      creationTimestamp: new Date(Date.now() - 48 * 3600_000).toISOString(),
      uid: 'fixture-1'
    },
    spec: {
      providerConfigRefs: ['gardener-sap-openstack'],
      controlPlane: { nodes: 1, requested: { cpu: '2', memory: '4Gi' } },
      workers: { nodes: 2, requested: { cpu: '4', memory: '16Gi' } }
    },
    status: {
      phase: 'Ready',
      cp: '1/1',
      wkr: '2/2',
      providerConfigRef: { name: 'gardener-sap-openstack' },
      targetCluster: 'gardener-sap-openstack',
      kubeconfigSecretRef: { name: 'demo-playground-kubeconfig' },
      kubeconfigExpiresAt: new Date(Date.now() + 24 * 3600_000).toISOString(),
      kubeconfigIssuedAt: new Date(Date.now() - 1 * 3600_000).toISOString(),
      conditions: [
        {
          type: 'Ready',
          status: 'True',
          reason: 'Reconciled',
          message: 'SimpleCluster is ready.',
          lastTransitionTime: new Date(Date.now() - 12 * 3600_000).toISOString()
        }
      ]
    }
  },
  {
    metadata: {
      name: 'autoscaled-workers',
      namespace: 'default',
      creationTimestamp: new Date(Date.now() - 6 * 3600_000).toISOString(),
      uid: 'fixture-3'
    },
    spec: {
      providerConfigRefs: ['gardener-sap-ironcore'],
      workers: { autoscaling: { min: 1, max: 3 }, requested: { cpu: '4', memory: '8Gi' } }
    },
    status: {
      phase: 'Ready',
      cp: '-/-',
      wkr: '1/1-3',
      providerConfigRef: { name: 'gardener-sap-ironcore' },
      targetCluster: 'gardener-sap-ironcore',
      kubeconfigSecretRef: { name: 'autoscaled-workers-kubeconfig' },
      kubeconfigExpiresAt: new Date(Date.now() + 24 * 3600_000).toISOString(),
      kubeconfigIssuedAt: new Date(Date.now() - 2 * 3600_000).toISOString(),
      conditions: [
        {
          type: 'Ready',
          status: 'True',
          reason: 'Reconciled',
          message: 'SimpleCluster is ready.',
          lastTransitionTime: new Date(Date.now() - 5 * 3600_000).toISOString()
        },
        {
          type: 'AutoscalingSupported',
          status: 'True',
          reason: 'ProviderAutoscaling',
          message: 'provider scales workers between 1 and 3',
          lastTransitionTime: new Date(Date.now() - 5 * 3600_000).toISOString()
        }
      ]
    }
  },
  {
    metadata: {
      name: 'ci-runner',
      namespace: 'default',
      creationTimestamp: new Date(Date.now() - 20 * 60_000).toISOString(),
      uid: 'fixture-2'
    },
    spec: {
      providerConfigRefs: ['kind'],
      workers: { nodes: 1, requested: { cpu: '2', memory: '8Gi' } }
    },
    status: {
      phase: 'Provisioning',
      cp: '0/1',
      wkr: '0/1',
      providerConfigRef: { name: 'kind' },
      conditions: [
        {
          type: 'Progressing',
          status: 'True',
          reason: 'Creating',
          message: 'Provisioning cluster.',
          lastTransitionTime: new Date(Date.now() - 10 * 60_000).toISOString()
        }
      ]
    }
  }
];
