import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  BusyIndicator,
  FlexBox,
  FlexBoxAlignItems,
  FlexBoxDirection,
  FlexBoxJustifyContent,
  IllustratedMessage,
  MessageStrip,
  ShellBar,
  Toast
} from '@ui5/webcomponents-react';
import '@ui5/webcomponents-fiori/dist/illustrations/UnableToLoad.js';

import { ClusterList, type ClusterAction } from './components/ClusterList';
import { CreateClusterDialog, type CreateValues } from './components/CreateClusterDialog';
import { DeleteConfirmDialog } from './components/DeleteConfirmDialog';
import { KubeconfigDialog } from './components/KubeconfigDialog';
import { ClusterDetail } from './components/ClusterDetail';
import {
  emptyRuntimeContext,
  subscribeToLuigi,
  type RuntimeContext
} from './lib/luigi';
import type { GraphqlClientOptions } from './lib/graphql';
import {
  createSimpleCluster,
  deleteSimpleCluster,
  listSimpleClusters,
  subscribeSimpleClusters
} from './lib/simplecluster';
import { fetchKubeconfig } from './lib/secret';
import { fallbackClusterConfig, loadClusterConfig } from './lib/config';
import { fetchAvailableProviders, offeredProviders } from './lib/providers';
import { entriesForKind, loadAddonCatalog, resolveEntitlements } from './lib/addons';
import type { AddonCatalogEntry, AddonEntitlement, ClusterConfig, SimpleCluster } from './types';

const POLL_INTERVAL_MS = 5000;
const LIST_TIMEOUT_MS = 30000;

const sameCluster = (selected: SimpleCluster, current: SimpleCluster) =>
  selected.metadata.name === current.metadata.name &&
  (selected.metadata.uid ? selected.metadata.uid === current.metadata.uid : selected === current);

