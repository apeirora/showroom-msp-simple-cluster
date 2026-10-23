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
  navigateToCluster,
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
import { fetchAvailableProviders } from './lib/providers';
import { entriesForKind, loadAddonCatalog, resolveEntitlements } from './lib/addons';
import type { AddonCatalogEntry, AddonEntitlement, ClusterConfig, SimpleCluster } from './types';

const POLL_INTERVAL_MS = 5000;

const App = () => {
  const [ctx, setCtx] = useState<RuntimeContext>(emptyRuntimeContext);
  const [config, setConfig] = useState<ClusterConfig>(fallbackClusterConfig);
  const [clusters, setClusters] = useState<SimpleCluster[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [createOpen, setCreateOpen] = useState(false);
  const [createBusy, setCreateBusy] = useState(false);

  // Live provider list discovered from installed provider CRs. null means
  // discovery has not produced a list, so the static config list is used.
  const [discoveredProviders, setDiscoveredProviders] = useState<string[] | null>(null);
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

  const [detailTarget, setDetailTarget] = useState<SimpleCluster | null>(null);

  const [kubeconfigOpen, setKubeconfigOpen] = useState(false);
  const [kubeconfigClusterName, setKubeconfigClusterName] = useState<string | null>(null);
  const [kubeconfigContents, setKubeconfigContents] = useState<string | null>(null);

  const [toastText, setToastText] = useState('');
  const [toastOpen, setToastOpen] = useState(false);
  const flashToast = useCallback((msg: string) => {
    setToastText(msg);
    setToastOpen(true);
  }, []);

  const subscriptionActiveRef = useRef(false);

  const requestController = useRef(new AbortController());
  const currentContext = useRef(ctx);
  useEffect(() => subscribeToLuigi((next) => {
    const previous = currentContext.current;
    const changed = previous.workspacePath !== next.workspacePath ||
      previous.namespace !== next.namespace || previous.graphqlUrl !== next.graphqlUrl;
    requestController.current.abort();
    requestController.current = new AbortController();
    currentContext.current = next;
    if (changed) {
      setClusters([]);
      setLoading(true);
      setError(null);
      setDetailTarget(null);
      setDeleteTarget(null);
      setCreateOpen(false);
      setCreateBusy(false);
      setDeleteBusy(false);
      setKubeconfigOpen(false);
      setKubeconfigContents(null);
      setDiscoveredProviders(null);
      setEntitlements(new Map());
    }
    setCtx(next);
  }), []);
  useEffect(() => () => requestController.current.abort(), []);

  useEffect(() => {
    void loadClusterConfig().then(setConfig);
  }, []);

  const client: GraphqlClientOptions = useMemo(
    () => ({
      graphqlUrl: ctx.graphqlUrl ?? '',
      token: ctx.token,
      workspacePath: ctx.workspacePath,
      signal: requestController.current.signal
    }),
    [ctx]
  );

  const usingDynamicProviders = config.providerSources.length > 0;

  const discoverProviders = useCallback(async () => {
    if (!usingDynamicProviders) {
      setDiscoveredProviders(null);
      return;
    }
    // Provider CRs are cluster-scoped in KCP, so discovery needs the gateway
    // but not a namespace.
    if (!ctx.graphqlUrl || !ctx.workspacePath || client.signal?.aborted) return;
    setProvidersLoading(true);
    try {
      const result = await fetchAvailableProviders(client, ctx.namespace ?? '', config.providerSources);
      // ran=false means discovery could not run — keep the static fallback.
      if (!client.signal?.aborted) setDiscoveredProviders(result.ran ? result.providers : null);
    } catch (err) {
      console.warn('[simple-cluster-ui] provider discovery failed', err);
    } finally {
      if (!client.signal?.aborted) setProvidersLoading(false);
    }
  }, [client, ctx.graphqlUrl, ctx.namespace, config.providerSources, usingDynamicProviders]);
  // note: ctx.namespace stays in deps so discovery reruns if it arrives late.

  useEffect(() => {
    void discoverProviders();
  }, [discoverProviders]);

  // Providers offered in the create dialog: the discovered list when dynamic
  // discovery produced one, otherwise the static config list.
  const providers = useMemo(
    () => (usingDynamicProviders && discoveredProviders !== null ? discoveredProviders : config.providers),
    [usingDynamicProviders, discoveredProviders, config.providers]
  );

  // The catalogue is global and the entitlement per account, so both are resolved
  // together: without an entitlement an entry can only be rendered disabled anyway.
  const discoverAddons = useCallback(async () => {
    if (client.signal?.aborted) return;
    setAddonsLoading(true);
    try {
      const catalog = await loadAddonCatalog();
      if (client.signal?.aborted) return;
      setCatalogUnavailable(!catalog.available);
      const offered = entriesForKind(catalog.entries, 'SimpleCluster');
      setAddons(offered);
      const result = await resolveEntitlements(client, ctx.namespace ?? '', offered);
      if (!client.signal?.aborted) setEntitlements(result);
    } catch (err) {
      if (client.signal?.aborted) return;
      console.warn('[simple-cluster-ui] extension discovery failed', err);
      setAddons([]);
      setCatalogUnavailable(true);
    } finally {
      if (!client.signal?.aborted) setAddonsLoading(false);
    }
  }, [client, ctx.namespace]);

  const openCreate = useCallback(() => {
    if (!ctx.namespace || client.signal?.aborted) return;
    setCreateOpen(true);
    // Refresh so providers and extensions installed since page load appear.
    void discoverProviders();
    void discoverAddons();
  }, [ctx.namespace, client, discoverProviders, discoverAddons]);

  const reload = useCallback(async () => {
    if (!ctx.graphqlUrl || !ctx.workspacePath || client.signal?.aborted) return;
    setLoading(true);
    try {
      const items = await listSimpleClusters(client, ctx.namespace);
      if (client.signal?.aborted) return;
      setClusters(items);
      setError(null);
    } catch (err) {
      if (!client.signal?.aborted) setError((err as Error).message);
    } finally {
      if (!client.signal?.aborted) setLoading(false);
    }
  }, [client, ctx.graphqlUrl, ctx.namespace, ctx.source]);

  useEffect(() => {
    void reload();
  }, [reload]);

  // Auto-refresh: prefer graphql-sse subscriptions; fall back to polling.
  useEffect(() => {
    if (!ctx.graphqlUrl || !ctx.workspacePath) return;

    let reloadQueued = false;
    const queueReload = () => {
      if (reloadQueued) return;
      reloadQueued = true;
      queueMicrotask(() => {
        if (client.signal?.aborted) return;
        reloadQueued = false;
        void reload();
      });
    };

    subscriptionActiveRef.current = true;
    const unsubscribe = subscribeSimpleClusters(client, ctx.namespace, () => {
      subscriptionActiveRef.current = true;
      queueReload();
    });

    const pollHandle = window.setInterval(() => {
      if (!subscriptionActiveRef.current) void reload();
    }, POLL_INTERVAL_MS);

    return () => {
      unsubscribe();
      window.clearInterval(pollHandle);
    };
  }, [client, ctx.graphqlUrl, ctx.namespace, reload]);

  const handleCreate = async (values: CreateValues) => {
    if (!ctx.namespace || client.signal?.aborted) return;
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
        controlPlaneMemory: values.controlPlaneMemory,
        encryption: values.encryption
      });
      if (client.signal?.aborted) return;
      setCreateOpen(false);
      await reload();
    } catch (err) {
      if (!client.signal?.aborted) setError(`Create failed: ${(err as Error).message}`);
    } finally {
      if (!client.signal?.aborted) setCreateBusy(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget?.metadata.namespace || client.signal?.aborted) return;
    setDeleteBusy(true);
    try {
      await deleteSimpleCluster(client, deleteTarget.metadata.namespace, deleteTarget.metadata.name);
      if (client.signal?.aborted) return;
      setDeleteTarget(null);
      await reload();
    } catch (err) {
      if (!client.signal?.aborted) setError(`Delete failed: ${(err as Error).message}`);
    } finally {
      if (!client.signal?.aborted) setDeleteBusy(false);
    }
  };

  const handleDownloadKubeconfig = async (cluster: SimpleCluster) => {
    if (!cluster.metadata.namespace || client.signal?.aborted) return;
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
      const contents = await fetchKubeconfig(client, cluster.metadata.namespace, secretName);
      if (!client.signal?.aborted) setKubeconfigContents(contents);
    } catch (err) {
      if (!client.signal?.aborted) setKubeconfigOpen(false);
      if (!client.signal?.aborted) setError(`Fetch failed: ${(err as Error).message}`);
    }
  };

  const handleAction = (action: ClusterAction, cluster: SimpleCluster) => {
    if (client.signal?.aborted) return;
    switch (action) {
      case 'view':
        if (!cluster.metadata.namespace) {
          setError('The cluster namespace is unavailable.');
          return;
        }
        void navigateToCluster(cluster.metadata.name, cluster.metadata.namespace, client.signal)
          .catch((err: Error) => { if (!client.signal?.aborted) setError(err.message); });
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

  useEffect(() => {
    if (!ctx.clusterName) {
      setDetailTarget(null);
      return;
    }
    if (!ctx.namespace) {
      setError('Select a namespace to open the cluster details.');
      return;
    }
    const cluster = clusters.find((item) => item.metadata.name === ctx.clusterName &&
      item.metadata.namespace === ctx.namespace);
    setDetailTarget(cluster ?? null);
    if (!loading && !cluster) setError('The cluster does not exist in the selected namespace.');
  }, [ctx.clusterName, ctx.namespace, clusters, loading]);

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

  if (!ctx.graphqlUrl || !ctx.workspacePath) {
    return (
      <IllustratedMessage
        name="UnableToLoad"
        titleText="No account context"
        subtitleText="The portal must provide the account workspace and GraphQL gateway."
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

          {!ctx.namespace && (
            <MessageStrip design="Information" hideCloseButton>
              All namespaces are shown. Select a namespace in the portal to create a cluster.
            </MessageStrip>
          )}
          <ClusterList
            clusters={clusters}
            loading={loading}
            canCreate={Boolean(ctx.namespace)}
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
        encryptionProviders={config.encryptionProviders}
        encryptionKeyStores={config.encryptionKeyStores}
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
        onClose={() => {
          void navigateToCluster(null, ctx.namespace, client.signal).catch((err: Error) => { if (!client.signal?.aborted) setError(err.message); });
        }}
        onDownloadKubeconfig={(c) => void handleDownloadKubeconfig(c)}
        encryptionKeyStores={config.encryptionKeyStores}
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
