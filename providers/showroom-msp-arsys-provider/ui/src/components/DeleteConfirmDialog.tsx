import { Bar, Button, Dialog, FlexBox, FlexBoxDirection, MessageStrip } from '@ui5/webcomponents-react';

interface Props {
  open: boolean;
  name: string | null;
  busy: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export const DeleteConfirmDialog = ({ open, name, busy, onConfirm, onCancel }: Props) => (
  <Dialog
    open={open}
    headerText="Delete machine"
    onClose={onCancel}
    footer={
      <Bar
        endContent={
          <>
            <Button design="Transparent" onClick={onCancel}>
              Cancel
            </Button>
            <Button design="Negative" disabled={busy || !name} onClick={onConfirm}>
              {busy ? 'Deleting…' : 'Delete'}
            </Button>
          </>
        }
      />
    }
  >
    <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.75rem', minWidth: 380, padding: '0.5rem 0' }}>
      <MessageStrip design="Critical" hideCloseButton>
        This deletes the server ordered from Arsys. This cannot be undone.
      </MessageStrip>
      <div>
        Delete <strong>{name ?? 'this machine'}</strong>?
      </div>
    </FlexBox>
  </Dialog>
);
