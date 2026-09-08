import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from '../src/App';
import type { GraphqlClientOptions } from '../src/lib/graphql';
import type { RuntimeContext } from '../src/lib/luigi';
import type { SimpleCluster } from '../src/types';

const harness = vi.hoisted(() => ({
  context: null as ((context: RuntimeContext) => void) | null,
  subscriptions: [] as Array<{ onEvent: () => void; opts: GraphqlClientOptions; namespace: string }>,
  list: vi.fn<(opts: GraphqlClientOptions, namespace: string) => Promise<SimpleCluster[]>>(),
  fetchKubeconfig: vi.fn()
}));

vi.mock('../src/lib/luigi', () => ({
  emptyRuntimeContext: () => ({ source: 'pending', graphqlUrl: null, token: null, workspacePath: null, namespace: null }),
  subscribeToLuigi: (callback: (context: RuntimeContext) => void) => {
    harness.context = callback;
    return () => { harness.context = null; };
  }
}));
vi.mock('../src/lib/simplecluster', () => ({
  listSimpleClusters: harness.list,
  subscribeSimpleClusters: (opts: GraphqlClientOptions, namespace: string, onEvent: () => void) => {
    harness.subscriptions.push({ opts, namespace, onEvent });
    return vi.fn();
  },
  createSimpleCluster: vi.fn(),
  deleteSimpleCluster: vi.fn()
}));
vi.mock('../src/lib/secret', () => ({ fetchKubeconfig: harness.fetchKubeconfig }));
vi.mock('../src/lib/config', () => {
  const config = {
    providers: ['synthetic'], providerSources: [], cpu: ['1'], memory: ['1Gi'],
    controlPlane: { min: 1, max: 1, default: 1 }, workers: { min: 1, max: 1, default: 1 }
  };
  return { fallbackClusterConfig: () => config, loadClusterConfig: vi.fn().mockResolvedValue(config) };
});
// Creation, deletion and credential presentation are outside this regression.
// App, ClusterList and ClusterDetail are real, including their state and events.
vi.mock('../src/components/CreateClusterDialog', () => ({ CreateClusterDialog: () => null }));
vi.mock('../src/components/DeleteConfirmDialog', () => ({ DeleteConfirmDialog: () => null }));
vi.mock('../src/components/KubeconfigDialog', () => ({ KubeconfigDialog: () => null }));
vi.mock('@ui5/webcomponents-fiori/dist/illustrations/NoData.js', () => ({}));
vi.mock('@ui5/webcomponents-fiori/dist/illustrations/UnableToLoad.js', () => ({}));

