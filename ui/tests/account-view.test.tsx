import { StrictMode } from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { extractRuntimeContext, type RuntimeContext } from '../src/lib/luigi';
import type { SimpleCluster } from '../src/types';

const api = vi.hoisted(() => ({
  initialContext: undefined as RuntimeContext | undefined,
  onContext: undefined as undefined | ((context: RuntimeContext) => void),
  list: vi.fn(), create: vi.fn(), delete: vi.fn(), secret: vi.fn(), navigate: vi.fn(), stop: vi.fn()
}));
const sdk = vi.hoisted(() => ({ getCurrentRoute: vi.fn(), navigate: vi.fn() }));
vi.mock('@luigi-project/client', () => ({ linkManager: () => sdk }));
vi.mock('../src/lib/luigi', async (original) => ({
  ...await original<typeof import('../src/lib/luigi')>(),
  subscribeToLuigi: (listener: typeof api.onContext) => { api.onContext = listener; if (api.initialContext) listener?.(api.initialContext); return () => {}; },
  navigateToCluster: api.navigate
}));
vi.mock('../src/lib/simplecluster', () => ({
  listSimpleClusters: api.list, createSimpleCluster: api.create, deleteSimpleCluster: api.delete,
  subscribeSimpleClusters: () => api.stop
}));
vi.mock('../src/lib/secret', () => ({ fetchKubeconfig: api.secret }));
vi.mock('../src/lib/config', () => ({
  fallbackClusterConfig: { providers: [], providerSources: [] },
  loadClusterConfig: async () => ({ providers: [], providerSources: [] })
}));
vi.mock('@ui5/webcomponents-react', () => ({
  FlexBox: ({ children }: any) => <div>{children}</div>,
  MessageStrip: ({ children }: any) => <div>{children}</div>,
  ShellBar: () => null, Toast: () => null, BusyIndicator: () => <span>Loading</span>,
  IllustratedMessage: ({ titleText }: any) => <span>{titleText}</span>,
  FlexBoxDirection: {}, FlexBoxAlignItems: {}, FlexBoxJustifyContent: {}
}));
vi.mock('../src/components/ClusterList', () => ({ ClusterList: (props: any) => <div>
  <button disabled={!props.canCreate} onClick={props.onCreate}>Create</button>
  {props.clusters.map((cluster: SimpleCluster) => <div key={cluster.metadata.namespace + '/' + cluster.metadata.name}>
    <span>{cluster.metadata.namespace}/{cluster.metadata.name}</span>
    <button onClick={() => props.onAction('view', cluster)}>View {cluster.metadata.namespace}</button>
    <button onClick={() => props.onAction('delete', cluster)}>Delete {cluster.metadata.namespace}</button>
    <button onClick={() => props.onAction('download-kubeconfig', cluster)}>Secret {cluster.metadata.namespace}</button>
  </div>)}
</div> }));
vi.mock('../src/components/DeleteConfirmDialog', () => ({ DeleteConfirmDialog: (props: any) =>
  props.open ? <button onClick={props.onConfirm}>Confirm delete</button> : null }));
vi.mock('../src/components/ClusterDetail', () => ({ ClusterDetail: (props: any) =>
  props.open ? <span>Detail {props.cluster.metadata.namespace}/{props.cluster.metadata.name}</span> : null }));
vi.mock('../src/components/CreateClusterDialog', () => ({ CreateClusterDialog: () => null }));
vi.mock('../src/components/KubeconfigDialog', () => ({ KubeconfigDialog: (props: any) =>
  props.open ? <span>{props.kubeconfig}</span> : null }));
import App from '../src/App';

const context = (account: string, namespace: string | null = null, clusterName: string | null = null): RuntimeContext => ({
  source: 'luigi', graphqlUrl: 'https://portal.test/gateway/api/clusters/root:orgs:showroom/graphql',
  token: null, workspacePath: `root:orgs:showroom:${account}`, namespace, clusterName
});
const cluster = (namespace: string, name = 'same-name'): SimpleCluster => ({
  metadata: { namespace, name }, status: { kubeconfigSecretRef: { name: 'cluster-kubeconfig' } }
});
const changeContext = (next: RuntimeContext) => act(() => api.onContext?.(next));

beforeEach(() => { vi.clearAllMocks(); api.initialContext = undefined; api.navigate.mockResolvedValue(undefined); api.list.mockResolvedValue([]); api.secret.mockResolvedValue('test-kubeconfig'); });
afterEach(cleanup);

