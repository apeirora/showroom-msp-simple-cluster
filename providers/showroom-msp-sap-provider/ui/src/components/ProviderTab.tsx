import { useCallback, useEffect, useRef, useState } from 'react';
import {
  FlexBox,
  FlexBoxDirection,
  MessageStrip,
  Toast
} from '@ui5/webcomponents-react';

import { ShootList, type ShootAction } from './ShootList';
import { CreateShootDialog, type CreateValues } from './CreateShootDialog';
import { DeleteConfirmDialog } from './DeleteConfirmDialog';
import { ShootDetail } from './ShootDetail';
import type { GraphqlClientOptions } from '../lib/graphql';
import type { ShootApi } from '../lib/shoot';
import type { ProviderShoot } from '../types';

const POLL_INTERVAL_MS = 5000;

interface Props {
  // GraphQL client resolved from the portal context.
  client: GraphqlClientOptions;
  // Whether the portal has already provided a gateway URL.
  ready: boolean;
  // The Kind-specific CRUD/subscribe surface.
  api: ShootApi;
  // Human label for this flavor, e.g. "OpenStack".
  flavor: string;
  // Suggested default resource name for the create dialog.
  defaultName: string;
  // Ready-to-use default Gardener Shoot manifest pre-filled in the create dialog.
  defaultSpec: string;
}

// One self-contained Shoot page (list + create + delete + detail), bound to a
// single Kind via `api`. Two of these are mounted side by side under the tabs.
export const ProviderTab = ({ client, ready, api, flavor, defaultName, defaultSpec }: Props) => {
  const [shoots, setShoots] = useState<ProviderShoot[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [createOpen, setCreateOpen] = useState(false);
  const [createBusy, setCreateBusy] = useState(false);

  const [deleteTarget, setDeleteTarget] = useState<ProviderShoot | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);

  const [detailTarget, setDetailTarget] = useState<ProviderShoot | null>(null);

  const [toastText, setToastText] = useState('');
  const [toastOpen, setToastOpen] = useState(false);
  const flashToast = useCallback((msg: string) => {
    setToastText(msg);
    setToastOpen(true);
  }, []);

  const subscriptionActiveRef = useRef(false);

  const reload = useCallback(async () => {
    if (!ready) return;
    setLoading(true);
    try {
      const items = await api.list(client);
      setShoots(items);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, [api, client, ready]);

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
    const unsubscribe = api.subscribe(client, () => {
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
  }, [api, client, ready, reload]);

  const handleCreate = async (values: CreateValues) => {
    setCreateBusy(true);
    try {
      await api.create(client, { name: values.name, spec: values.spec });
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
      await api.remove(client, deleteTarget.metadata.name);
      setDeleteTarget(null);
      await reload();
    } catch (err) {
      setError(`Delete failed: ${(err as Error).message}`);
    } finally {
      setDeleteBusy(false);
    }
  };

  const handleAction = (action: ShootAction, shoot: ProviderShoot) => {
    switch (action) {
      case 'view':
        setDetailTarget(shoot);
        break;
      case 'delete':
        flashToast(`Confirm delete for ${shoot.metadata.name}…`);
        setDeleteTarget(shoot);
        break;
    }
  };

  return (
    <FlexBox
      direction={FlexBoxDirection.Column}
      style={{ gap: '1rem', maxWidth: 1200, margin: '0 auto', padding: '1.25rem 0' }}
    >
      {error && (
        <MessageStrip design="Negative" onClose={() => setError(null)}>
          {error}
        </MessageStrip>
      )}

      <ShootList
        shoots={shoots}
        loading={loading}
        onCreate={() => setCreateOpen(true)}
        onAction={handleAction}
      />

      <CreateShootDialog
        open={createOpen}
        busy={createBusy}
        flavor={flavor}
        defaultName={defaultName}
        defaultSpec={defaultSpec}
        onClose={() => setCreateOpen(false)}
        onSubmit={handleCreate}
      />

      <DeleteConfirmDialog
        open={Boolean(deleteTarget)}
        shootName={deleteTarget?.metadata.name ?? null}
        busy={deleteBusy}
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => void handleDelete()}
      />

      <ShootDetail
        open={Boolean(detailTarget)}
        shoot={detailTarget}
        onClose={() => setDetailTarget(null)}
      />

      <Toast open={toastOpen} duration={3000} onClose={() => setToastOpen(false)}>
        {toastText}
      </Toast>
    </FlexBox>
  );
};