type TableRow = Record<string, unknown>;
type TableColumn = {
  accessor: string;
  Cell?: (props: { value: unknown; row: { original: TableRow } }) => ReactNode;
};
vi.mock('@ui5/webcomponents-react', () => ({
  Button: ({ design: _design, icon: _icon, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & {
    design?: string; icon?: string;
  }) => <button {...props} />,
  Dialog: ({ open, children, footer, headerText }: {
    open: boolean; children: ReactNode; footer: ReactNode; headerText: string;
  }) => open ? <div role="dialog" aria-label={headerText}>{children}{footer}</div> : null,
  FlexBox: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  FlexBoxDirection: { Column: 'column' },
  FlexBoxAlignItems: { Center: 'center' },
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
  ShellBar: () => <header>Simple Cluster</header>,
  Toast: () => null,
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

const initialContext: RuntimeContext = {
  source: 'luigi', graphqlUrl: 'https://gateway.example.invalid/root/graphql',
  token: 'synthetic-auth-a', workspacePath: 'root:orgs:synthetic:a', namespace: 'synthetic-a'
};
const cluster = (overrides: Partial<SimpleCluster> = {}): SimpleCluster => ({
  metadata: { name: 'synthetic-cluster', namespace: 'synthetic-a', uid: 'synthetic-uid-a' },
  spec: {}, status: { phase: 'Pending' }, ...overrides
});
const readyCluster = (): SimpleCluster => cluster({
  status: { phase: 'Ready', kubeconfigSecretRef: { name: 'synthetic-kubeconfig' } }
});
const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
};

beforeEach(() => {
  harness.context = null;
  harness.subscriptions = [];
  harness.list.mockReset().mockResolvedValue([]);
  harness.fetchKubeconfig.mockReset();
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Unexpected network request in synthetic App test')));
});
afterEach(() => {
  cleanup();
  expect(fetch).not.toHaveBeenCalled();
  expect(harness.fetchKubeconfig).not.toHaveBeenCalled();
  vi.unstubAllGlobals();
});

const mountWithDetails = async (initial = cluster()) => {
  harness.list.mockResolvedValueOnce([initial]);
  render(<App />);
  await act(async () => { harness.context!(initialContext); });
  fireEvent.click(screen.getByRole('button', { name: `View details for ${initial.metadata.name}` }));
  return screen.getByRole('dialog', { name: initial.metadata.name });
};
const notify = async () => {
  const subscription = harness.subscriptions[harness.subscriptions.length - 1];
  await act(async () => { subscription.onEvent(); });
};
const reloadWith = async (items: SimpleCluster[]) => {
  harness.list.mockResolvedValueOnce(items);
  await notify();
};

describe('open cluster detail follows the current list identity', () => {
  it('updates Pending details and exposes the new Secret reference without reopening', async () => {
    const dialog = await mountWithDetails();
    expect(within(dialog).getByText('Pending')).toBeTruthy();
    expect(within(dialog).queryByRole('button', { name: 'Download kubeconfig' })).toBeNull();
    await reloadWith([readyCluster()]);
    const updated = screen.getByRole('dialog', { name: 'synthetic-cluster' });
    expect(within(updated).getByText('Ready')).toBeTruthy();
    expect(within(updated).getByText('synthetic-kubeconfig')).toBeTruthy();
    expect((within(updated).getByRole('button', { name: 'Download kubeconfig' }) as HTMLButtonElement).disabled).toBe(false);
    expect(within(updated).queryByText('Pending')).toBeNull();
  });

  it('updates failure conditions in the open details while preserving the last phase', async () => {
    await mountWithDetails();
    await reloadWith([cluster({ status: { phase: 'Pending', conditions: [
      { type: 'Error', status: 'True', reason: 'SyntheticFailure', message: 'synthetic provider rejected input' }
    ] } })]);
    const dialog = screen.getByRole('dialog', { name: 'synthetic-cluster' });
    expect(within(dialog).getByRole('alert').textContent).toBe('SyntheticFailure: synthetic provider rejected input');
    expect(within(dialog).getAllByText('Error').length).toBeGreaterThan(0);
    expect(within(dialog).getByText('Last observed phase')).toBeTruthy();
  });

  it('closes details when the selected UID disappears', async () => {
    await mountWithDetails();
    await reloadWith([]);
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('closes instead of switching to a replacement object with the same name and a different UID', async () => {
    await mountWithDetails();
    const replacement = readyCluster();
    replacement.metadata = { ...replacement.metadata, uid: 'synthetic-replacement-uid' };
    await reloadWith([replacement]);
    expect(screen.getByRole('button', { name: 'View details for synthetic-cluster' })).toBeTruthy();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('closes on refresh when a missing UID cannot establish that the object is still the same', async () => {
    const metadata = { name: 'synthetic-cluster', namespace: 'synthetic-a' };
    await mountWithDetails(cluster({ metadata }));
    await reloadWith([cluster({ metadata, status: { phase: 'Ready' } })]);
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(within(screen.getByRole('table')).getByText('Ready')).toBeTruthy();
  });

  it('preserves current details and shows a failed reload as an error instead of an empty list', async () => {
    await mountWithDetails(readyCluster());
    harness.list.mockRejectedValueOnce(new Error('synthetic list unavailable'));
    await notify();
    expect(screen.getByRole('alert').textContent).toBe('synthetic list unavailable');
    const dialog = screen.getByRole('dialog', { name: 'synthetic-cluster' });
    expect(within(dialog).getByText('Ready')).toBeTruthy();
    expect(within(dialog).getByRole('button', { name: 'Download kubeconfig' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'View details for synthetic-cluster' })).toBeTruthy();
  });
});

describe('detail and list scope freshness', () => {
  it.each([
    { label: 'namespace', change: { namespace: 'synthetic-b' } },
    { label: 'workspace', change: { workspacePath: 'root:orgs:synthetic:b' } },
    { label: 'authentication', change: { token: 'synthetic-auth-b' } }
  ])('hides old details immediately when $label changes, before the new list resolves', async ({ change }) => {
    await mountWithDetails(readyCluster());
    const next = deferred<SimpleCluster[]>();
    harness.list.mockReturnValueOnce(next.promise);
    act(() => { harness.context!({ ...initialContext, ...change }); });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(screen.queryByRole('button', { name: 'View details for synthetic-cluster' })).toBeNull();
    await act(async () => { next.resolve([]); });
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('ignores an old scope result that arrives after the new scope list', async () => {
    await mountWithDetails();
    const older = deferred<SimpleCluster[]>();
    harness.list.mockReturnValueOnce(older.promise);
    await notify();
    const current = cluster({ metadata: {
      name: 'new-scope-cluster', namespace: 'synthetic-b', uid: 'synthetic-new-scope-uid'
    }, status: { phase: 'Ready' } });
    harness.list.mockResolvedValueOnce([current]);
    await act(async () => { harness.context!({ ...initialContext, namespace: 'synthetic-b', workspacePath: 'root:orgs:synthetic:b' }); });
    expect(screen.getByRole('button', { name: 'View details for new-scope-cluster' })).toBeTruthy();
    await act(async () => { older.resolve([cluster()]); });
    expect(screen.getByRole('button', { name: 'View details for new-scope-cluster' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'View details for synthetic-cluster' })).toBeNull();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('coalesces overlapping same-scope reloads and then presents the queued fresh result', async () => {
    await mountWithDetails();
    const inFlight = deferred<SimpleCluster[]>();
    harness.list.mockReturnValueOnce(inFlight.promise);
    await notify();
    expect(harness.list).toHaveBeenCalledTimes(2);
    harness.list.mockResolvedValueOnce([readyCluster()]);
    await notify();
    await notify();
    expect(harness.list).toHaveBeenCalledTimes(2);
    await act(async () => { inFlight.resolve([cluster()]); });
    expect(harness.list).toHaveBeenCalledTimes(3);
    const table = screen.getByRole('table');
    expect(within(table).getByText('Ready')).toBeTruthy();
    expect(within(table).queryByText('Pending')).toBeNull();
    const dialog = screen.getByRole('dialog', { name: 'synthetic-cluster' });
    expect(within(dialog).getByText('Ready')).toBeTruthy();
    expect(within(dialog).getByRole('button', { name: 'Download kubeconfig' })).toBeTruthy();
  });
});
