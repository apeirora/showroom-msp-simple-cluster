import { useEffect, useState } from 'react';
import {
  Bar,
  Button,
  Dialog,
  FlexBox,
  FlexBoxDirection,
  Input,
  Label,
  MessageStrip,
  Option,
  Select
} from '@ui5/webcomponents-react';
import { MACHINE_SIZES, defaultSizeId, type CreateInput } from '../lib/machine';

interface Props {
  open: boolean;
  busy: boolean;
  onClose: () => void;
  onSubmit: (values: CreateInput) => void;
}

// A DNS-1123 subdomain, the constraint Kubernetes object names must satisfy.
const NAME_RE = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

export const CreateMachineDialog = ({ open, busy, onClose, onSubmit }: Props) => {
  const [name, setName] = useState('');
  const [sizeId, setSizeId] = useState(defaultSizeId);

  useEffect(() => {
    if (open) {
      setName('');
      setSizeId(defaultSizeId);
    }
  }, [open]);

  const nameValid = NAME_RE.test(name);
  const canSubmit = nameValid && !busy;

  return (
    <Dialog
      open={open}
      headerText="New Arsys machine"
      onClose={onClose}
      style={{ width: '480px', maxWidth: '95vw' }}
      footer={
        <Bar
          endContent={
            <>
              <Button design="Transparent" onClick={onClose}>
                Cancel
              </Button>
              <Button design="Emphasized" disabled={!canSubmit} onClick={() => onSubmit({ name: name.trim(), sizeId })}>
                {busy ? 'Creating…' : 'Create'}
              </Button>
            </>
          }
        />
      }
    >
      <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '1rem', padding: '0.5rem 0' }}>
        <MessageStrip design="Information" hideCloseButton>
          Orders one cloud server from Arsys. Appliance, datacenter and API token use the Arsys provider
          defaults.
        </MessageStrip>

        <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.35rem' }}>
          <Label required>Name</Label>
          <Input
            value={name}
            onInput={(e) => setName((e.target as unknown as HTMLInputElement).value)}
            valueState={name.length === 0 || nameValid ? 'None' : 'Negative'}
            placeholder="my-machine"
          />
          {!nameValid && name.length > 0 && (
            <span style={{ fontSize: '0.75rem', color: 'var(--sapNegativeColor)' }}>
              Lowercase letters, digits and dashes only (DNS-1123).
            </span>
          )}
        </FlexBox>

        <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.35rem' }}>
          <Label required>Machine size</Label>
          <Select onChange={(e) => setSizeId(e.detail.selectedOption.dataset.id ?? defaultSizeId)}>
            {MACHINE_SIZES.map((s) => (
              <Option key={s.id} data-id={s.id} selected={s.id === sizeId}>
                {s.label}
              </Option>
            ))}
          </Select>
        </FlexBox>
      </FlexBox>
    </Dialog>
  );
};
