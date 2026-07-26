import { useCallback, useEffect, useRef, useState } from 'react';
import { FlexBox, FlexBoxDirection, MessageStrip } from '@ui5/webcomponents-react';

import { VmList } from './VmList';
import { CreateVmDialog } from './CreateVmDialog';
import { VmDeleteConfirmDialog } from './VmDeleteConfirmDialog';
import { VmKeyDialog } from './VmKeyDialog';
import type { GraphqlClientOptions } from '../lib/graphql';
import { ironCoreVmApi, type CreateInput } from '../lib/vm';
import { fetchSshPrivateKey } from '../lib/secret';
import type { IronCoreVM } from '../types';

const POLL_INTERVAL_MS = 5000;

interface Props {
  client: GraphqlClientOptions;
  namespace: string | null;
  ready: boolean;
}

// Self-contained IronCore VM page (list + create + delete + SSH key). Mirrors
// the CloudFerro provider's ProviderView, bound to the namespaced IronCoreVM API.
export const VmView = ({ client, namespace, ready }: Props) => {
  const [vms, setVms] = useState<IronCoreVM[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [createOpen, setCreateOpen] = useState(false);
  const [createBusy, setCreateBusy] = useState(false);

  const [deleteTarget, setDeleteTarget] = useState<IronCoreVM | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);

  const [keyOpen, setKeyOpen] = useState(false);
  const [keyVmName, setKeyVmName] = useState<string | null>(null);
  const [keyContents, setKeyContents] = useState<string | null>(null);

  const subscriptionActiveRef = useRef(false);

  const reload = useCallback(async () => {
    if (!ready || !namespace) return;
    setLoading(true);
    try {
      const items = await ironCoreVmApi.list(client, namespace);
      setVms(items);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, [client, namespace, ready]);

  useEffect(() => {
    void reload();
  }, [reload]);

  // Auto-refresh: prefer graphql-sse subscriptions; fall back to polling.
  useEffect(() => {
    if (!ready || !namespace) return;

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
    const unsubscribe = ironCoreVmApi.subscribe(client, namespace, () => {
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
  }, [client, namespace, ready, reload]);

  const handleCreate = async (values: CreateInput) => {
    if (!namespace) return;
    setCreateBusy(true);
    try {
      await ironCoreVmApi.create(client, namespace, values);
      setCreateOpen(false);
      await reload();
    } catch (err) {
      setError(`Create failed: ${(err as Error).message}`);
    } finally {
      setCreateBusy(false);
    }
  };

  const handleDelete = async () => {
    if (!namespace || !deleteTarget) return;
    setDeleteBusy(true);
    try {
      await ironCoreVmApi.remove(client, namespace, deleteTarget.metadata.name);
      setDeleteTarget(null);
      await reload();
    } catch (err) {
      setError(`Delete failed: ${(err as Error).message}`);
    } finally {
      setDeleteBusy(false);
    }
  };

  const handleDownloadKey = async (vm: IronCoreVM) => {
    if (!namespace) return;
    // The operator falls back to the per-VM Secret `<vm-name>-ssh` when
    // status.sshSecretRef is not yet populated.
    const secretName = vm.status?.sshSecretRef?.name ?? `${vm.metadata.name}-ssh`;
    setKeyVmName(vm.metadata.name);
    setKeyContents(null);
    setKeyOpen(true);
    try {
      const contents = await fetchSshPrivateKey(client, namespace, secretName);
      setKeyContents(contents);
    } catch (err) {
      setKeyOpen(false);
      setError(`Fetch SSH key failed: ${(err as Error).message}`);
    }
  };

  return (
    <FlexBox
      direction={FlexBoxDirection.Column}
      style={{ gap: '1rem', maxWidth: 1200, margin: '0 auto', padding: '1.25rem 1rem' }}
    >
      {error && (
        <MessageStrip design="Negative" onClose={() => setError(null)}>
          {error}
        </MessageStrip>
      )}

      <VmList
        vms={vms}
        loading={loading}
        onCreate={() => setCreateOpen(true)}
        onRefresh={() => void reload()}
        onDelete={(vm) => setDeleteTarget(vm)}
        onDownloadKey={(vm) => void handleDownloadKey(vm)}
      />

      <CreateVmDialog
        open={createOpen}
        busy={createBusy}
        onClose={() => setCreateOpen(false)}
        onSubmit={handleCreate}
      />

      <VmDeleteConfirmDialog
        open={Boolean(deleteTarget)}
        vmName={deleteTarget?.metadata.name ?? null}
        busy={deleteBusy}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => void handleDelete()}
      />

      <VmKeyDialog
        open={keyOpen}
        vmName={keyVmName}
        privateKey={keyContents}
        onClose={() => setKeyOpen(false)}
      />
    </FlexBox>
  );
};
