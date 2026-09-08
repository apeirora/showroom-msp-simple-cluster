import { describe, expect, it } from 'vitest';
import { clusterDisplayPhase, conditionState, conditionSummary, reconciliationError } from '../src/lib/clusterStatus';
import type { SimpleCluster } from '../src/types';

const cluster = (phase: string, status = 'True'): SimpleCluster => ({
  metadata: { name: 'invalid-region' }, spec: {},
  status: { phase, conditions: [{ type: 'Error', status, reason: 'InputError', message: 'unsupported region' }] }
});

describe('cluster failure presentation', () => {
  it.each(['Pending', 'Ready'])('shows reconciliation Error instead of the last %s phase', (phase) => {
    const value = cluster(phase);
    expect(clusterDisplayPhase(value)).toBe('Error');
    expect(conditionSummary(reconciliationError(value)!)).toBe('InputError: unsupported region');
    expect(value.status!.phase).toBe(phase); // Presentation never changes domain state.
  });

  it('keeps deletion ahead of an active error', () => {
    const value = cluster('Ready');
    value.metadata.deletionTimestamp = '2026-09-04T21:00:00Z';
    expect(clusterDisplayPhase(value)).toBe('Deleting');
    expect(reconciliationError(value)?.reason).toBe('InputError');
  });

  it('does not turn an inactive error into a failed cluster', () => {
    expect(clusterDisplayPhase(cluster('Ready', 'False'))).toBe('Ready');
    expect(reconciliationError(cluster('Ready', 'False'))).toBeUndefined();
    expect(conditionState({ type: 'Error', status: 'False' })).toBe('None');
    expect(conditionState({ type: 'Error', status: 'True' })).toBe('Negative');
    expect(conditionState({ type: 'Error', status: 'Unknown' })).toBe('Critical');
  });

  it('preserves ordinary readiness condition polarity and absent-status fallback', () => {
    expect(conditionState({ type: 'Ready', status: 'True' })).toBe('Positive');
    expect(conditionState({ type: 'Ready', status: 'False' })).toBe('Negative');
    expect(clusterDisplayPhase({ metadata: { name: 'new' }, spec: {} })).toBe('Unknown');
    expect(clusterDisplayPhase({ metadata: { name: 'new' }, spec: {}, status: { observedGeneration: 1 } })).toBe('Pending');
  });
});
