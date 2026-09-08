import type { ReactNode } from 'react';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from '../src/App';
import type { CreateValues } from '../src/components/CreateClusterDialog';
import type { ClusterAction } from '../src/components/ClusterList';
import type { GraphqlClientOptions } from '../src/lib/graphql';
import type { RuntimeContext } from '../src/lib/luigi';
import type { CreateSimpleClusterInput } from '../src/lib/simplecluster';
import type { SimpleCluster } from '../src/types';

const harness = vi.hoisted(() => ({
  context: null as ((context: RuntimeContext) => void) | null,
  list: vi.fn<(opts: GraphqlClientOptions, namespace: string) => Promise<SimpleCluster[]>>(),
  create: vi.fn<(opts: GraphqlClientOptions, input: CreateSimpleClusterInput) => Promise<void>>(),
  delete: vi.fn<(opts: GraphqlClientOptions, namespace: string, name: string) => Promise<void>>(),
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
  subscribeSimpleClusters: () => vi.fn(),
  createSimpleCluster: harness.create,
  deleteSimpleCluster: harness.delete
}));
vi.mock('../src/lib/secret', () => ({ fetchKubeconfig: harness.fetchKubeconfig }));
vi.mock('../src/lib/config', () => {
  const config = {
    providers: ['synthetic'], providerSources: [], cpu: ['1'], memory: ['1Gi'],
    controlPlane: { min: 1, max: 1, default: 1 }, workers: { min: 1, max: 1, default: 1 }
  };
  return { fallbackClusterConfig: () => config, loadClusterConfig: vi.fn().mockResolvedValue(config) };
});
vi.mock('../src/lib/addons', () => ({
  loadAddonCatalog: vi.fn().mockResolvedValue({ available: true, entries: [] }),
  entriesForKind: vi.fn().mockReturnValue([]),
  resolveEntitlements: vi.fn().mockResolvedValue(new Map())
}));

// Controlled UI boundaries retain real App callbacks and pending-button behavior.
// Only form entry and unrelated detail/credential presentation are substituted.
vi.mock('../src/components/ClusterList', () => ({
  ClusterList: ({ clusters, onCreate, onAction }: {
    clusters: SimpleCluster[]; onCreate: () => void;
    onAction: (action: ClusterAction, cluster: SimpleCluster) => void;
  }) => <div>
    <button onClick={onCreate}>New SimpleCluster</button>
    {clusters.map((cluster) => <div key={cluster.metadata.uid}>
      <span>{cluster.metadata.name}</span>
      <button onClick={() => onAction('delete', cluster)}>Delete {cluster.metadata.name}</button>
    </div>)}
  </div>
}));
vi.mock('../src/components/CreateClusterDialog', () => ({
  CreateClusterDialog: ({ open, busy, onSubmit }: {
    open: boolean; busy: boolean; onSubmit: (values: CreateValues) => void;
  }) => open ? <div role="dialog" aria-label="Synthetic create">
    <button disabled={busy} onClick={() => onSubmit({
      name: 'synthetic-created', provider: 'synthetic', labels: {},
      workerNodes: 1, workerAutoscaling: false, workerMin: 1, workerMax: 1,
      workerCpu: '1', workerMemory: '1Gi', controlPlaneNodes: 0,
      controlPlaneCpu: '1', controlPlaneMemory: '1Gi'
    })}>Submit synthetic create</button>
  </div> : null
}));
vi.mock('../src/components/DeleteConfirmDialog', () => ({
  DeleteConfirmDialog: ({ open, busy, clusterName, onConfirm }: {
    open: boolean; busy: boolean; clusterName: string | null; onConfirm: () => void;
  }) => open ? <div role="dialog" aria-label="Synthetic delete">
    <button disabled={busy || !clusterName} onClick={onConfirm}>Confirm synthetic delete</button>
  </div> : null
}));
vi.mock('../src/components/ClusterDetail', () => ({ ClusterDetail: () => null }));
vi.mock('../src/components/KubeconfigDialog', () => ({ KubeconfigDialog: () => null }));
vi.mock('@ui5/webcomponents-fiori/dist/illustrations/UnableToLoad.js', () => ({}));
vi.mock('@ui5/webcomponents-react', () => ({
  FlexBox: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  FlexBoxDirection: { Column: 'column' }, FlexBoxAlignItems: { Center: 'center' },
  FlexBoxJustifyContent: { Center: 'center' },
  BusyIndicator: () => <span>Loading</span>,
  IllustratedMessage: () => <span>Empty</span>,
  MessageStrip: ({ children }: { children: ReactNode }) => <div role="alert">{children}</div>,
  ShellBar: () => <header>Simple Cluster</header>, Toast: () => null
}));