it('lists all namespaces and requires one namespace for creation', async () => {
  render(<App />);
  changeContext(context('a'));
  await waitFor(() => expect(api.list).toHaveBeenCalledWith(expect.objectContaining({ workspacePath: 'root:orgs:showroom:a' }), null));
  expect((screen.getByText('Create') as HTMLButtonElement).disabled).toBe(true);
  changeContext(context('a', 'team-a'));
  await waitFor(() => expect(api.list).toHaveBeenLastCalledWith(expect.anything(), 'team-a'));
  expect((screen.getByText('Create') as HTMLButtonElement).disabled).toBe(false);
});

it('uses the row namespace for delete and kubeconfig from the all-namespace list', async () => {
  api.list.mockResolvedValue([cluster('team-a'), cluster('team-b')]);
  render(<App />);
  changeContext(context('a'));
  await screen.findByText('team-b/same-name');
  fireEvent.click(screen.getByText('Delete team-b'));
  fireEvent.click(screen.getByText('Confirm delete'));
  await waitFor(() => expect(api.delete).toHaveBeenCalledWith(expect.anything(), 'team-b', 'same-name'));
  fireEvent.click(screen.getByText('Secret team-a'));
  await waitFor(() => expect(api.secret).toHaveBeenCalledWith(expect.anything(), 'team-a', 'cluster-kubeconfig'));
});

it('opens equal names by their namespace from a detail deep link', async () => {
  api.list.mockResolvedValue([cluster('team-a'), cluster('team-b')]);
  render(<App />);
  changeContext(context('a', 'team-b', 'same-name'));
  await screen.findByText('Detail team-b/same-name');
  expect(screen.queryByText('Detail team-a/same-name')).toBeNull();
});

it('aborts the old account and rejects its late list response after an account switch', async () => {
  let finishOld!: (rows: SimpleCluster[]) => void;
  api.list.mockImplementationOnce(() => new Promise((resolve) => { finishOld = resolve; }));
  render(<App />);
  changeContext(context('a'));
  await waitFor(() => expect(api.list).toHaveBeenCalledTimes(1));
  const oldClient = api.list.mock.calls[0][0];
  api.list.mockResolvedValue([cluster('team-b', 'new-account')]);
  changeContext(context('b'));
  await screen.findByText('team-b/new-account');
  expect(oldClient.signal.aborted).toBe(true);
  expect(api.stop).toHaveBeenCalled();
  await act(async () => finishOld([cluster('team-a', 'old-account')]));
  expect(screen.queryByText('team-a/old-account')).toBeNull();
  expect(api.list.mock.calls[1][0].workspacePath).toBe('root:orgs:showroom:b');
});

it('closes old account dialogs and ignores a late kubeconfig response', async () => {
  let finishSecret!: (text: string) => void;
  api.secret.mockImplementationOnce(() => new Promise((resolve) => { finishSecret = resolve; }));
  api.list.mockResolvedValue([cluster('team-a')]);
  render(<App />);
  changeContext(context('a'));
  await screen.findByText('team-a/same-name');
  fireEvent.click(screen.getByText('Secret team-a'));
  fireEvent.click(screen.getByText('Delete team-a'));
  api.list.mockResolvedValue([]);
  changeContext(context('b'));
  await act(async () => finishSecret('old-account-kubeconfig'));
  expect(screen.queryByText('old-account-kubeconfig')).toBeNull();
  expect(screen.queryByText('Confirm delete')).toBeNull();
});

it('makes no resource requests for an unresolved placeholder and a display account name', async () => {
  render(<App />);
  const next = extractRuntimeContext({ accountId: 'Display name', portalContext: {
    crdGatewayApiUrl: 'https://portal.test/gateway/api/clusters/:accountId/graphql'
  } }, 'luigi');
  act(() => api.onContext?.(next));
  await act(async () => {});
  expect(next.workspacePath).toBeNull();
  expect(api.list).not.toHaveBeenCalled();
});

it('uses the visible Details control with the row namespace', async () => {
  api.list.mockResolvedValue([cluster('team-a'), cluster('team-b')]);
  const actual = await vi.importActual<typeof import('../src/lib/luigi')>('../src/lib/luigi');
  api.navigate.mockImplementation(actual.navigateToCluster);
  sdk.getCurrentRoute.mockResolvedValue('/home/accounts/a/showroom_apeirora_eu_simpleclusters');
  api.initialContext = context('a');
  render(<StrictMode><App /></StrictMode>);
  await screen.findByText('team-b/same-name');
  fireEvent.click(screen.getByText('View team-b'));
  await waitFor(() => expect(api.navigate).toHaveBeenCalledWith('same-name', 'team-b', expect.any(AbortSignal)));
  expect(sdk.navigate).toHaveBeenCalledWith('/home/accounts/a/showroom_apeirora_eu_simpleclusters/same-name?namespace=team-b');
  expect(api.list.mock.calls.at(-1)![0].signal.aborted).toBe(false);
});
