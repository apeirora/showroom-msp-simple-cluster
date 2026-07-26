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
import type { OpenStackVM } from '../types';

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
  return `${Math.floor(h / 24)}d`;
};

type StatusState = 'Positive' | 'Negative' | 'Critical' | 'Information' | 'None';

const phaseState = (phase?: string): StatusState => {
  switch (phase) {
    case 'Ready':
      return 'Positive';
    case 'Failed':
      return 'Negative';
    case 'Deleting':
      return 'Critical';
    case 'Provisioning':
    case 'Init':
      return 'Information';
    default:
      return 'None';
  }
};

interface Props {
  vms: OpenStackVM[];
  loading: boolean;
  onCreate: () => void;
  onRefresh: () => void;
  onDelete: (vm: OpenStackVM) => void;
}

export const MachineList = ({ vms, loading, onCreate, onRefresh, onDelete }: Props) => {
  if (loading && vms.length === 0) {
    return (
      <FlexBox justifyContent={FlexBoxJustifyContent.Center} style={{ padding: '2rem' }}>
        <BusyIndicator active size="M" />
      </FlexBox>
    );
  }

  if (!loading && vms.length === 0) {
    return (
      <IllustratedMessage
        name="NoData"
        titleText="No virtual machines yet"
        subtitleText="Create an OpenStack VM on CloudFerro."
      >
        <Button design="Emphasized" onClick={onCreate}>
          New VM
        </Button>
      </IllustratedMessage>
    );
  }

  const rows = vms.map((vm) => {
    const st = vm.status ?? {};
    return {
      name: vm.metadata.name,
      phase: st.phase || 'Pending',
      phaseStateValue: phaseState(st.phase),
      nova: st.novaStatus || '—',
      flavor: st.resolvedFlavor?.flavor || '—',
      internal: st.internalAddress || '—',
      external: st.externalAddress || '—',
      age: ageOf(vm.metadata.creationTimestamp),
      _vm: vm
    };
  });

  const columns = [
    { Header: 'Name', accessor: 'name', minWidth: 150 },
    {
      Header: 'Phase',
      accessor: 'phase',
      width: 130,
      Cell: ({ row }: { row: { original: (typeof rows)[number] } }) => (
        <ObjectStatus state={row.original.phaseStateValue} inverted>
          {row.original.phase}
        </ObjectStatus>
      )
    },
    { Header: 'Nova', accessor: 'nova', width: 100 },
    { Header: 'Flavor', accessor: 'flavor', minWidth: 130 },
    { Header: 'Internal', accessor: 'internal', minWidth: 130 },
    { Header: 'External', accessor: 'external', minWidth: 130 },
    { Header: 'Age', accessor: 'age', width: 80 },
    {
      Header: 'Actions',
      accessor: '_actions',
      width: 130,
      disableSortBy: true,
      Cell: ({ row }: { row: { original: (typeof rows)[number] } }) => (
        <Button
          design="Negative"
          icon="delete"
          onClick={() => onDelete(row.original._vm)}
          aria-label={`Delete ${row.original.name}`}
        >
          Delete
        </Button>
      )
    }
  ];

  return (
    <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.75rem' }}>
      <FlexBox justifyContent={FlexBoxJustifyContent.SpaceBetween} style={{ alignItems: 'baseline' }}>
        <Title level="H3">Virtual machines</Title>
        <FlexBox style={{ gap: '0.5rem' }}>
          <Button design="Transparent" icon="refresh" onClick={onRefresh}>
            Refresh
          </Button>
          <Button design="Emphasized" icon="add" onClick={onCreate}>
            New VM
          </Button>
        </FlexBox>
      </FlexBox>
      <AnalyticalTable data={rows} columns={columns as any} visibleRows={10} minRows={1} filterable sortable />
    </FlexBox>
  );
};
