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
import type { Condition, ProviderShoot } from '../types';

export type ShootAction = 'view' | 'delete';

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

const readyCondition = (shoot: ProviderShoot): Condition | undefined =>
  shoot.status?.conditions?.find((c) => c.type === 'Ready');

type StatusState = 'Positive' | 'Negative' | 'Critical' | 'Information' | 'None';

const readyState = (shoot: ProviderShoot): { label: string; state: StatusState } => {
  const c = readyCondition(shoot);
  if (!c) return { label: 'Pending', state: 'Information' };
  if (c.status === 'True') return { label: 'Ready', state: 'Positive' };
  if (c.status === 'False') return { label: c.reason || 'NotReady', state: 'Critical' };
  return { label: 'Unknown', state: 'None' };
};

interface ShootListProps {
  shoots: ProviderShoot[];
  loading: boolean;
  onCreate: () => void;
  onAction: (action: ShootAction, shoot: ProviderShoot) => void;
}

export const ShootList = ({ shoots, loading, onCreate, onAction }: ShootListProps) => {
  if (loading && shoots.length === 0) {
    return (
      <FlexBox justifyContent={FlexBoxJustifyContent.Center} style={{ padding: '2rem' }}>
        <BusyIndicator active size="M" />
      </FlexBox>
    );
  }

  if (!loading && shoots.length === 0) {
    return (
      <IllustratedMessage
        name="NoData"
        titleText="No Shoots yet"
        subtitleText="Create a Shoot to add an optional SAP Integration resource in this workspace."
      >
        <Button design="Emphasized" onClick={onCreate}>
          New Shoot
        </Button>
      </IllustratedMessage>
    );
  }

  const rows = shoots.map((s) => {
    const ready = readyState(s);
    return {
      name: s.metadata.name,
      ready: ready.label,
      readyStateValue: ready.state,
      template: s.spec?.trim() ? 'Custom' : 'Built-in',
      age: ageOf(s.metadata.creationTimestamp),
      _shoot: s
    };
  });

  const columns = [
    { Header: 'Name', accessor: 'name', minWidth: 150 },
    {
      Header: 'Ready',
      accessor: 'ready',
      width: 140,
      Cell: ({ row }: { row: { original: (typeof rows)[number] } }) => (
        <ObjectStatus state={row.original.readyStateValue} inverted>
          {row.original.ready}
        </ObjectStatus>
      )
    },
    { Header: 'Template', accessor: 'template', width: 110 },
    { Header: 'Age', accessor: 'age', width: 80 },
    {
      Header: 'Actions',
      accessor: '_actions',
      width: 220,
      disableSortBy: true,
      Cell: ({ row }: { row: { original: (typeof rows)[number] } }) => {
        const r = row.original;
        return (
          <FlexBox style={{ gap: '0.25rem' }}>
            <Button
              design="Transparent"
              icon="detail-view"
              onClick={() => onAction('view', r._shoot)}
              aria-label={`View details for ${r.name}`}
            >
              Details
            </Button>
            <Button
              design="Negative"
              icon="delete"
              onClick={() => onAction('delete', r._shoot)}
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
        <Title level="H3">Shoots</Title>
        <Button design="Emphasized" icon="add" onClick={onCreate}>
          New Shoot
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
