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

const derivePhase = (cluster: SimpleCluster): string => {
  if (cluster.metadata.deletionTimestamp) return 'Deleting';
  const st = cluster.status;
  if (st?.phase) return st.phase;
  if (typeof st?.observedGeneration === 'number') return 'Pending';
  return 'Unknown';
};

interface ClusterListProps {
  clusters: SimpleCluster[];
  loading: boolean;
  onCreate: () => void;
  onAction: (action: ClusterAction, cluster: SimpleCluster) => void;
}

export const ClusterList = ({ clusters, loading, onCreate, onAction }: ClusterListProps) => {
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
    phase: derivePhase(c),
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
      width: 130,
      Cell: ({ value }: { value: ClusterPhase }) => (
        <ObjectStatus state={phaseColor(value)} inverted>
          {value}
        </ObjectStatus>
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
        <Button design="Emphasized" icon="add" onClick={onCreate}>
          New SimpleCluster
        </Button>
      </FlexBox>
      <AnalyticalTable
        data={rows}
        columns={columns as any}
        visibleRows={10}
        minRows={1}
        filterable
        sortable
      />
    </FlexBox>
  );
};
