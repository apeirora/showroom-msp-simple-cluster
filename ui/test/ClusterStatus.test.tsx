import type { ReactNode } from 'react';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ClusterDetail } from '../src/components/ClusterDetail';
import { ClusterList } from '../src/components/ClusterList';
import type { SimpleCluster } from '../src/types';

const table = vi.hoisted(() => ({ data: [] as Array<Record<string, any>>, columns: [] as any[] }));
vi.mock('@ui5/webcomponents-fiori/dist/illustrations/NoData.js', () => ({}));
vi.mock('@ui5/webcomponents-react', () => ({
  Dialog: ({ open, children, footer }: { open: boolean; children: ReactNode; footer: ReactNode }) =>
    open ? <div role="dialog">{children}{footer}</div> : null,
  FlexBox: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  FlexBoxDirection: { Column: 'column' }, FlexBoxJustifyContent: { Center: 'center', SpaceBetween: 'space-between' },
  Bar: ({ endContent }: { endContent: ReactNode }) => <div>{endContent}</div>,
  Label: ({ children }: { children: ReactNode }) => <span>{children}</span>,
  Title: ({ children }: { children: ReactNode }) => <h3>{children}</h3>,
  Panel: ({ children }: { children: ReactNode }) => <section>{children}</section>,
  MessageStrip: ({ children, design }: { children: ReactNode; design: string }) =>
    <div role="alert" data-design={design}>{children}</div>,
  ObjectStatus: ({ children, state }: { children: ReactNode; state: string }) =>
    <span data-state={state}>{children}</span>,
  Button: ({ children }: { children: ReactNode }) => <button>{children}</button>,
  BusyIndicator: () => <span>Loading</span>, IllustratedMessage: () => <span>Empty</span>,
  AnalyticalTable: ({ data, columns }: { data: Array<Record<string, any>>; columns: any[] }) => {
    table.data = data; table.columns = columns;
    const phase = columns.find((column) => column.accessor === 'phase');
    return <div>{data.map((row) => <div key={row.name}>
      <phase.Cell value={row.phase} row={{ original: row }} />
    </div>)}</div>;
  }
}));
afterEach(cleanup);

const cluster = (phase: string, error = 'True'): SimpleCluster => ({
  metadata: { name: 'invalid-region' }, spec: {}, status: { phase,
    conditions: [{ type: 'Error', status: error, reason: 'InputError', message: 'unsupported region' }] }
});

describe('error-aware cluster components', () => {
  it.each(['Pending', 'Ready'])('renders and filters list rows as Error, not stale %s', (phase) => {
    render(<ClusterList clusters={[cluster(phase)]} loading={false} onCreate={vi.fn()} onAction={vi.fn()} />);
    expect(screen.getByText('Error').getAttribute('data-state')).toBe('Negative');
    expect(screen.getByText('InputError: unsupported region')).toBeTruthy();
    // The actual AnalyticalTable accessor receives Error: filtering/sorting
    // must not retain the underlying Pending/Ready value behind a red badge.
    const accessor = table.columns.find((column) => column.Header === 'Phase').accessor;
    expect(table.data.filter((row) => row[accessor] === phase)).toHaveLength(0);
    expect(table.data.filter((row) => row[accessor] === 'Error')).toHaveLength(1);
  });

  it.each(['Pending', 'Ready'])('shows failure details while preserving last observed %s', (phase) => {
    render(<ClusterDetail open cluster={cluster(phase)} onClose={vi.fn()} />);
    expect(screen.getByRole('alert').textContent).toBe('InputError: unsupported region');
    expect(screen.getByRole('alert').getAttribute('data-design')).toBe('Negative');
    expect(screen.getByText('True').getAttribute('data-state')).toBe('Negative');
    expect(screen.getByText('Last observed phase')).toBeTruthy();
    expect(screen.getByText(phase)).toBeTruthy();
  });

  it('keeps Error=False neutral and an ordinary Ready=True positive', () => {
    const value = cluster('Ready', 'False');
    value.status!.conditions!.push({ type: 'Ready', status: 'True' });
    render(<ClusterDetail open cluster={value} onClose={vi.fn()} />);
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.getByText('False').getAttribute('data-state')).toBe('None');
    expect(screen.getByText('True').getAttribute('data-state')).toBe('Positive');
  });

  it('keeps deletion as the headline in both components while retaining the diagnostic', () => {
    const value = cluster('Ready');
    value.metadata.deletionTimestamp = '2026-09-04T21:00:00Z';
    const { unmount } = render(<ClusterList clusters={[value]} loading={false} onCreate={vi.fn()} onAction={vi.fn()} />);
    expect(table.data[0].phase).toBe('Deleting');
    expect(screen.getByText('Deleting')).toBeTruthy();
    unmount();
    render(<ClusterDetail open cluster={value} onClose={vi.fn()} />);
    expect(screen.getByText('Deleting')).toBeTruthy();
    expect(screen.queryByText('Last observed phase')).toBeNull();
    expect(screen.getByRole('alert').textContent).toBe('InputError: unsupported region');
  });
});

describe('cluster detail Quantity presentation', () => {
  it('displays JSONString quantities without quote wrappers in both pools without mutating their source', () => {
    const value: SimpleCluster = { metadata: { name: 'gateway-quantities' }, spec: {
      controlPlane: { nodes: 1, requested: { cpu: '"500m"', memory: '"2Gi"' } },
      workers: { autoscaling: { min: 1, max: 2 }, requested: { cpu: '"2"', memory: '"4Gi"' } }
    } };
    const original = structuredClone(value);
    render(<ClusterDetail open cluster={value} onClose={vi.fn()} />);
    for (const quantity of ['500m', '2Gi', '2', '4Gi']) {
      expect(screen.getByText(quantity, { exact: true })).toBeTruthy();
      expect(screen.queryByText(JSON.stringify(quantity), { exact: true })).toBeNull();
    }
    expect(value).toEqual(original);
  });

  it('preserves plain Kubernetes quantity spelling and numeric fixture scalars', () => {
    const value: SimpleCluster = { metadata: { name: 'plain-quantities' }, spec: {
      controlPlane: { requested: { cpu: '500m', memory: '2Gi' } },
      workers: { requested: { cpu: 2, memory: '4Gi' } }
    } };
    render(<ClusterDetail open cluster={value} onClose={vi.fn()} />);
    for (const quantity of ['500m', '2Gi', '2', '4Gi']) {
      expect(screen.getByText(quantity, { exact: true })).toBeTruthy();
    }
  });
});
