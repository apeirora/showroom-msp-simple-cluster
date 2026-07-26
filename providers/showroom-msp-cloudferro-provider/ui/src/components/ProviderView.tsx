import { useCallback, useEffect, useRef, useState } from 'react';
import { FlexBox, FlexBoxDirection, MessageStrip } from '@ui5/webcomponents-react';

import { MachineList } from './MachineList';
import { CreateMachineDialog } from './CreateMachineDialog';
import { DeleteConfirmDialog } from './DeleteConfirmDialog';
import type { GraphqlClientOptions } from '../lib/graphql';
import { openStackVmApi, type CreateInput } from '../lib/machine';
import type { OpenStackVM } from '../types';

const POLL_INTERVAL_MS = 5000;

interface Props {
  client: GraphqlClientOptions;
  ready: boolean;
}

export const ProviderView = ({ client, ready }: Props) => {
  const [vms, setVms] = useState<OpenStackVM[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [createOpen, setCreateOpen] = useState(false);
  const [createBusy, setCreateBusy] = useState(false);

  const [deleteTarget, setDeleteTarget] = useState<OpenStackVM | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);

  const subscriptionActiveRef = useRef(false);

  const reload = useCallback(async () => {
    if (!ready) return;
    setLoading(true);
    try {
      const items = await openStackVmApi.list(client);
      setVms(items);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, [client, ready]);

  useEffect(() => {
    void reload();
  }, [reload]);

  // Auto-refresh: prefer graphql-sse subscriptions; fall back to polling.
  useEffect(() => {
    if (!ready) return;

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
    const unsubscribe = openStackVmApi.subscribe(client, () => {
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
  }, [client, ready, reload]);

  const handleCreate = async (values: CreateInput) => {
    setCreateBusy(true);
    try {
      await openStackVmApi.create(client, values);
      setCreateOpen(false);
      await reload();
    } catch (err) {
      setError(`Create failed: ${(err as Error).message}`);
    } finally {
      setCreateBusy(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleteBusy(true);
    try {
      await openStackVmApi.remove(client, deleteTarget.metadata.name);
      setDeleteTarget(null);
      await reload();
    } catch (err) {
      setError(`Delete failed: ${(err as Error).message}`);
    } finally {
      setDeleteBusy(false);
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

      <MachineList
        vms={vms}
        loading={loading}
        onCreate={() => setCreateOpen(true)}
        onRefresh={() => void reload()}
        onDelete={(vm) => setDeleteTarget(vm)}
      />

      <CreateMachineDialog
        open={createOpen}
        busy={createBusy}
        onClose={() => setCreateOpen(false)}
        onSubmit={handleCreate}
      />

      <DeleteConfirmDialog
        open={Boolean(deleteTarget)}
        name={deleteTarget?.metadata.name ?? null}
        busy={deleteBusy}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => void handleDelete()}
      />
    </FlexBox>
  );
};
