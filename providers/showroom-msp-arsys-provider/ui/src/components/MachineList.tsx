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
import type { ArsysUbuntuMachine } from '../types';

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
    case 'Pending':
      return 'Information';
    default:
      return 'None';
  }
};

interface Props {
  machines: ArsysUbuntuMachine[];
  loading: boolean;
  onCreate: () => void;
  onRefresh: () => void;
  onDelete: (machine: ArsysUbuntuMachine) => void;
  onShowPassword: (machine: ArsysUbuntuMachine) => void;
}

export const MachineList = ({
  machines,
  loading,
  onCreate,
  onRefresh,
  onDelete,
  onShowPassword
}: Props) => {
  if (loading && machines.length === 0) {
    return (
      <FlexBox justifyContent={FlexBoxJustifyContent.Center} style={{ padding: '2rem' }}>
        <BusyIndicator active size="M" />
      </FlexBox>
    );
  }

  if (!loading && machines.length === 0) {
    return (
      <IllustratedMessage
        name="NoData"
        titleText="No machines yet"
        subtitleText="Order an Ubuntu server from Arsys."
      >
        <Button design="Emphasized" onClick={onCreate}>
          New machine
        </Button>
      </IllustratedMessage>
    );
  }

  const rows = machines.map((machine) => {
    const st = machine.status ?? {};
    return {
      name: machine.metadata.name,
      phase: st.phase || 'Pending',
      phaseStateValue: phaseState(st.phase),
      progress: st.provisioningPercent != null ? `${st.provisioningPercent}%` : '—',
      server: st.serverName || '—',
      address: st.address || '—',
      age: ageOf(machine.metadata.creationTimestamp),
      _machine: machine
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
    { Header: 'Progress', accessor: 'progress', width: 100 },
    { Header: 'Server', accessor: 'server', minWidth: 140 },
    { Header: 'Address', accessor: 'address', minWidth: 140 },
    { Header: 'Age', accessor: 'age', width: 80 },
    {
      Header: 'Actions',
      accessor: '_actions',
      width: 250,
      disableSortBy: true,
      Cell: ({ row }: { row: { original: (typeof rows)[number] } }) => (
        <FlexBox style={{ gap: '0.5rem' }}>
          <Button
            design="Transparent"
            icon="key"
            onClick={() => onShowPassword(row.original._machine)}
            aria-label={`Show password for ${row.original.name}`}
          >
            Password
          </Button>
          <Button
            design="Negative"
            icon="delete"
            onClick={() => onDelete(row.original._machine)}
            aria-label={`Delete ${row.original.name}`}
          >
            Delete
          </Button>
        </FlexBox>
      )
    }
  ];

  return (
    <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.75rem' }}>
      <FlexBox justifyContent={FlexBoxJustifyContent.SpaceBetween} style={{ alignItems: 'baseline' }}>
        <Title level="H3">Machines</Title>
        <FlexBox style={{ gap: '0.5rem' }}>
          <Button design="Transparent" icon="refresh" onClick={onRefresh}>
            Refresh
          </Button>
          <Button design="Emphasized" icon="add" onClick={onCreate}>
            New machine
          </Button>
        </FlexBox>
      </FlexBox>
      <AnalyticalTable data={rows} columns={columns as any} visibleRows={10} minRows={1} filterable sortable />
    </FlexBox>
  );
};
