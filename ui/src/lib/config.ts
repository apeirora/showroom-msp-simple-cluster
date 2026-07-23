/*
 * Loads /simple-cluster-config.json at startup. Values are editable at
 * deployment time via the Helm ConfigMap without rebuilding the container.
 */

import type { ClusterConfig } from '../types';

const FALLBACK: ClusterConfig = {
  providers: ['cloudferro', 'gardener-sap-ironcore', 'gardener-sap-openstack', 'kind'],
  providerSources: [
    {
      group: 'dati.gardener',
      version: 'v1alpha1',
      listField: 'Shoots',
      label: 'Dati (Gardener Shoot)',
      providerConfigName: 'dati'
    }
  ],
  cpu: ['1', '2', '4', '8', '16'],
  memory: ['2Gi', '4Gi', '8Gi', '16Gi', '32Gi'],
  controlPlane: { min: 1, max: 10, default: 1 },
  workers: { min: 1, max: 10, default: 1 }
};

export const loadClusterConfig = async (
  url = '/simple-cluster-config.json'
): Promise<ClusterConfig> => {
  try {
    const resp = await fetch(url, { cache: 'no-cache' });
    if (!resp.ok) {
      console.warn(
        `[simple-cluster-ui] simple-cluster-config.json unavailable (${resp.status}); using fallback`
      );
      return FALLBACK;
    }
    const body = (await resp.json()) as Partial<ClusterConfig>;
    return {
      providers: body.providers?.length ? body.providers : FALLBACK.providers,
      providerSources: body.providerSources ?? FALLBACK.providerSources,
      cpu: body.cpu?.length ? body.cpu : FALLBACK.cpu,
      memory: body.memory?.length ? body.memory : FALLBACK.memory,
      controlPlane: { ...FALLBACK.controlPlane, ...(body.controlPlane ?? {}) },
      workers: { ...FALLBACK.workers, ...(body.workers ?? {}) }
    };
  } catch (err) {
    console.warn('[simple-cluster-ui] failed to load simple-cluster-config.json; using fallback', err);
    return FALLBACK;
  }
};

export const fallbackClusterConfig = (): ClusterConfig => FALLBACK;
