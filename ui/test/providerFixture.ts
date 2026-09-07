import type { ClusterConfig, ProviderSource } from '../src/types';

// Providers contribute discovery and presentation; sizing remains shared.
export const source: ProviderSource = {
  group: 'example.test', version: 'v1', listField: 'Claims',
  providerConfigName: 'example', label: 'Example Cloud', logoUrl: '/provider.svg'
};

export const standardSources: ProviderSource[] = [
  { group: 'dati.eu', version: 'v1alpha1', listField: 'Shoots', providerConfigName: 'dati' },
  { group: 'sap.eu', version: 'v1alpha1', listField: 'OpenStackShoots', providerConfigName: 'sap-gardener-openstack' },
  { group: 'sap.eu', version: 'v1alpha1', listField: 'IronCoreShoots', providerConfigName: 'sap-gardener-ironcore' },
  { group: 'cloudferro.eu', version: 'v1alpha1', listField: 'OpenStackVMs', providerConfigName: 'cloudferro', namespaced: true },
  { group: 'arsys.eu', version: 'v1alpha1', listField: 'UbuntuMachines', providerConfigName: 'arsys-virtual', namespaced: true },
  { group: 'gardener.t-systems.com', version: 'v1', listField: 'ShootClaims', providerConfigName: 'telekom',
    namespaced: true, label: 'Telekom', logoUrl: '/telekom.svg' }
];

// Old mounted configuration must not reintroduce fields or replace sizing.
export const obsoleteSource = {
  ...source,
  inputs: [{ name: 'region', label: 'Region', type: 'enum', default: 'mdb',
    values: [{ key: 'mdb' }] }],
  sizingPresets: [{ id: 'fixed', label: 'Old provider preset',
    workers: { autoscaling: { min: 1, max: 2 }, requested: { cpu: '2', memory: '4Gi' } } }]
};

export const config: ClusterConfig = {
  providers: ['example', 'legacy'], providerSources: [source],
  cpu: ['1', '2'], memory: ['2Gi', '4Gi'],
  controlPlane: { min: 1, max: 3, default: 1 }, workers: { min: 1, max: 10, default: 1 }
};
