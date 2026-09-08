import type { ReactNode } from 'react';
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import App from '../src/App';
import type { GraphqlClientOptions } from '../src/lib/graphql';
import type { RuntimeContext } from '../src/lib/luigi';
import type { SimpleCluster } from '../src/types';

// Regression: an account with no installed providers. Discovery is real code
// here — every provider query comes back HTTP 200 with a GraphQL validation
// error ("Cannot query field") because no provider API is bound. The clusters
// view must land on an explicit empty state, not an endless busy indicator.
const harness = vi.hoisted(() => ({
  context: null as ((context: RuntimeContext) => void) | null,
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
  subscribeSimpleClusters: () => vi.fn(),
  createSimpleCluster: vi.fn(), deleteSimpleCluster: vi.fn()
}));
vi.mock('../src/lib/config', async () => {
  // The five standard sources deployed on the environments; static names must
  // not resurface once dynamic discovery is configured.
  const { standardSources } = await import('./providerFixture');
  const config = {
    providers: ['synthetic-static'], providerSources: standardSources.slice(0, 5),
    cpu: ['1'], memory: ['2Gi'],
    controlPlane: { min: 1, max: 3, default: 1 }, workers: { min: 1, max: 10, default: 1 }
  };
  return { fallbackClusterConfig: () => config, loadClusterConfig: () => Promise.resolve(config) };
});
vi.mock('../src/lib/secret', () => ({ fetchKubeconfig: vi.fn() }));
vi.mock('../src/components/CreateClusterDialog', () => ({ CreateClusterDialog: () => null }));
vi.mock('../src/components/DeleteConfirmDialog', () => ({ DeleteConfirmDialog: () => null }));
vi.mock('../src/components/ClusterDetail', () => ({ ClusterDetail: () => null }));
vi.mock('../src/components/KubeconfigDialog', () => ({ KubeconfigDialog: () => null }));
vi.mock('@ui5/webcomponents-fiori/dist/illustrations/UnableToLoad.js', () => ({}));
vi.mock('@ui5/webcomponents-fiori/dist/illustrations/NoData.js', () => ({}));
// The real ClusterList renders through these doubles so its busy/empty states
// stay observable.
vi.mock('@ui5/webcomponents-react', () => ({
  FlexBox: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  FlexBoxDirection: { Column: 'column' }, FlexBoxAlignItems: { Center: 'center' },
  FlexBoxJustifyContent: { Center: 'center', SpaceBetween: 'space-between' },
  BusyIndicator: ({ active }: { active?: boolean }) => (active ? <span data-testid="busy">Loading</span> : null),
  IllustratedMessage: ({ titleText, subtitleText, children }: {
    titleText?: string; subtitleText?: string; children?: ReactNode;
  }) => <div>{titleText && <h2>{titleText}</h2>}{subtitleText && <p>{subtitleText}</p>}{children}</div>,
  MessageStrip: ({ children }: { children: ReactNode }) => <div role="alert">{children}</div>,
  ShellBar: () => <header>Simple Cluster</header>, Toast: () => null,
  Title: ({ children }: { children: ReactNode }) => <h3>{children}</h3>,
  Button: ({ children, disabled, onClick }: { children: ReactNode; disabled?: boolean; onClick?: () => void }) =>
    <button disabled={disabled} onClick={onClick}>{children}</button>,
  ObjectStatus: ({ children }: { children: ReactNode }) => <span>{children}</span>,
  AnalyticalTable: () => <div data-testid="table" />
}));

const context = (namespace: string | null): RuntimeContext => ({
  source: 'luigi', graphqlUrl: 'https://gateway.example.invalid/root/graphql',
  token: 'synthetic-auth', workspacePath: 'root:orgs:synthetic:a', namespace
});
const cluster: SimpleCluster = {
  metadata: { name: 'synthetic-leftover', namespace: 'synthetic-a', uid: 'synthetic-uid' },
  spec: {}, status: { phase: 'Ready' }
};

let warn: ReturnType<typeof vi.spyOn>;

const mount = async (ctx: RuntimeContext) => {
  render(<App />);
  await act(async () => { harness.context!(ctx); });
};

beforeEach(() => {
  harness.context = null;
  harness.list.mockReset().mockResolvedValue([]);
  warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
  // The gateway answers every discovery query, but no provider type is in the
  // schema — a definitive "nothing is installed", not a transport failure.
  vi.stubGlobal('fetch', vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify({
    errors: [{ message: 'Cannot query field "synthetic" on type "Query"' }]
  }), { status: 200 }))));
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it('replaces the endless spinner with an empty state when the account offers no providers', async () => {
  // Live shape: the portal context lets discovery run but the cluster list
  // never starts, so nothing else ever clears the loading state.
  await mount(context(null));
  expect(warn.mock.calls.filter(([first]) => String(first).includes('provider discovery skipped'))).toHaveLength(5);
  expect(harness.list).not.toHaveBeenCalled();
  expect(screen.queryByTestId('busy')).toBeNull();
  expect(screen.getByText('No cluster providers are available in this account.')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'New SimpleCluster' })).toBeNull();
});

it('does not invite creation when the list is empty and discovery found no providers', async () => {
  await mount(context('synthetic-a'));
  expect(harness.list).toHaveBeenCalled();
  expect(screen.getByText('No cluster providers are available in this account.')).toBeTruthy();
  expect(screen.queryByText('No SimpleClusters yet')).toBeNull();
  expect(screen.queryByRole('button', { name: 'New SimpleCluster' })).toBeNull();
});

it('keeps listing existing clusters but disables creation without a provider', async () => {
  harness.list.mockResolvedValue([cluster]);
  await mount(context('synthetic-a'));
  expect(screen.getByTestId('table')).toBeTruthy();
  const create = screen.getByRole('button', { name: 'New SimpleCluster' }) as HTMLButtonElement;
  expect(create.disabled).toBe(true);
});
