import type { ReactNode } from 'react';
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from '../src/App';
import type { GraphqlClientOptions } from '../src/lib/graphql';
import type { RuntimeContext } from '../src/lib/luigi';
import type { SimpleCluster } from '../src/types';

const harness = vi.hoisted(() => ({
  context: null as ((context: RuntimeContext) => void) | null,
  subscriptions: [] as Array<{ onEvent: () => void; unsubscribe: () => void }>,
  list: vi.fn<(opts: GraphqlClientOptions, namespace: string, signal?: AbortSignal) => Promise<SimpleCluster[]>>()
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
  subscribeSimpleClusters: (_opts: GraphqlClientOptions, _namespace: string, onEvent: () => void) => {
    const unsubscribe = vi.fn();
    harness.subscriptions.push({ onEvent, unsubscribe });
    return unsubscribe;
  },
  createSimpleCluster: vi.fn(), deleteSimpleCluster: vi.fn()
}));
vi.mock('../src/lib/config', () => {
  const config = { providers: [], providerSources: [], cpu: [], memory: [],
    controlPlane: { min: 1, max: 1, default: 1 }, workers: { min: 1, max: 1, default: 1 } };
  return { fallbackClusterConfig: () => config, loadClusterConfig: () => Promise.resolve(config) };
});
vi.mock('../src/lib/secret', () => ({ fetchKubeconfig: vi.fn() }));
// Keep App's real effects/state; presentation and unrelated workflows are doubles.
vi.mock('../src/components/ClusterList', () => ({
  ClusterList: ({ clusters, loading }: { clusters: SimpleCluster[]; loading: boolean }) => <>
    <div data-testid="clusters">{clusters.map((cluster) =>
      `${cluster.metadata.name}:${cluster.status?.phase}`).join(',')}</div>
    <div data-testid="loading">{String(loading)}</div>
  </>
}));
vi.mock('../src/components/ClusterDetail', () => ({ ClusterDetail: () => null }));
vi.mock('../src/components/CreateClusterDialog', () => ({ CreateClusterDialog: () => null }));
vi.mock('../src/components/DeleteConfirmDialog', () => ({ DeleteConfirmDialog: () => null }));
vi.mock('../src/components/KubeconfigDialog', () => ({ KubeconfigDialog: () => null }));
vi.mock('@ui5/webcomponents-fiori/dist/illustrations/UnableToLoad.js', () => ({}));
vi.mock('@ui5/webcomponents-react', () => ({
  FlexBox: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  FlexBoxDirection: { Column: 'column' }, FlexBoxAlignItems: { Center: 'center' },
  FlexBoxJustifyContent: { Center: 'center' },
  MessageStrip: ({ children }: { children: ReactNode }) => <div role="alert">{children}</div>,
  BusyIndicator: () => null, IllustratedMessage: () => null, ShellBar: () => null, Toast: () => null
}));

