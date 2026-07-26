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
  TextArea
} from '@ui5/webcomponents-react';

export interface CreateValues {
  name: string;
  spec: string;
}

interface Props {
  open: boolean;
  busy: boolean;
  // Human label of the Shoot flavor, e.g. "OpenStack" / "IronCore".
  flavor: string;
  // Suggested default resource name (also used as the input placeholder).
  defaultName: string;
  // Ready-to-use default Gardener Shoot manifest to pre-fill the textarea with.
  defaultSpec: string;
  onClose: () => void;
  onSubmit: (values: CreateValues) => void;
}

// A DNS-1123 subdomain, the constraint Kubernetes object names must satisfy.
const NAME_RE = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

export const CreateShootDialog = ({
  open,
  busy,
  flavor,
  defaultName,
  defaultSpec,
  onClose,
  onSubmit
}: Props) => {
  const [name, setName] = useState(defaultName);
  const [spec, setSpec] = useState(defaultSpec);

  useEffect(() => {
    if (open) {
      setName(defaultName);
      setSpec(defaultSpec);
    }
  }, [open, defaultName, defaultSpec]);

  const nameValid = NAME_RE.test(name);
  const canSubmit = nameValid && !busy;

  return (
    <Dialog
      open={open}
      headerText={`New ${flavor} Shoot`}
      onClose={onClose}
      style={{ width: '640px', maxWidth: '95vw' }}
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
                onClick={() => onSubmit({ name: name.trim(), spec })}
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
          A Shoot is an optional Gardener resource provided by SAP Integration {flavor}. The manifest
          below is pre-filled with a ready-to-use default; edit it as needed, or clear it to fall
          back to the operator's built-in default template.
        </MessageStrip>

        <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.35rem' }}>
          <Label required>Name</Label>
          <Input
            value={name}
            onInput={(e) => setName((e.target as unknown as HTMLInputElement).value)}
            valueState={name.length === 0 || nameValid ? 'None' : 'Negative'}
            placeholder={defaultName}
          />
          {!nameValid && name.length > 0 && (
            <span style={{ fontSize: '0.75rem', color: 'var(--sapNegativeColor)' }}>
              Lowercase letters, digits and dashes only (DNS-1123).
            </span>
          )}
        </FlexBox>

        <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.35rem' }}>
          <Label>Gardener Shoot manifest</Label>
          <TextArea
            value={spec}
            rows={12}
            growing={false}
            placeholder="# Leave empty to use the built-in default template"
            onInput={(e) => setSpec((e.target as unknown as HTMLTextAreaElement).value)}
            style={{ fontFamily: 'var(--sapCode_FontFamily, monospace)', width: '100%' }}
          />
        </FlexBox>
      </FlexBox>
    </Dialog>
  );
};
