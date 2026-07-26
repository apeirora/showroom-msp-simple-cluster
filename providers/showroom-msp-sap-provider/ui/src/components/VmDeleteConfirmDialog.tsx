import {
  Bar,
  Button,
  Dialog,
  FlexBox,
  FlexBoxDirection,
  MessageStrip
} from '@ui5/webcomponents-react';

interface Props {
  open: boolean;
  vmName: string | null;
  busy: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export const VmDeleteConfirmDialog = ({ open, vmName, busy, onConfirm, onCancel }: Props) => (
  <Dialog
    open={open}
    headerText="Delete VM"
    onClose={onCancel}
    footer={
      <Bar
        endContent={
          <>
            <Button design="Transparent" onClick={onCancel}>
              Cancel
            </Button>
            <Button design="Negative" disabled={busy || !vmName} onClick={onConfirm}>
              {busy ? 'Deleting…' : 'Delete'}
            </Button>
          </>
        }
      />
    }
  >
    <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.75rem', minWidth: 380, padding: '0.5rem 0' }}>
      <MessageStrip design="Critical" hideCloseButton>
        This removes the IronCore VM and its generated SSH keypair Secret. The underlying Machine is
        deprovisioned and cannot be recovered.
      </MessageStrip>
      <div>
        Delete <strong>{vmName ?? 'this VM'}</strong>?
      </div>
    </FlexBox>
  </Dialog>
);