const initialContext: RuntimeContext = {
  source: 'luigi', graphqlUrl: 'https://gateway.example.invalid/root/graphql', token: null,
  workspacePath: 'root:orgs:synthetic:a', namespace: 'synthetic-a'
};
const otherContext: RuntimeContext = {
  ...initialContext, workspacePath: 'root:orgs:synthetic:b', namespace: 'synthetic-b'
};
const cluster = (phase: string, name = 'synthetic-a'): SimpleCluster => ({
  metadata: { name, uid: `uid-${name}` }, spec: {}, status: { phase }
});
const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
};
const advance = async (milliseconds: number) => {
  await act(async () => { await vi.advanceTimersByTimeAsync(milliseconds); });
};
const mount = async () => {
  const view = render(<App />);
  await act(async () => { harness.context!(initialContext); });
  return view;
};
const notify = async () => {
  await act(async () => { harness.subscriptions[harness.subscriptions.length - 1].onEvent(); });
};

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'setTimeout', 'clearTimeout'] });
  harness.context = null;
  harness.subscriptions = [];
  harness.list.mockReset().mockResolvedValue([]);
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('Network forbidden in App polling regression')));
});
afterEach(() => {
  cleanup();
  expect(fetch).not.toHaveBeenCalled();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('App periodic refresh and subscription lifecycle', () => {
  it('polls every five seconds when a quiet or unavailable subscription emits no events', async () => {
    harness.list.mockResolvedValueOnce([cluster('Pending')]).mockResolvedValueOnce([cluster('Ready')]);
    await mount();
    expect(screen.getByTestId('clusters').textContent).toBe('synthetic-a:Pending');
    await advance(4999);
    expect(harness.list).toHaveBeenCalledTimes(1);
    await advance(1);
    expect(harness.list).toHaveBeenCalledTimes(2);
    expect(screen.getByTestId('clusters').textContent).toBe('synthetic-a:Ready');
  });

  it('coalesces immediate events and keeps polling after a previously active stream goes quiet', async () => {
    harness.list.mockResolvedValueOnce([cluster('Pending')]).mockResolvedValueOnce([cluster('Ready')])
      .mockResolvedValueOnce([cluster('Failed')]);
    await mount();
    await act(async () => {
      const { onEvent } = harness.subscriptions[0];
      onEvent(); onEvent(); onEvent();
    });
    expect(harness.list).toHaveBeenCalledTimes(2);
    expect(screen.getByTestId('clusters').textContent).toBe('synthetic-a:Ready');
    await advance(5000);
    expect(harness.list).toHaveBeenCalledTimes(3);
    expect(screen.getByTestId('clusters').textContent).toBe('synthetic-a:Failed');
  });

  it('accepts a slow request without concurrent timer requests or a queued timer refresh', async () => {
    const slow = deferred<SimpleCluster[]>();
    harness.list.mockReturnValueOnce(slow.promise);
    await mount();
    await advance(15000);
    expect(harness.list).toHaveBeenCalledTimes(1);
    await act(async () => { slow.resolve([cluster('Pending')]); });
    expect(screen.getByTestId('clusters').textContent).toBe('synthetic-a:Pending');
    expect(screen.getByTestId('loading').textContent).toBe('false');
    expect(harness.list).toHaveBeenCalledTimes(1);
    harness.list.mockResolvedValueOnce([cluster('Ready')]);
    await advance(5000);
    expect(harness.list).toHaveBeenCalledTimes(2);
    expect(screen.getByTestId('clusters').textContent).toBe('synthetic-a:Ready');
  });

  it('queues only one event refresh behind a slow poll and presents both completed responses', async () => {
    harness.list.mockResolvedValueOnce([cluster('Pending')]);
    await mount();
    const polling = deferred<SimpleCluster[]>();
    const queued = deferred<SimpleCluster[]>();
    harness.list.mockReturnValueOnce(polling.promise).mockReturnValueOnce(queued.promise);
    await advance(5000);
    expect(harness.list).toHaveBeenCalledTimes(2);
    await notify();
    await notify();
    await notify();
    await advance(10000);
    expect(harness.list).toHaveBeenCalledTimes(2);
    await act(async () => { polling.resolve([cluster('Ready')]); });
    expect(harness.list).toHaveBeenCalledTimes(3);
    expect(screen.getByTestId('clusters').textContent).toBe('synthetic-a:Ready');
    await act(async () => { queued.resolve([cluster('Failed')]); });
    expect(harness.list).toHaveBeenCalledTimes(3);
    expect(screen.getByTestId('clusters').textContent).toBe('synthetic-a:Failed');
  });

  it('starts the new scope independently and ignores old completion while its request is pending', async () => {
    const oldScope = deferred<SimpleCluster[]>();
    const newScope = deferred<SimpleCluster[]>();
    harness.list.mockReturnValueOnce(oldScope.promise).mockReturnValueOnce(newScope.promise);
    await mount();
    await act(async () => { harness.context!(otherContext); });
    expect(harness.list).toHaveBeenCalledTimes(2);
    expect(harness.list.mock.calls[1][1]).toBe('synthetic-b');
    expect(harness.list.mock.calls[0][2]?.aborted).toBe(true);
    expect(harness.list.mock.calls[1][2]?.aborted).toBe(false);
    await act(async () => { oldScope.resolve([cluster('Failed')]); });
    expect(screen.getByTestId('clusters').textContent).toBe('');
    expect(screen.getByTestId('loading').textContent).toBe('true');
    await advance(10000);
    expect(harness.list).toHaveBeenCalledTimes(2);
    await act(async () => { newScope.resolve([cluster('Pending', 'synthetic-b')]); });
    expect(screen.getByTestId('clusters').textContent).toBe('synthetic-b:Pending');
    harness.list.mockResolvedValueOnce([cluster('Ready', 'synthetic-b')]);
    await advance(5000);
    expect(harness.list).toHaveBeenCalledTimes(3);
    expect(harness.list.mock.calls[2][1]).toBe('synthetic-b');
    expect(screen.getByTestId('clusters').textContent).toBe('synthetic-b:Ready');
  });

  it('retries on a later poll after a list request fails', async () => {
    harness.list.mockResolvedValueOnce([cluster('Pending')])
      .mockRejectedValueOnce(new Error('synthetic list unavailable')).mockResolvedValueOnce([cluster('Ready')]);
    await mount();
    await advance(5000);
    expect(screen.getByRole('alert').textContent).toBe('synthetic list unavailable');
    await advance(5000);
    expect(harness.list).toHaveBeenCalledTimes(3);
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.getByTestId('clusters').textContent).toBe('synthetic-a:Ready');
  });

  it.each(['during recovery', 'after recovery'] as const)(
    'aborts a 30-second read, retries, and ignores its late completion %s', async (completion) => {
      // Deliberately ignore cancellation in this mock: App must also reject a late result.
      const timedOut = deferred<SimpleCluster[]>();
      const recovery = deferred<SimpleCluster[]>();
      harness.list.mockReturnValueOnce(timedOut.promise).mockReturnValueOnce(recovery.promise);
      await mount();
      const oldSignal = harness.list.mock.calls[0][2];
      expect(oldSignal?.aborted).toBe(false);
      await advance(29999);
      expect(oldSignal?.aborted).toBe(false);
      expect(harness.list).toHaveBeenCalledTimes(1);
      await advance(1);
      expect(oldSignal?.aborted).toBe(true);
      expect(screen.getByRole('alert').textContent).toContain('timed out');
      await advance(5000);
      expect(harness.list).toHaveBeenCalledTimes(2);
      expect(harness.list.mock.calls[1][2]?.aborted).toBe(false);
      if (completion === 'during recovery') {
        await act(async () => { timedOut.resolve([cluster('Failed')]); });
        expect(screen.getByTestId('clusters').textContent).toBe('');
        expect(screen.getByTestId('loading').textContent).toBe('true');
        await advance(5000);
        expect(harness.list).toHaveBeenCalledTimes(2);
      }
      await act(async () => { recovery.resolve([cluster('Ready')]); });
      expect(screen.getByTestId('clusters').textContent).toBe('synthetic-a:Ready');
      expect(screen.queryByRole('alert')).toBeNull();
      if (completion === 'after recovery') {
        await act(async () => { timedOut.resolve([cluster('Failed')]); });
        expect(screen.getByTestId('clusters').textContent).toBe('synthetic-a:Ready');
      }
    }
  );

  it('aborts its pending read and clears its deadline and periodic retry on unmount', async () => {
    const pending = deferred<SimpleCluster[]>();
    harness.list.mockReturnValueOnce(pending.promise);
    const view = await mount();
    const signal = harness.list.mock.calls[0][2];
    expect(signal?.aborted).toBe(false);
    view.unmount();
    expect(signal?.aborted).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
    await advance(60000);
    await act(async () => { pending.resolve([cluster('Failed')]); });
    expect(harness.list).toHaveBeenCalledTimes(1);
  });

  it.each(['unmount', 'context change'] as const)('disposes timers, delayed events and already queued work on %s', async (action) => {
    const view = await mount();
    const subscription = harness.subscriptions[0];
    const queued: Array<() => void> = [];
    const microtask = vi.spyOn(globalThis, 'queueMicrotask').mockImplementation((callback) => { queued.push(callback); });
    act(() => { subscription.onEvent(); });
    expect(queued).toHaveLength(1);
    microtask.mockRestore();
    if (action === 'unmount') view.unmount();
    else await act(async () => { harness.context!(otherContext); });
    expect(subscription.unsubscribe).toHaveBeenCalledTimes(1);
    const requestsAfterCleanup = harness.list.mock.calls.length;
    await act(async () => {
      queued[0]();
      subscription.onEvent();
    });
    expect(harness.list).toHaveBeenCalledTimes(requestsAfterCleanup);
    await advance(5000);
    expect(harness.list).toHaveBeenCalledTimes(requestsAfterCleanup + (action === 'unmount' ? 0 : 1));
    if (action === 'context change') {
      expect(harness.list.mock.calls[harness.list.mock.calls.length - 1][1]).toBe('synthetic-b');
    }
  });
});
