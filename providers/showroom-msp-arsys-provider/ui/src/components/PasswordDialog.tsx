import { useEffect, useState } from 'react';
import {
  Bar,
  BusyIndicator,
  Button,
  Dialog,
  FlexBox,
  FlexBoxDirection,
  Input,
  Label,
  MessageStrip
} from '@ui5/webcomponents-react';
import type { Credential } from '../lib/secret';

interface Props {
  open: boolean;
  machineName: string | null;
  credential: Credential | null;
  onClose: () => void;
}

/**
 * Presenter for a machine's generated root credentials. The password is fetched
 * on demand from the synced Secret; a copy button is offered since the value is
 * short and copy-paste is the expected way to use it.
 */
export const PasswordDialog = ({ open, machineName, credential, onClose }: Props) => {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!open) setCopied(false);
  }, [open]);

  const copy = async () => {
    if (!credential) return;
    try {
      await navigator.clipboard.writeText(credential.password);
      setCopied(true);
      setTimeout(() => setCopied(false), 2500);
    } catch {
      const el = document.getElementById('password-contents') as HTMLInputElement | null;
      if (el) el.focus();
    }
  };

  return (
    <Dialog
      open={open}
      headerText={machineName ? `Credentials — ${machineName}` : 'Credentials'}
      onClose={onClose}
      style={{ width: '460px', maxWidth: '95vw' }}
      footer={
        <Bar
          endContent={
            <>
              <Button design="Transparent" onClick={onClose}>
                Close
              </Button>
              <Button design="Emphasized" icon="copy" onClick={() => void copy()} disabled={!credential}>
                {copied ? 'Copied!' : 'Copy password'}
              </Button>
            </>
          }
        />
      }
    >
      <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.5rem', padding: '0.5rem 0' }}>
        {!credential ? (
          <FlexBox style={{ gap: '0.5rem', alignItems: 'center', padding: '0.5rem 0' }}>
            <BusyIndicator active size="S" />
            <span>Fetching credentials…</span>
          </FlexBox>
        ) : (
          <>
            <MessageStrip design="Information" hideCloseButton>
              Root login for <code>{machineName ?? 'this machine'}</code>. Connect with
              <code> ssh {credential.username}@&lt;address&gt;</code> and this password.
            </MessageStrip>
            <Label>Username</Label>
            <Input value={credential.username} readonly style={{ width: '100%' }} />
            <Label>Password</Label>
            <Input
              id="password-contents"
              value={credential.password}
              readonly
              style={{ width: '100%', fontFamily: 'monospace' }}
            />
          </>
        )}
      </FlexBox>
    </Dialog>
  );
};
