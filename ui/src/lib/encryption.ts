/*
 * What the list and the detail view say about a cluster's encryption at
 * rest. The request lives in spec.encryption; whether it is honoured is the
 * operator's EncryptionReady condition (present only while encryption is
 * requested) or, when the provider config does not admit the request at all,
 * the Error condition with reason EncryptionUnsupported.
 */

import type { SimpleCluster } from '../types';

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
