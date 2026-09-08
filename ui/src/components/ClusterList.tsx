import {
  AnalyticalTable,
  Button,
  BusyIndicator,
  FlexBox,
  FlexBoxDirection,
  FlexBoxJustifyContent,
  IllustratedMessage,
  ObjectStatus,
  Title
} from '@ui5/webcomponents-react';
import '@ui5/webcomponents-fiori/dist/illustrations/NoData.js';
import type { SimpleCluster, ClusterPhase } from '../types';
import { clusterDisplayPhase, conditionSummary, reconciliationError } from '../lib/clusterStatus';

export type ClusterAction = 'view' | 'delete' | 'download-kubeconfig';

const ageOf = (ts?: string): string => {
  if (!ts) return '—';
  const ms = Date.now() - new Date(ts).getTime();
  if (ms < 0) return 'just now';
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  const d = Math.floor(h / 24);
  return `${d}d`;
};

const phaseColor = (
  phase?: ClusterPhase
): 'Positive' | 'Negative' | 'Critical' | 'Information' | 'None' => {
  switch (phase) {
    case 'Ready':
    case 'Succeeded':
      return 'Positive';
    case 'Failed':
    case 'Error':
      return 'Negative';
    case 'Provisioning':
    case 'Processing':
    case 'Deleting':
      return 'Critical';
    case 'Pending':
      return 'Information';
    default:
      return 'None';
  }
};

interface ClusterListProps {
  clusters: SimpleCluster[];
  loading: boolean;
  noProviders?: boolean;
  onCreate: () => void;
  onAction: (action: ClusterAction, cluster: SimpleCluster) => void;
}

export const ClusterList = ({ clusters, loading, noProviders = false, onCreate, onAction }: ClusterListProps) => {
  // Nothing can be created without a provider, so an empty account lands here
  // instead of on a busy indicator or a create invitation that cannot succeed.
  if (noProviders && clusters.length === 0) {
    return (
      <IllustratedMessage
        name="NoData"
        titleText="No cluster providers"
        subtitleText="No cluster providers are available in this account."
      />
    );
  }

  if (loading && clusters.length === 0) {
    return (
      <FlexBox justifyContent={FlexBoxJustifyContent.Center} style={{ padding: '2rem' }}>
        <BusyIndicator active size="M" />
      </FlexBox>
    );
  }

  if (!loading && clusters.length === 0) {
    return (
      <IllustratedMessage
        name="NoData"
        titleText="No SimpleClusters yet"
        subtitleText="Provision your first managed Kubernetes cluster."
      >
        <Button design="Emphasized" onClick={onCreate}>
          New SimpleCluster
        </Button>
      </IllustratedMessage>
    );
  }

  const rows = clusters.map((c) => ({
    name: c.metadata.name,
    // Filtering/sorting use the same display state as the badge, not a stale
    // Ready phase concealed behind an Error condition.
    phase: clusterDisplayPhase(c),
    error: reconciliationError(c) ? conditionSummary(reconciliationError(c)!) : '',
    provider: c.status?.providerConfigRef?.name ?? c.spec?.providerConfigRefs?.[0] ?? '—',
    cp: c.status?.cp ?? '—',
    wkr: c.status?.wkr ?? '—',
    age: ageOf(c.metadata.creationTimestamp),
    hasKubeconfig: Boolean(c.status?.kubeconfigSecretRef?.name),
    _cluster: c
  }));

  const columns = [
    { Header: 'Name', accessor: 'name', minWidth: 150 },
    {
      Header: 'Phase',
      accessor: 'phase',
      width: 240,
      Cell: ({ value, row }: { value: ClusterPhase; row: { original: (typeof rows)[number] } }) => (
        <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 2, minWidth: 0 }}>
          <ObjectStatus state={phaseColor(value)} inverted>
            {value}
          </ObjectStatus>
          {row.original.error && (
            <span title={row.original.error} style={{ fontSize: '0.75rem', maxWidth: '100%',
              overflow: 'hidden', whiteSpace: 'nowrap', textOverflow: 'ellipsis' }}>
              {row.original.error}
            </span>
          )}
        </FlexBox>
      )
    },
    { Header: 'Provider', accessor: 'provider', minWidth: 180 },
    { Header: 'CP', accessor: 'cp', width: 70 },
    { Header: 'Workers', accessor: 'wkr', width: 90 },
    { Header: 'Age', accessor: 'age', width: 80 },
    {
      Header: 'Actions',
      accessor: '_actions',
      width: 320,
      disableSortBy: true,
      Cell: ({ row }: { row: { original: (typeof rows)[number] } }) => {
        const r = row.original;
        return (
          <FlexBox style={{ gap: '0.25rem' }}>
            <Button
              design="Transparent"
              icon="detail-view"
              onClick={() => onAction('view', r._cluster)}
              aria-label={`View details for ${r.name}`}
            >
              Details
            </Button>
            <Button
              design="Transparent"
              icon="download"
              disabled={!r.hasKubeconfig}
              onClick={() => onAction('download-kubeconfig', r._cluster)}
              aria-label={`Download kubeconfig for ${r.name}`}
            >
              Kubeconfig
            </Button>
            <Button
              design="Negative"
              icon="delete"
              onClick={() => onAction('delete', r._cluster)}
              aria-label={`Delete ${r.name}`}
            >
              Delete
            </Button>
          </FlexBox>
        );
      }
    }
  ];

  return (
    <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.75rem' }}>
      <FlexBox justifyContent={FlexBoxJustifyContent.SpaceBetween} style={{ alignItems: 'baseline' }}>
        <Title level="H3">SimpleClusters</Title>
        <Button design="Emphasized" icon="add" disabled={noProviders} onClick={onCreate}>
          New SimpleCluster
        </Button>
      </FlexBox>
      <AnalyticalTable
        data={rows}
        columns={columns as any}
        visibleRows={10}
        minRows={1}
        rowHeight={rows.some((row) => row.error) ? 56 : undefined}
        filterable
        sortable
      />
    </FlexBox>
  );
};
