import { useEffect, useState } from 'react';
import {
  Bar,
  Button,
  Dialog,
  FlexBox,
  FlexBoxDirection,
  MessageStrip,
  TextArea
} from '@ui5/webcomponents-react';

interface Props {
  open: boolean;
  clusterName: string | null;
  kubeconfig: string | null;
  onClose: () => void;
}

/**
 * Presenter for a kubeconfig. The portal embeds micro-frontends in a Luigi
 * iframe; on some browsers / sandbox configurations both `showSaveFilePicker`
 * and anchor-click+download fail silently. This dialog guarantees the user can
 * always obtain the kubeconfig: it is visible inline (select-all + copy), a
 * clipboard button is offered, and a "Save as file" button also tries the
 * usual download path for sandboxes that allow it.
 */
export const KubeconfigDialog = ({ open, clusterName, kubeconfig, onClose }: Props) => {
  const [copied, setCopied] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveStatus, setSaveStatus] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setCopied(false);
      setSaveStatus(null);
    }
  }, [open]);

  const copy = async () => {
    if (!kubeconfig) return;
    try {
      await navigator.clipboard.writeText(kubeconfig);
      setCopied(true);
      setTimeout(() => setCopied(false), 2500);
    } catch {
      const ta = document.getElementById('kubeconfig-contents') as HTMLTextAreaElement | null;
      if (ta) {
        ta.focus();
        ta.select();
      }
    }
  };

  const saveAs = async () => {
    if (!kubeconfig || !clusterName) return;
    setSaving(true);
    setSaveStatus(null);
    const filename = `${clusterName}.kubeconfig`;
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
          types: [
            {
              description: 'Kubeconfig',
              accept: { 'application/yaml': ['.yaml', '.yml', '.kubeconfig'] }
            }
          ]
        });
        const writable = await handle.createWritable();
        await writable.write(kubeconfig);
        await writable.close();
        setSaveStatus(`Saved ${filename}`);
        return;
      }
      // Anchor fallback.
      const blob = new Blob([kubeconfig], { type: 'application/yaml' });
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
      headerText={clusterName ? `Kubeconfig — ${clusterName}` : 'Kubeconfig'}
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
              <Button
                design="Transparent"
                icon="copy"
                onClick={() => void copy()}
                disabled={!kubeconfig}
              >
                {copied ? 'Copied!' : 'Copy to clipboard'}
              </Button>
              <Button
                design="Emphasized"
                icon="save"
                onClick={() => void saveAs()}
                disabled={!kubeconfig || saving}
              >
                {saving ? 'Saving…' : 'Save as file'}
              </Button>
            </>
          }
        />
      }
    >
      <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.5rem', padding: '0.5rem 0' }}>
        <MessageStrip design="Information" hideCloseButton>
          If the download doesn't start (some embedded iframes block downloads), use
          <strong> Copy to clipboard </strong>
          and paste into a file named <code>{clusterName ? `${clusterName}.kubeconfig` : 'cluster.kubeconfig'}</code>.
        </MessageStrip>
        <TextArea
          id="kubeconfig-contents"
          value={kubeconfig ?? ''}
          readonly
          growing
          growingMaxRows={24}
          style={{ width: '100%', fontFamily: 'monospace', fontSize: '0.82rem' }}
        />
      </FlexBox>
    </Dialog>
  );
};
