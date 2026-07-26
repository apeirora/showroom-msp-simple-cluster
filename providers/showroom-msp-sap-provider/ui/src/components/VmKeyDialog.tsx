import { useEffect, useState } from 'react';
import {
  Bar,
  Button,
  Dialog,
  FlexBox,
  FlexBoxDirection,
  TextArea
} from '@ui5/webcomponents-react';

interface Props {
  open: boolean;
  vmName: string | null;
  privateKey: string | null;
  onClose: () => void;
}

/**
 * Presenter for a VM's SSH private key. The portal embeds micro-frontends in a
 * Luigi iframe; on some browsers / sandbox configurations both
 * `showSaveFilePicker` and anchor-click+download fail silently. This dialog
 * guarantees the user can always obtain the key: it is shown inline (select-all
 * + copy), a clipboard button is offered, and a "Save as file" button also
 * tries the usual download path for sandboxes that allow it.
 */
export const VmKeyDialog = ({ open, vmName, privateKey, onClose }: Props) => {
  const [copied, setCopied] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveStatus, setSaveStatus] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setCopied(false);
      setSaveStatus(null);
    }
  }, [open]);

  const filename = vmName ? `${vmName}.pem` : 'vm.pem';

  const copy = async () => {
    if (!privateKey) return;
    try {
      await navigator.clipboard.writeText(privateKey);
      setCopied(true);
      setTimeout(() => setCopied(false), 2500);
    } catch {
      const ta = document.getElementById('ssh-key-contents') as HTMLTextAreaElement | null;
      if (ta) {
        ta.focus();
        ta.select();
      }
    }
  };

  const saveAs = async () => {
    if (!privateKey || !vmName) return;
    setSaving(true);
    setSaveStatus(null);
    try {
      const anyWin = window as unknown as {
        showSaveFilePicker?: (opts: unknown) => Promise<{
          createWritable: () => Promise<{
            write: (data: string) => Promise<void>;
            close: () => Promise<void>;
          }>;
        }>;
      };
      if (typeof anyWin.showSaveFilePicker === 'function') {
        const handle = await anyWin.showSaveFilePicker({
          suggestedName: filename,
          types: [{ description: 'SSH private key', accept: { 'application/x-pem-file': ['.pem'] } }]
        });
        const writable = await handle.createWritable();
        await writable.write(privateKey);
        await writable.close();
        setSaveStatus(`Saved ${filename}`);
        return;
      }
      // Anchor fallback.
      const blob = new Blob([privateKey], { type: 'application/x-pem-file' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      a.target = '_blank';
      a.rel = 'noopener';
      a.style.display = 'none';
      document.body.appendChild(a);
      a.click();
      setTimeout(() => {
        if (a.parentNode) a.parentNode.removeChild(a);
        URL.revokeObjectURL(url);
      }, 250);
      setSaveStatus(`Triggered download: ${filename}`);
    } catch (err) {
      const name = (err as { name?: string }).name;
      if (name !== 'AbortError') {
        setSaveStatus(
          `Could not save directly (${name ?? 'unknown'}). Use Copy, paste into a file named ${filename}.`
        );
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog
      open={open}
      headerText={vmName ? `SSH key — ${vmName}` : 'SSH key'}
      onClose={onClose}
      style={{ width: '760px', maxWidth: '95vw' }}
      footer={
        <Bar
          startContent={
            saveStatus ? (
              <span style={{ fontSize: '0.75rem', color: 'var(--sapContent_LabelColor)' }}>
                {saveStatus}
              </span>
            ) : null
          }
          endContent={
            <>
              <Button design="Transparent" onClick={onClose}>
                Close
              </Button>
              <Button design="Transparent" icon="copy" onClick={() => void copy()} disabled={!privateKey}>
                {copied ? 'Copied!' : 'Copy to clipboard'}
              </Button>
              <Button
                design="Emphasized"
                icon="download"
                onClick={() => void saveAs()}
                disabled={!privateKey || saving}
              >
                {saving ? 'Saving…' : 'Download key'}
              </Button>
            </>
          }
        />
      }
    >
      <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.5rem', padding: '0.5rem 0' }}>
        <TextArea
          id="ssh-key-contents"
          value={privateKey ?? ''}
          readonly
          growing
          growingMaxRows={24}
          style={{ width: '100%', fontFamily: 'monospace', fontSize: '0.82rem' }}
        />
      </FlexBox>
    </Dialog>
  );
};
