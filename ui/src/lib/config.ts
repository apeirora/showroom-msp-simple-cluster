/*
 * Loads /simple-cluster-config.json at startup. Values are editable at
 * deployment time via the Helm ConfigMap without rebuilding the container.
 */

import type { ClusterConfig } from '../types';
import { ENCRYPTION_PROVIDER_TYPES } from './encryption';

const FALLBACK: ClusterConfig = {
  providers: ['cloudferro', 'gardener-sap-ironcore', 'gardener-sap-openstack', 'kind'],
  providerSources: [
    {
      group: 'dati.eu',
      version: 'v1alpha1',
      listField: 'Shoots',
      label: 'Dati (Gardener Shoot)',
      providerConfigName: 'dati'
    }
  ],
  encryptionProviders: {},
  cpu: ['1', '2', '4', '8', '16'],
  memory: ['2Gi', '4Gi', '8Gi', '16Gi', '32Gi'],
  controlPlane: { min: 1, max: 10, default: 1 },
  workers: { min: 1, max: 10, default: 1 }
};

// Keeps only what can work: a provider config name mapped to provider types
// the SimpleCluster API admits, trimmed and without duplicates. Anything else
// would surface as a control that creates an object the API rejects, so it
// is dropped with a console warning that names it.
const sanitizeEncryptionProviders = (raw: unknown): Record<string, string[]> => {
  const out: Record<string, string[]> = {};
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return out;
  for (const [name, types] of Object.entries(raw as Record<string, unknown>)) {
    if (!name.trim() || !Array.isArray(types)) continue;
    const clean: string[] = [];
    for (const t of types) {
      const type = typeof t === 'string' ? t.trim() : '';
      if (!type || clean.includes(type)) continue;
      if (!ENCRYPTION_PROVIDER_TYPES.includes(type)) {
        console.warn(
          `[simple-cluster-ui] encryptionProviders.${name}: "${type}" is not a provider type the SimpleCluster API admits; ignored`
        );
        continue;
      }
      clean.push(type);
    }
    if (clean.length) out[name.trim()] = clean;
  }
  return out;
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
      encryptionProviders: sanitizeEncryptionProviders(body.encryptionProviders),
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
