import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { KubeconfigDialog } from '../src/components/KubeconfigDialog';

vi.mock('@ui5/webcomponents-react', () => ({
  Button: ({ design: _design, icon: _icon, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & {
    design?: string; icon?: string;
  }) => <button {...props} />,
  Dialog: ({ open, children, footer }: { open: boolean; children: ReactNode; footer: ReactNode }) =>
    open ? <div role="dialog">{children}{footer}</div> : null,
  Bar: ({ startContent, endContent }: { startContent: ReactNode; endContent: ReactNode }) =>
    <div>{startContent}{endContent}</div>,
  FlexBox: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  FlexBoxDirection: { Column: 'column' },
  MessageStrip: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  TextArea: ({ id, value, readonly }: { id: string; value: string; readonly: boolean }) =>
    <textarea id={id} value={value} readOnly={readonly} />
}));

// Only inert, synthetic content is ever passed to clipboard or file API doubles.
const syntheticConfig = 'apiVersion: v1\nkind: Config\nclusters: []\nusers: []\ncontexts: []\n';
const writeText = vi.fn<(value: string) => Promise<void>>();
beforeEach(() => {
  vi.stubGlobal('navigator', { clipboard: { writeText } });
  vi.stubGlobal('showSaveFilePicker', undefined);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

const showDialog = (kubeconfig: string | null = syntheticConfig) => render(
  <KubeconfigDialog open clusterName="synthetic-cluster" kubeconfig={kubeconfig} onClose={vi.fn()} />
);

describe('kubeconfig presentation controls', () => {
  it('keeps copy and save disabled while content is pending', () => {
    const picker = vi.fn();
    vi.stubGlobal('showSaveFilePicker', picker);
    showDialog(null);
    for (const name of ['Copy to clipboard', 'Save as file']) {
      const button = screen.getByRole('button', { name }) as HTMLButtonElement;
      expect(button.disabled).toBe(true);
      fireEvent.click(button);
    }
    expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('');
    expect(writeText).not.toHaveBeenCalled();
    expect(picker).not.toHaveBeenCalled();
  });

  it('copies the supplied content and acknowledges successful clipboard access', async () => {
    writeText.mockResolvedValue(undefined);
    showDialog();
    fireEvent.click(screen.getByRole('button', { name: 'Copy to clipboard' }));
    await screen.findByRole('button', { name: 'Copied!' });
    expect(writeText).toHaveBeenCalledExactlyOnceWith(syntheticConfig);
    expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe(syntheticConfig);
  });

  it('selects the visible content for manual copy when clipboard access fails', async () => {
    writeText.mockRejectedValue(new Error('synthetic clipboard denial'));
    showDialog();
    const textarea = screen.getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.click(screen.getByRole('button', { name: 'Copy to clipboard' }));
    await waitFor(() => expect(document.activeElement).toBe(textarea));
    expect(textarea.selectionStart).toBe(0);
    expect(textarea.selectionEnd).toBe(syntheticConfig.length);
    expect(screen.queryByRole('button', { name: 'Copied!' })).toBeNull();
  });

  it('writes and closes the selected file, disabling Save until the write completes', async () => {
    let finishWrite!: () => void;
    const write = vi.fn(() => new Promise<void>((resolve) => { finishWrite = resolve; }));
    const close = vi.fn().mockResolvedValue(undefined);
    const picker = vi.fn().mockResolvedValue({ createWritable: vi.fn().mockResolvedValue({ write, close }) });
    vi.stubGlobal('showSaveFilePicker', picker);
    showDialog();
    fireEvent.click(screen.getByRole('button', { name: 'Save as file' }));
    await waitFor(() => expect(write).toHaveBeenCalledExactlyOnceWith(syntheticConfig));
    expect((screen.getByRole('button', { name: 'Saving…' }) as HTMLButtonElement).disabled).toBe(true);
    expect(close).not.toHaveBeenCalled();
    expect(picker).toHaveBeenCalledWith(expect.objectContaining({ suggestedName: 'synthetic-cluster.kubeconfig' }));
    await act(async () => { finishWrite(); });
    expect(close).toHaveBeenCalledOnce();
    expect(screen.getByText('Saved synthetic-cluster.kubeconfig')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Save as file' }) as HTMLButtonElement).disabled).toBe(false);
  });

  it('uses a temporary download anchor when the file picker is unavailable', () => {
    vi.useFakeTimers();
    const createObjectURL = vi.fn((_object: Blob | MediaSource) => 'blob:synthetic-download');
    const revokeObjectURL = vi.fn();
    const NativeURL = URL;
    vi.stubGlobal('URL', class extends NativeURL {
      static createObjectURL = createObjectURL;
      static revokeObjectURL = revokeObjectURL;
    });
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    showDialog();
    fireEvent.click(screen.getByRole('button', { name: 'Save as file' }));
    const anchor = document.querySelector('a[download]') as HTMLAnchorElement;
    expect(anchor.download).toBe('synthetic-cluster.kubeconfig');
    expect(anchor.href).toBe('blob:synthetic-download');
    expect(click).toHaveBeenCalledOnce();
    expect(createObjectURL.mock.calls[0][0]).toBeInstanceOf(Blob);
    expect(screen.getByText('Triggered download: synthetic-cluster.kubeconfig')).toBeTruthy();
    act(() => { vi.advanceTimersByTime(250); });
    expect(document.querySelector('a[download]')).toBeNull();
    expect(revokeObjectURL).toHaveBeenCalledExactlyOnceWith('blob:synthetic-download');
  });

  it('offers manual copy when the file picker rejects direct saving', async () => {
    vi.stubGlobal('showSaveFilePicker', vi.fn().mockRejectedValue(new DOMException('synthetic denial', 'SecurityError')));
    showDialog();
    fireEvent.click(screen.getByRole('button', { name: 'Save as file' }));
    expect(await screen.findByText('Could not save directly (SecurityError). Use Copy, paste into a file named synthetic-cluster.kubeconfig.')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Save as file' }) as HTMLButtonElement).disabled).toBe(false);
  });

  it('does not report a user-cancelled file picker as a save failure', async () => {
    const picker = vi.fn().mockRejectedValue(new DOMException('synthetic cancellation', 'AbortError'));
    vi.stubGlobal('showSaveFilePicker', picker);
    showDialog();
    fireEvent.click(screen.getByRole('button', { name: 'Save as file' }));
    await waitFor(() => expect((screen.getByRole('button', { name: 'Save as file' }) as HTMLButtonElement).disabled).toBe(false));
    expect(picker).toHaveBeenCalledOnce();
    expect(screen.queryByText(/Could not save directly/)).toBeNull();
  });
});