const App = () => {
  const [ctx, setCtx] = useState<RuntimeContext>(emptyRuntimeContext);
  const [config, setConfig] = useState<ClusterConfig>(fallbackClusterConfig);
  const [inventory, setInventory] = useState<{ scope: object; items: SimpleCluster[] } | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [createOpen, setCreateOpen] = useState(false);
  const [createBusy, setCreateBusy] = useState(false);

  // Live provider list discovered from installed provider CRs. null means
  // availability is unconfirmed and dynamic provider submission stays disabled.
  const [discoveredProviders, setDiscoveredProviders] = useState<string[] | null>(null);
  const [discoveredScope, setDiscoveredScope] = useState<object | null>(null);
  const [providersLoading, setProvidersLoading] = useState(false);

  // Extensions offerable at creation time, and whether this workspace has each of them.
  const [addons, setAddons] = useState<AddonCatalogEntry[]>([]);
  const [entitlements, setEntitlements] = useState<Map<string, AddonEntitlement>>(new Map());
  const [addonsLoading, setAddonsLoading] = useState(false);
  // Told apart from "no extensions installed": one is a deployment fault worth saying
  // out loud, the other is the normal case and says nothing.
  const [catalogUnavailable, setCatalogUnavailable] = useState(false);

  const [deleteTarget, setDeleteTarget] = useState<SimpleCluster | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);

  const [detailSelection, setDetailSelection] = useState<{ scope: object; cluster: SimpleCluster } | null>(null);

  const [kubeconfigOpen, setKubeconfigOpen] = useState(false);
  const [kubeconfigClusterName, setKubeconfigClusterName] = useState<string | null>(null);
  const [kubeconfigContents, setKubeconfigContents] = useState<string | null>(null);

  const [toastText, setToastText] = useState('');
  const [toastOpen, setToastOpen] = useState(false);
  const flashToast = useCallback((msg: string) => {
    setToastText(msg);
    setToastOpen(true);
  }, []);

  const listRequest = useRef<{
    scope: object; refreshQueued: boolean; controller: AbortController; timeout: number;
  } | null>(null);
  const activeClusterScope = useRef<object | null>(null);
  const providerDiscoveryGeneration = useRef(0);

  useEffect(() => subscribeToLuigi(setCtx), []);

  useEffect(() => {
    void loadClusterConfig().then(setConfig);
  }, []);

  const client: GraphqlClientOptions = useMemo(
    () => ({
      graphqlUrl: ctx.graphqlUrl ?? '',
      token: ctx.token,
      workspacePath: ctx.workspacePath
    }),
    [ctx.graphqlUrl, ctx.token, ctx.workspacePath]
  );

  // An inventory/selection belongs to this exact account, namespace and auth
  // context. Hide it immediately when context changes, before effects run.
  const clusterScope = useMemo(() => ({ client, namespace: ctx.namespace }), [client, ctx.namespace]);
  const clusters = inventory?.scope === clusterScope ? inventory.items : [];
  const detailTarget = detailSelection?.scope === clusterScope
    ? clusters.find((cluster) => sameCluster(detailSelection.cluster, cluster)) ?? null
    : null;

  const usingDynamicProviders = config.providerSources.length > 0;
  const discoveryScope = useMemo(() => ({ client, namespace: ctx.namespace, sources: config.providerSources }),
    [client, ctx.namespace, config.providerSources]);
  const currentDiscoveredProviders = discoveredScope === discoveryScope ? discoveredProviders : null;

  const discoverProviders = useCallback(async () => {
    const generation = ++providerDiscoveryGeneration.current;
    setDiscoveredProviders(null);
    setProvidersLoading(false);
    if (!usingDynamicProviders) {
      return;
    }
    // Provider CRs are cluster-scoped in KCP, so discovery needs the gateway
    // but not a namespace.
    if (!ctx.graphqlUrl) return;
    setProvidersLoading(true);
    try {
      const result = await fetchAvailableProviders(client, ctx.namespace ?? '', config.providerSources);
      if (providerDiscoveryGeneration.current === generation) {
        setDiscoveredProviders(result.ran ? result.providers : null);
        setDiscoveredScope(discoveryScope);
      }
    } catch (err) {
      console.warn('[simple-cluster-ui] provider discovery failed', err);
    } finally {
      if (providerDiscoveryGeneration.current === generation) setProvidersLoading(false);
    }
  }, [client, ctx.graphqlUrl, ctx.namespace, config.providerSources, usingDynamicProviders, discoveryScope]);
  // note: ctx.namespace stays in deps so discovery reruns if it arrives late.

  useEffect(() => {
    void discoverProviders();
  }, [discoverProviders]);

  // Dynamic discovery must confirm availability. A transport failure must not
  // turn a configured provider name into permission to submit to that provider.
  const providers = useMemo(
    () => offeredProviders(config, currentDiscoveredProviders),
    [config, currentDiscoveredProviders]
  );

  // Discovery answered and offered nothing: this account has no providers, so
  // the clusters view says so instead of waiting for a create flow that cannot
  // succeed. An unconfirmed result (null) keeps the normal loading behaviour.
  const noProviders = usingDynamicProviders && currentDiscoveredProviders?.length === 0;

  // The catalogue is global and the entitlement per account, so both are resolved
  // together: without an entitlement an entry can only be rendered disabled anyway.
  const discoverAddons = useCallback(async () => {
    setAddonsLoading(true);
    try {
      const catalog = await loadAddonCatalog();
      setCatalogUnavailable(!catalog.available);
      const offered = entriesForKind(catalog.entries, 'SimpleCluster');
      setAddons(offered);
      setEntitlements(await resolveEntitlements(client, ctx.namespace ?? '', offered));
    } catch (err) {
      console.warn('[simple-cluster-ui] extension discovery failed', err);
      setAddons([]);
      setCatalogUnavailable(true);
    } finally {
      setAddonsLoading(false);
    }
  }, [client, ctx.namespace]);

  const openCreate = useCallback(() => {
    setCreateOpen(true);
    // Refresh so providers and extensions installed since page load appear.
    void discoverProviders();
    void discoverAddons();
  }, [discoverProviders, discoverAddons]);

  const reload = useCallback(async (): Promise<void> => {
    if (activeClusterScope.current !== clusterScope || !ctx.graphqlUrl || !ctx.namespace) return;
    if (listRequest.current?.scope === clusterScope) {
      // Coalesce event bursts without starving a slow read or racing its result.
      listRequest.current.refreshQueued = true;
      return;
    }
    const request = { scope: clusterScope, refreshQueued: false, controller: new AbortController(), timeout: 0 };
    listRequest.current = request;
    request.timeout = window.setTimeout(() => {
      if (listRequest.current !== request) return;
      listRequest.current = null;
      request.controller.abort();
      setLoading(false);
      setError('Cluster list refresh timed out; retrying automatically.');
    }, LIST_TIMEOUT_MS);
    setLoading(true);
    try {
      const items = await listSimpleClusters(client, ctx.namespace, request.controller.signal);
      if (listRequest.current !== request) return;
      setInventory({ scope: clusterScope, items });
      setDetailSelection((selection) => selection?.scope === clusterScope &&
        items.some((item) => sameCluster(selection.cluster, item)) ? selection : null);
      setError(null);
    } catch (err) {
      if (listRequest.current === request) setError((err as Error).message);
    } finally {
      window.clearTimeout(request.timeout);
      if (listRequest.current === request) {
        listRequest.current = null;
        setLoading(false);
        if (request.refreshQueued) void reload();
      }
    }
  }, [client, ctx.graphqlUrl, ctx.namespace, clusterScope]);

  useEffect(() => {
    activeClusterScope.current = clusterScope;
    void reload();
    return () => {
      if (activeClusterScope.current === clusterScope) activeClusterScope.current = null;
      if (listRequest.current?.scope === clusterScope) {
        const request = listRequest.current;
        listRequest.current = null;
        window.clearTimeout(request.timeout);
        request.controller.abort();
      }
    };
  }, [reload, clusterScope]);

  // SSE events refresh immediately. A five-second safety poll also covers
  // unsupported, failed or silently stalled streams; it never polls Secrets.
  useEffect(() => {
    if (!ctx.graphqlUrl || !ctx.namespace) return;

    let disposed = false;
    let reloadQueued = false;
    const queueReload = () => {
      if (disposed || reloadQueued) return;
      reloadQueued = true;
      queueMicrotask(() => {
        reloadQueued = false;
        if (!disposed) void reload();
      });
    };

    const unsubscribe = subscribeSimpleClusters(client, ctx.namespace, queueReload);

    const pollHandle = window.setInterval(() => {
      if (!disposed && listRequest.current?.scope !== clusterScope) void reload();
    }, POLL_INTERVAL_MS);

    return () => {
      disposed = true;
      unsubscribe();
      window.clearInterval(pollHandle);
    };
  }, [client, ctx.graphqlUrl, ctx.namespace, reload, clusterScope]);

  const handleCreate = async (values: CreateValues) => {
    if (!ctx.namespace) return;
    setCreateBusy(true);
    try {
      await createSimpleCluster(client, {
        namespace: ctx.namespace,
        name: values.name,
        provider: values.provider,
        labels: values.labels,
        workerNodes: values.workerNodes,
        workerAutoscaling: values.workerAutoscaling,
        workerMin: values.workerMin,
        workerMax: values.workerMax,
        workerCpu: values.workerCpu,
        workerMemory: values.workerMemory,
        controlPlaneNodes: values.controlPlaneNodes,
        controlPlaneCpu: values.controlPlaneCpu,
        controlPlaneMemory: values.controlPlaneMemory
      });
      setCreateOpen(false);
      await reload();
    } catch (err) {
      setError(`Create failed: ${(err as Error).message}`);
    } finally {
      setCreateBusy(false);
    }
  };

  const handleDelete = async () => {
    if (!ctx.namespace || !deleteTarget) return;
    setDeleteBusy(true);
    try {
      await deleteSimpleCluster(client, ctx.namespace, deleteTarget.metadata.name);
      setDeleteTarget(null);
      await reload();
    } catch (err) {
      setError(`Delete failed: ${(err as Error).message}`);
    } finally {
      setDeleteBusy(false);
    }
  };

  const handleDownloadKubeconfig = async (cluster: SimpleCluster) => {
    if (!ctx.namespace) return;
    const secretName = cluster.status?.kubeconfigSecretRef?.name;
    if (!secretName) {
      setError(`${cluster.metadata.name} has no kubeconfig Secret yet.`);
      return;
    }
    flashToast(`Fetching kubeconfig for ${cluster.metadata.name}…`);
    setKubeconfigClusterName(cluster.metadata.name);
    setKubeconfigContents(null);
    setKubeconfigOpen(true);
    try {
      const contents = await fetchKubeconfig(client, ctx.namespace, secretName);
      setKubeconfigContents(contents);
    } catch (err) {
      setKubeconfigOpen(false);
      setError(`Fetch failed: ${(err as Error).message}`);
    }
  };

  const handleAction = (action: ClusterAction, cluster: SimpleCluster) => {
    switch (action) {
      case 'view':
        setDetailSelection({ scope: clusterScope, cluster });
        break;
      case 'delete':
        flashToast(`Confirm delete for ${cluster.metadata.name}…`);
        setDeleteTarget(cluster);
        break;
      case 'download-kubeconfig':
        void handleDownloadKubeconfig(cluster);
        break;
    }
  };

  if (ctx.source === 'pending') {
    return (
      <FlexBox
        direction={FlexBoxDirection.Column}
        alignItems={FlexBoxAlignItems.Center}
        justifyContent={FlexBoxJustifyContent.Center}
        style={{ height: '100vh', gap: '0.75rem' }}
      >
        <BusyIndicator active size="L" />
        <span>Waiting for portal context…</span>
      </FlexBox>
    );
  }

  if (!ctx.graphqlUrl) {
    return (
      <IllustratedMessage
        name="UnableToLoad"
        titleText="No GraphQL gateway"
        subtitleText="The portal did not provide crdGatewayApiUrl in the Luigi context."
      />
    );
  }

  return (
    <FlexBox direction={FlexBoxDirection.Column} style={{ height: '100vh' }}>
      <ShellBar primaryTitle="Simple Cluster" secondaryTitle="Managed Kubernetes clusters" />
      <div
        style={{
          flex: 1,
          overflow: 'auto',
          padding: '1.25rem 1.5rem',
          background: 'var(--sapBackgroundColor)'
        }}
      >
        <FlexBox
          direction={FlexBoxDirection.Column}
          style={{ gap: '1rem', maxWidth: 1200, margin: '0 auto' }}
        >
          {error && (
            <MessageStrip design="Negative" onClose={() => setError(null)}>
              {error}
            </MessageStrip>
          )}

          <ClusterList
            clusters={clusters}
            loading={loading}
            noProviders={noProviders}
            onCreate={openCreate}
            onAction={handleAction}
          />
        </FlexBox>
      </div>

      <CreateClusterDialog
        open={createOpen}
        busy={createBusy}
        config={config}
        providers={providers}
        providersLoading={providersLoading}
        providersUnavailable={usingDynamicProviders && currentDiscoveredProviders === null && !providersLoading}
        addons={addons}
        entitlements={entitlements}
        addonsLoading={addonsLoading}
        catalogUnavailable={catalogUnavailable}
        onClose={() => setCreateOpen(false)}
        onSubmit={handleCreate}
      />

      <DeleteConfirmDialog
        open={Boolean(deleteTarget)}
        clusterName={deleteTarget?.metadata.name ?? null}
        busy={deleteBusy}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => void handleDelete()}
      />

      <ClusterDetail
        open={Boolean(detailTarget)}
        cluster={detailTarget}
        onClose={() => setDetailSelection(null)}
        onDownloadKubeconfig={(c) => void handleDownloadKubeconfig(c)}
      />

      <KubeconfigDialog
        open={kubeconfigOpen}
        clusterName={kubeconfigClusterName}
        kubeconfig={kubeconfigContents}
        onClose={() => setKubeconfigOpen(false)}
      />

      <Toast open={toastOpen} duration={3000} onClose={() => setToastOpen(false)}>
        {toastText}
      </Toast>
    </FlexBox>
  );
};

export default App;
