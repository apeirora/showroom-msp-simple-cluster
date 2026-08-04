import {
  Bar,
  Button,
  Dialog,
  FlexBox,
  FlexBoxDirection,
  Label,
  ObjectStatus,
  Panel,
  Title
} from '@ui5/webcomponents-react';
import type { Condition, SimpleCluster } from '../types';

const Row = ({ label, children }: { label: string; children: React.ReactNode }) => (
  <FlexBox style={{ gap: '0.75rem', padding: '0.25rem 0', alignItems: 'baseline' }}>
    <Label style={{ minWidth: 190, color: 'var(--sapContent_LabelColor)' }}>{label}</Label>
    <div style={{ flex: 1, wordBreak: 'break-word' }}>{children ?? '—'}</div>
  </FlexBox>
);

const fmtDate = (ts?: string): string => {
  if (!ts) return '—';
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? ts : d.toLocaleString();
};

const conditionState = (c: Condition): 'Positive' | 'Negative' | 'Critical' | 'None' => {
  if (c.status === 'True') return 'Positive';
  if (c.status === 'False') return 'Negative';
  return 'Critical';
};

const phaseState = (phase?: string): 'Positive' | 'Negative' | 'Critical' | 'None' => {
  if (phase === 'Ready' || phase === 'Succeeded') return 'Positive';
  if (phase === 'Failed' || phase === 'Error') return 'Negative';
  if (!phase || phase === 'Unknown') return 'None';
  return 'Critical';
};

interface Props {
  open: boolean;
  cluster: SimpleCluster | null;
  onClose: () => void;
  onDownloadKubeconfig?: (cluster: SimpleCluster) => void;
}

