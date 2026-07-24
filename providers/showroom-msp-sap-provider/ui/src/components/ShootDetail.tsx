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
import type { Condition, ProviderShoot } from '../types';

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

interface Props {
  open: boolean;
  shoot: ProviderShoot | null;
  onClose: () => void;
}

export const ShootDetail = ({ open, shoot, onClose }: Props) => {
  if (!shoot) return null;
  const st = shoot.status ?? {};
  const spec = shoot.spec?.trim();

  return (
    <Dialog
      open={open}
      headerText={shoot.metadata.name}
      onClose={onClose}
      style={{ width: '680px', maxWidth: '95vw' }}
      footer={
        <Bar
          endContent={
            <Button design="Transparent" onClick={onClose}>
              Close
            </Button>
          }
        />
      }
    >
      <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '1rem', padding: '0.25rem 0' }}>
        <Panel headerText="Shoot" collapsed={false}>
          <Row label="Name">{shoot.metadata.name}</Row>
          <Row label="Template">{spec ? 'Custom manifest' : 'Built-in default'}</Row>
          <Row label="Provider configs">{st.providerConfigs?.join(', ') || '—'}</Row>
          <Row label="Observed generation">{st.observedGeneration ?? '—'}</Row>
          <Row label="Created">{fmtDate(shoot.metadata.creationTimestamp)}</Row>
        </Panel>

        <Panel headerText="Manifest" collapsed={!spec}>
          {spec ? (
            <pre
              style={{
                margin: 0,
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
                fontFamily: 'var(--sapCode_FontFamily, monospace)',
                fontSize: '0.8rem'
              }}
            >
              {spec}
            </pre>
          ) : (
            <div>Uses the operator's built-in default template.</div>
          )}
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
