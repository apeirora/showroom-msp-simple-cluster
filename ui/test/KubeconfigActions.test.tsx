import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ClusterDetail } from '../src/components/ClusterDetail';
import { ClusterList } from '../src/components/ClusterList';
import type { SimpleCluster } from '../src/types';

type TableRow = Record<string, unknown>;
type TableColumn = {
  Header: string;
  accessor: string;
  Cell?: (props: { value: unknown; row: { original: TableRow } }) => ReactNode;
};

vi.mock('@ui5/webcomponents-fiori/dist/illustrations/NoData.js', () => ({}));
vi.mock('@ui5/webcomponents-react', () => ({
  // Preserve native disabled, click and accessible-name behavior. Render every
  // table cell, including Actions, so the tests exercise the component's gate.
  Button: ({ design: _design, icon: _icon, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & {
    design?: string; icon?: string;
  }) => <button {...props} />,
  Dialog: ({ open, children, footer }: { open: boolean; children: ReactNode; footer: ReactNode }) =>
    open ? <div role="dialog">{children}{footer}</div> : null,
  FlexBox: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  FlexBoxDirection: { Column: 'column' },
  FlexBoxJustifyContent: { Center: 'center', SpaceBetween: 'space-between' },
  Bar: ({ startContent, endContent }: { startContent: ReactNode; endContent: ReactNode }) =>
    <div>{startContent}{endContent}</div>,
  Label: ({ children }: { children: ReactNode }) => <span>{children}</span>,
  Title: ({ children }: { children: ReactNode }) => <h3>{children}</h3>,
  Panel: ({ children }: { children: ReactNode }) => <section>{children}</section>,
  MessageStrip: ({ children }: { children: ReactNode }) => <div role="alert">{children}</div>,
  ObjectStatus: ({ children }: { children: ReactNode }) => <span>{children}</span>,
  BusyIndicator: () => <span>Loading</span>,
  IllustratedMessage: () => <span>Empty</span>,
  AnalyticalTable: ({ data, columns }: { data: TableRow[]; columns: TableColumn[] }) => (
    <table><tbody>{data.map((row, index) => <tr key={index}>
      {columns.map((column) => <td key={column.accessor}>
        {column.Cell
          ? <column.Cell value={row[column.accessor]} row={{ original: row }} />
          : String(row[column.accessor] ?? '')}
      </td>)}
    </tr>)}</tbody></table>
  )
}));

afterEach(cleanup);

const cluster = (status?: SimpleCluster['status'], deleting = false): SimpleCluster => ({
  metadata: {
    name: 'synthetic-cluster',
    ...(deleting ? { deletionTimestamp: '2026-09-07T10:00:00Z' } : {})
  },
  spec: {},
  ...(status ? { status } : {})
});

const withoutReference = [
  { label: 'absent status', value: cluster() },
  { label: 'Ready without a reference', value: cluster({ phase: 'Ready' }) }
];

const reference = { name: 'synthetic-kubeconfig' };
const withReference = [
  { label: 'Ready', value: cluster({ phase: 'Ready', kubeconfigSecretRef: reference }) },
  { label: 'Pending', value: cluster({ phase: 'Pending', kubeconfigSecretRef: reference }) },
  { label: 'active Error', value: cluster({ phase: 'Ready', kubeconfigSecretRef: reference,
    conditions: [{ type: 'Error', status: 'True', reason: 'SyntheticError' }] }) },
  { label: 'Deleting', value: cluster({ phase: 'Ready', kubeconfigSecretRef: reference }, true) },
  { label: 'expired reference', value: cluster({ phase: 'Ready', kubeconfigSecretRef: reference,
    kubeconfigExpiresAt: '2000-01-01T00:00:00Z' }) }
];

describe('kubeconfig action availability', () => {
  it.each(withoutReference)('shows a disabled list action for $label and does not invoke it', ({ value }) => {
    const onAction = vi.fn();
    render(<ClusterList clusters={[value]} loading={false} onCreate={vi.fn()} onAction={onAction} />);
    const button = screen.getByRole('button', { name: 'Download kubeconfig for synthetic-cluster' }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    fireEvent.click(button);
    expect(onAction).not.toHaveBeenCalled();
  });

  it.each(withoutReference)('hides the detail download action for $label', ({ value }) => {
    render(<ClusterDetail open cluster={value} onClose={vi.fn()} onDownloadKubeconfig={vi.fn()} />);
    expect(screen.queryByRole('button', { name: 'Download kubeconfig' })).toBeNull();
  });

  // Current contract: a reference enables fetching. Phase, deletion and expiry
  // do not gate this control; backend authorization still governs Secret access.
  it.each(withReference)('enables and invokes the list action with a reference during $label', ({ value }) => {
    const onAction = vi.fn();
    render(<ClusterList clusters={[value]} loading={false} onCreate={vi.fn()} onAction={onAction} />);
    const button = screen.getByRole('button', { name: 'Download kubeconfig for synthetic-cluster' }) as HTMLButtonElement;
    expect(button.disabled).toBe(false);
    fireEvent.click(button);
    expect(onAction).toHaveBeenCalledExactlyOnceWith('download-kubeconfig', value);
  });

  it.each(withReference)('shows and invokes the detail action with a reference during $label', ({ value }) => {
    const onDownload = vi.fn();
    render(<ClusterDetail open cluster={value} onClose={vi.fn()} onDownloadKubeconfig={onDownload} />);
    const button = screen.getByRole('button', { name: 'Download kubeconfig' }) as HTMLButtonElement;
    expect(button.disabled).toBe(false);
    fireEvent.click(button);
    expect(onDownload).toHaveBeenCalledExactlyOnceWith(value);
  });

  it('hides the detail download action when no callback is supplied', () => {
    render(<ClusterDetail open cluster={withReference[0].value} onClose={vi.fn()} />);
    expect(screen.queryByRole('button', { name: 'Download kubeconfig' })).toBeNull();
  });
});
