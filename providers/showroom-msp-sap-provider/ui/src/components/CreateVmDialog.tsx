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
import { MACHINE_CLASSES, defaultMachineClassId, type CreateInput } from '../lib/vm';

interface Props {
  open: boolean;
  busy: boolean;
  onClose: () => void;
  onSubmit: (values: CreateInput) => void;
}

// A DNS-1123 subdomain, the constraint Kubernetes object names must satisfy.
const NAME_RE = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

export const CreateVmDialog = ({ open, busy, onClose, onSubmit }: Props) => {
  const [name, setName] = useState('');
  const [machineClassId, setMachineClassId] = useState(defaultMachineClassId);

  useEffect(() => {
    if (open) {
      setName('');
      setMachineClassId(defaultMachineClassId);
    }
  }, [open]);

  const nameValid = NAME_RE.test(name);
  const canSubmit = nameValid && !busy;

  return (
    <Dialog
      open={open}
      headerText="New IronCore VM"
      onClose={onClose}
      style={{ width: '480px', maxWidth: '95vw' }}
      footer={
        <Bar
          endContent={
            <>
              <Button design="Transparent" onClick={onClose}>
                Cancel
              </Button>
              <Button
                design="Emphasized"
                disabled={!canSubmit}
                onClick={() => onSubmit({ name: name.trim(), machineClassId })}
              >
                {busy ? 'Creating…' : 'Create'}
              </Button>
            </>
          }
        />
      }
    >
      <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '1rem', padding: '0.5rem 0' }}>
        <MessageStrip design="Information" hideCloseButton>
          Provisions one IronCore Machine. Network, image and credentials use the SAP Integration
          provider defaults; a per-VM SSH keypair is generated automatically.
        </MessageStrip>

        <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.35rem' }}>
          <Label required>Name</Label>
          <Input
            value={name}
            onInput={(e) => setName((e.target as unknown as HTMLInputElement).value)}
            valueState={name.length === 0 || nameValid ? 'None' : 'Negative'}
            placeholder="my-vm"
          />
          {!nameValid && name.length > 0 && (
            <span style={{ fontSize: '0.75rem', color: 'var(--sapNegativeColor)' }}>
              Lowercase letters, digits and dashes only (DNS-1123).
            </span>
          )}
        </FlexBox>

        <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.35rem' }}>
          <Label required>Machine class</Label>
          <Select
            onChange={(e) =>
              setMachineClassId(e.detail.selectedOption.dataset.id ?? defaultMachineClassId)
            }
          >
            {MACHINE_CLASSES.map((c) => (
              <Option key={c.id} data-id={c.id} selected={c.id === machineClassId}>
                {c.label}
              </Option>
            ))}
          </Select>
        </FlexBox>
      </FlexBox>
    </Dialog>
  );
};
