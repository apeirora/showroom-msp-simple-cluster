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
  shootName: string | null;
  busy: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export const DeleteConfirmDialog = ({ open, shootName, busy, onConfirm, onCancel }: Props) => (
  <Dialog
    open={open}
    headerText="Delete Shoot"
    onClose={onCancel}
    footer={
      <Bar
        endContent={
          <>
            <Button design="Transparent" onClick={onCancel}>
              Cancel
            </Button>
            <Button design="Negative" disabled={busy || !shootName} onClick={onConfirm}>
              {busy ? 'Deleting…' : 'Delete'}
            </Button>
          </>
        }
      />
    }
  >
    <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.75rem', minWidth: 380, padding: '0.5rem 0' }}>
      <MessageStrip design="Critical" hideCloseButton>
        This removes the SAP Integration Shoot resource. Any provider configuration it materialised is removed
        too, so SimpleClusters that reference it will no longer resolve their provider.
      </MessageStrip>
      <div>
        Delete <strong>{shootName ?? 'this Shoot'}</strong>?
      </div>
    </FlexBox>
  </Dialog>
);