export const ClusterDetail = ({ open, cluster, onClose, onDownloadKubeconfig }: Props) => {
  if (!cluster) return null;
  const s = cluster.spec ?? {};
  const st = cluster.status ?? {};
  const hasKubeconfig = Boolean(st.kubeconfigSecretRef?.name);
  const phase = cluster.metadata.deletionTimestamp ? 'Deleting' : st.phase;

  return (
    <Dialog
      open={open}
      headerText={cluster.metadata.name}
      onClose={onClose}
      style={{ width: '680px', maxWidth: '95vw' }}
      footer={
        <Bar
          startContent={
            hasKubeconfig && st.kubeconfigExpiresAt ? (
              <span style={{ fontSize: '0.75rem', color: 'var(--sapContent_LabelColor)' }}>
                Kubeconfig expires {fmtDate(st.kubeconfigExpiresAt)}
              </span>
            ) : null
          }
          endContent={
            <>
              {hasKubeconfig && onDownloadKubeconfig && (
                <Button
                  design="Emphasized"
                  icon="download"
                  onClick={() => onDownloadKubeconfig(cluster)}
                >
                  Download kubeconfig
                </Button>
              )}
              <Button design="Transparent" onClick={onClose}>
                Close
              </Button>
            </>
          }
        />
      }
    >
      <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '1rem', padding: '0.25rem 0' }}>
        <Panel headerText="Cluster" collapsed={false}>
          <Row label="Name">{cluster.metadata.name}</Row>
          <Row label="Phase">
            <ObjectStatus state={phaseState(phase)} inverted>
              {phase ?? 'Unknown'}
            </ObjectStatus>
          </Row>
          <Row label="Provider">
            {st.providerConfigRef?.name ?? s.providerConfigRefs?.[0]}
          </Row>
          <Row label="Target cluster">{st.targetCluster}</Row>
          <Row label="Created">{fmtDate(cluster.metadata.creationTimestamp)}</Row>
        </Panel>

        <Panel headerText="Control plane" collapsed={false}>
          <Row label="Ready / declared">{st.cp}</Row>
          <Row label="Nodes">{s.controlPlane?.nodes ?? '—'}</Row>
          <Row label="CPU / node">{String(s.controlPlane?.requested?.cpu ?? '—')}</Row>
          <Row label="Memory / node">{String(s.controlPlane?.requested?.memory ?? '—')}</Row>
        </Panel>

        <Panel headerText="Workers" collapsed={false}>
          <Row label="Ready / declared">{st.wkr}</Row>
          {s.workers?.autoscaling ? (
            <Row label="Nodes (autoscaled)">
              {`${s.workers.autoscaling.min ?? '—'} – ${s.workers.autoscaling.max ?? '—'}`}
            </Row>
          ) : (
            <Row label="Nodes">{s.workers?.nodes ?? '—'}</Row>
          )}
          <Row label="CPU / node">{String(s.workers?.requested?.cpu ?? '—')}</Row>
          <Row label="Memory / node">{String(s.workers?.requested?.memory ?? '—')}</Row>
        </Panel>

        <Panel headerText="Nodes" collapsed={!st.nodes?.length}>
          {st.nodes?.length ? (
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.85rem' }}>
              <thead>
                <tr style={{ textAlign: 'left', color: 'var(--sapContent_LabelColor)' }}>
                  <th style={{ padding: '0.25rem 0.5rem 0.25rem 0' }}>Name</th>
                  <th style={{ padding: '0.25rem 0.5rem' }}>Role</th>
                  <th style={{ padding: '0.25rem 0.5rem' }}>External IP</th>
                  <th style={{ padding: '0.25rem 0.5rem' }}>Internal IP</th>
                  <th style={{ padding: '0.25rem 0.5rem' }}>Ready</th>
                </tr>
              </thead>
              <tbody>
                {st.nodes.map((n) => (
                  <tr
                    key={n.name}
                    style={{ borderTop: '1px solid var(--sapGroup_ContentBorderColor)' }}
                  >
                    <td style={{ padding: '0.25rem 0.5rem 0.25rem 0', wordBreak: 'break-all' }}>
                      {n.name}
                    </td>
                    <td style={{ padding: '0.25rem 0.5rem' }}>{n.role || '—'}</td>
                    <td style={{ padding: '0.25rem 0.5rem' }}>{n.externalIP || '—'}</td>
                    <td style={{ padding: '0.25rem 0.5rem' }}>{n.internalIP || '—'}</td>
                    <td style={{ padding: '0.25rem 0.5rem' }}>
                      <ObjectStatus state={n.ready ? 'Positive' : 'Negative'} inverted>
                        {n.ready ? 'Ready' : 'NotReady'}
                      </ObjectStatus>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <div>—</div>
          )}
        </Panel>

        <Panel headerText="Kubeconfig" collapsed>
          <Row label="Secret name">{st.kubeconfigSecretRef?.name}</Row>
          <Row label="Issued at">{fmtDate(st.kubeconfigIssuedAt)}</Row>
          <Row label="Expires at">{fmtDate(st.kubeconfigExpiresAt)}</Row>
        </Panel>

        <Panel headerText="Resources" collapsed={!st.resources?.length}>
          {(st.resources ?? []).map((r) => (
            <Row key={r.name} label={r.name ?? '—'}>
              <ObjectStatus state={phaseState(r.phase)} inverted>
                {r.phase ?? 'Unknown'}
              </ObjectStatus>
              {r.message && (
                <span style={{ marginLeft: '0.5rem', fontSize: '0.8rem' }}>{r.message}</span>
              )}
            </Row>
          ))}
          {!st.resources?.length && <div>—</div>}
        </Panel>

        <Panel headerText="Conditions" collapsed={!st.conditions?.length}>
          {(st.conditions ?? []).map((c) => (
            <FlexBox
              key={c.type}
              direction={FlexBoxDirection.Column}
              style={{ gap: 2, padding: '0.35rem 0', borderBottom: '1px solid var(--sapGroup_ContentBorderColor)' }}
            >
              <FlexBox style={{ gap: '0.75rem', alignItems: 'baseline' }}>
                <Title level="H6" style={{ margin: 0, minWidth: 190 }}>
                  {c.type}
                </Title>
                <ObjectStatus state={conditionState(c)} inverted>
                  {c.status}
                </ObjectStatus>
                {c.reason && (
                  <span style={{ color: 'var(--sapContent_LabelColor)', fontSize: '0.75rem' }}>
                    {c.reason}
                  </span>
                )}
              </FlexBox>
              {c.message && <div style={{ fontSize: '0.8rem' }}>{c.message}</div>}
              {c.lastTransitionTime && (
                <div style={{ fontSize: '0.7rem', color: 'var(--sapContent_LabelColor)' }}>
                  {fmtDate(c.lastTransitionTime)}
                </div>
              )}
            </FlexBox>
          ))}
          {!st.conditions?.length && <div>—</div>}
        </Panel>
      </FlexBox>
    </Dialog>
  );
};