const scopeA: RuntimeContext = {
  source: 'luigi', graphqlUrl: 'https://gateway.example.invalid/root/graphql',
  token: 'synthetic-auth-a', workspacePath: 'root:orgs:synthetic:a', namespace: 'synthetic-a'
};
const scopeB: RuntimeContext = {
  ...scopeA, token: 'synthetic-auth-b', workspacePath: 'root:orgs:synthetic:b', namespace: 'synthetic-b'
};
const cluster = (scope: 'a' | 'b'): SimpleCluster => ({
  metadata: { name: `synthetic-cluster-${scope}`, namespace: `synthetic-${scope}`, uid: `synthetic-uid-${scope}` },
  spec: {}, status: { phase: 'Ready' }
});
const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
};

beforeEach(() => {
  harness.context = null;
  harness.list.mockReset().mockResolvedValue([]);
  harness.create.mockReset();
  harness.delete.mockReset();
  harness.fetchKubeconfig.mockReset();
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Unexpected network request in synthetic lifecycle test')));
});
afterEach(() => {
  cleanup();
  expect(fetch).not.toHaveBeenCalled();
  expect(harness.fetchKubeconfig).not.toHaveBeenCalled();
  vi.unstubAllGlobals();
});

const mountScopeA = async () => {
  harness.list.mockResolvedValueOnce([cluster('a')]);
  const result = render(<App />);
  await act(async () => { harness.context!(scopeA); });
  expect(screen.getByText('synthetic-cluster-a')).toBeTruthy();
  return result;
};
const startMutation = async (kind: 'create' | 'delete') => {
  const completion = deferred<void>();
  harness[kind].mockReturnValueOnce(completion.promise);
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: kind === 'create' ? 'New SimpleCluster' : 'Delete synthetic-cluster-a' }));
  });
  fireEvent.click(screen.getByRole('button', { name: kind === 'create' ? 'Submit synthetic create' : 'Confirm synthetic delete' }));
  expect(harness[kind]).toHaveBeenCalledTimes(1);
  expect(harness[kind].mock.calls[0][0]).toEqual(expect.objectContaining({ workspacePath: scopeA.workspacePath }));
  return completion;
};

describe.each(['create', 'delete'] as const)('%s completion lifetime', (kind) => {
  it('still refreshes the list when the mutation completes in its active scope', async () => {
    await mountScopeA();
    const completion = await startMutation(kind);
    const created = cluster('a');
    created.metadata = { ...created.metadata, name: 'synthetic-created', uid: 'synthetic-created-uid' };
    harness.list.mockResolvedValueOnce(kind === 'create' ? [created] : []);
    await act(async () => { completion.resolve(); });
    expect(harness.list.mock.calls.map(([, namespace]) => namespace)).toEqual(['synthetic-a', 'synthetic-a']);
    if (kind === 'create') expect(screen.getByText('synthetic-created')).toBeTruthy();
    expect(screen.queryByText('synthetic-cluster-a')).toBeNull();
  });

  it('does not start an old-scope reload or discard the pending new-scope list after navigation', async () => {
    await mountScopeA();
    const completion = await startMutation(kind);
    const nextList = deferred<SimpleCluster[]>();
    harness.list.mockReturnValueOnce(nextList.promise);
    await act(async () => { harness.context!(scopeB); });
    expect(harness.list.mock.calls.map(([, namespace]) => namespace)).toEqual(['synthetic-a', 'synthetic-b']);
    await act(async () => { completion.resolve(); });
    const requestedNamespaces = harness.list.mock.calls.map(([, namespace]) => namespace);
    await act(async () => { nextList.resolve([cluster('b')]); });
    expect.soft(requestedNamespaces).toEqual(['synthetic-a', 'synthetic-b']);
    expect(screen.queryByText('synthetic-cluster-b')).not.toBeNull();
    expect(screen.queryByText('synthetic-cluster-a')).toBeNull();
  });

  it('does not start a list request when the mutation completes after unmount', async () => {
    const { unmount } = await mountScopeA();
    const completion = await startMutation(kind);
    expect(harness.list).toHaveBeenCalledTimes(1);
    unmount();
    await act(async () => { completion.resolve(); });
    expect(harness.list).toHaveBeenCalledTimes(1);
  });
});
