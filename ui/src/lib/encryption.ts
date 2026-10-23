/*
 * What the list and the detail view say about a cluster's encryption at
 * rest. The request lives in spec.encryption; whether it is honoured is the
 * operator's EncryptionReady condition (present only while encryption is
 * requested) or, when the provider config does not admit the request at all,
 * the Error condition with reason EncryptionUnsupported.
 */

import type { KeyStoreInfo, SimpleCluster } from '../types';

/**
 * Provider types the SimpleCluster API admits (the enum of
 * spec.encryption.provider.type). The UI offers nothing outside this list,
 * whatever the deployment config says, because the API would refuse it.
 */
export const ENCRYPTION_PROVIDER_TYPES: readonly string[] = ['OpenBao'];

export type EncryptionState = 'Positive' | 'Negative' | 'Critical' | 'None';

export interface EncryptionView {
  /** Encryption was asked for: spec.encryption is set, or the operator reports on it. */
  requested: boolean;
  /**
   * Provider type from the spec. Undefined when requested but the response
   * carried no spec.encryption (a gateway schema without the field): the
   * conditions still prove the request, the type is not known here.
   */
  provider?: string;
  keyRotationPeriod?: string;
  /** Positive: verified encrypted. Critical: requested, not verified yet. Negative: refused or failed. */
  state: EncryptionState;
  /** Short text for a table cell. */
  label: string;
  /** The condition message, when there is one. */
  detail?: string;
}

const NOT_REQUESTED: EncryptionView = { requested: false, state: 'None', label: '—' };

export const encryptionView = (cluster: SimpleCluster): EncryptionView => {
  const conditions = cluster.status?.conditions ?? [];
  const ready = conditions.find((c) => c.type === 'EncryptionReady');
  const refused = conditions.find(
    (c) => c.type === 'Error' && c.status === 'True' && c.reason === 'EncryptionUnsupported'
  );
  const provider = cluster.spec?.encryption?.provider;
  const type = provider?.type?.trim() || undefined;
  if (!type && !ready && !refused) return NOT_REQUESTED;
  let state: EncryptionState = 'Critical';
  if (refused) {
    state = 'Negative';
  } else if (ready?.status === 'True') {
    state = 'Positive';
  } else if (ready?.status === 'False' && ready.reason && ready.reason !== 'Provisioning') {
    state = 'Negative';
  }
  return {
    requested: true,
    provider: type,
    keyRotationPeriod: type ? provider?.keyRotationPeriod : undefined,
    state,
    label: type ?? 'Requested',
    detail: refused?.message ?? ready?.message
  };
};

/**
 * The host part of a key store address for display, or the address as given
 * when it has no host (not a URL, or a scheme-only form such as "host:8200"
 * that the URL parser reads as a scheme).
 */
const displayAddress = (address: string): string => {
  try {
    const u = new URL(address);
    if (!u.hostname) return address;
    return u.port ? `${u.hostname}:${u.port}` : u.hostname;
  } catch {
    return address;
  }
};

/**
 * One line answering "where are the keys stored?" for a provider type, from
 * deployment configuration. Without configuration the line says so instead
 * of guessing: the portal has no way to read the operator's settings.
 */
export const keyStoreSummary = (type: string, info?: KeyStoreInfo): string => {
  const name = info?.name?.trim() || type;
  const parts: string[] = [];
  if (info?.address?.trim()) parts.push(`${name} at ${displayAddress(info.address.trim())}`);
  else parts.push(name);
  if (info?.namespace?.trim()) parts.push(`namespace ${info.namespace.trim()}`);
  if (parts.length === 1 && !info?.address?.trim()) {
    return `${name}, operated by the platform; its location is not published for this portal`;
  }
  return parts.join(', ');
};
