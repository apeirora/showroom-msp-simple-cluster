import type { Condition, SimpleCluster } from '../types';

// The controller preserves the last observed phase on reconciliation errors.
// Present that error separately instead of implying Pending/Ready is healthy.
export const reconciliationError = (cluster: SimpleCluster): Condition | undefined =>
  cluster.status?.conditions?.find((condition) => condition.type === 'Error' && condition.status === 'True');

export const clusterDisplayPhase = (cluster: SimpleCluster): string => {
  if (cluster.metadata.deletionTimestamp) return 'Deleting';
  if (reconciliationError(cluster)) return 'Error';
  if (cluster.status?.phase) return cluster.status.phase;
  return typeof cluster.status?.observedGeneration === 'number' ? 'Pending' : 'Unknown';
};

export const conditionSummary = (condition: Condition): string =>
  [condition.reason || 'Reconciliation error', condition.message].filter(Boolean).join(': ');

export const conditionState = (condition: Condition): 'Positive' | 'Negative' | 'Critical' | 'None' => {
  if (condition.type === 'Error') {
    if (condition.status === 'True') return 'Negative';
    if (condition.status === 'False') return 'None';
    return 'Critical';
  }
  if (condition.status === 'True') return 'Positive';
  if (condition.status === 'False') return 'Negative';
  return 'Critical';
};
