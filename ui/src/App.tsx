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
import { fetchAvailableProviders } from './lib/providers';
import type { ClusterConfig, SimpleCluster } from './types';

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

  const usingDynamicProviders = config.providerSources.length > 0;

  const discoverProviders = useCallback(async () => {
    if (!usingDynamicProviders) {
      setDiscoveredProviders(null);
      return;
    }
    // Provider CRs are cluster-scoped in KCP, so discovery needs the gateway
    // but not a namespace.
    if (!ctx.graphqlUrl) return;
    setProvidersLoading(true);
    try {
      const result = await fetchAvailableProviders(client, ctx.namespace ?? '', config.providerSources);
      // ran=false means discovery could not run — keep the static fallback.
      setDiscoveredProviders(result.ran ? result.providers : null);
    } catch (err) {
      console.warn('[simple-cluster-ui] provider discovery failed', err);
    } finally {
      setProvidersLoading(false);
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

  const openCreate = useCallback(() => {
    setCreateOpen(true);
    // Refresh so providers installed since page load appear.
    void discoverProviders();
  }, [discoverProviders]);

  const reload = useCallback(async () => {
    if (!ctx.graphqlUrl || !ctx.namespace) return;
    setLoading(true);
    try {
      const items = await listSimpleClusters(client, ctx.namespace);
      setClusters(items);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, [client, ctx.graphqlUrl, ctx.namespace]);

  useEffect(() => {
    void reload();
  }, [reload]);

  // Auto-refresh: prefer graphql-sse subscriptions; fall back to polling.
  useEffect(() => {
    if (!ctx.graphqlUrl || !ctx.namespace) return;

    let reloadQueued = false;
    const queueReload = () => {
      if (reloadQueued) return;
      reloadQueued = true;
      queueMicrotask(() => {
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
    if (!ctx.namespace) return;
    setCreateBusy(true);
    try {
      await createSimpleCluster(client, {
        namespace: ctx.namespace,
        name: values.name,
        provider: values.provider,
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
        setDetailTarget(cluster);
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
        onClose={() => setDetailTarget(null)}
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
