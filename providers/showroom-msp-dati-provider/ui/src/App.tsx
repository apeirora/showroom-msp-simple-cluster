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

import { ShootList, type ShootAction } from './components/ShootList';
import { CreateShootDialog, type CreateValues } from './components/CreateShootDialog';
import { DeleteConfirmDialog } from './components/DeleteConfirmDialog';
import { ShootDetail } from './components/ShootDetail';
import {
  emptyRuntimeContext,
  subscribeToLuigi,
  type RuntimeContext
} from './lib/luigi';
import type { GraphqlClientOptions } from './lib/graphql';
import { createShoot, deleteShoot, listShoots, subscribeShoots } from './lib/shoot';
import type { Shoot } from './types';

const POLL_INTERVAL_MS = 5000;

const App = () => {
  const [ctx, setCtx] = useState<RuntimeContext>(emptyRuntimeContext);
  const [shoots, setShoots] = useState<Shoot[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [createOpen, setCreateOpen] = useState(false);
  const [createBusy, setCreateBusy] = useState(false);

  const [deleteTarget, setDeleteTarget] = useState<Shoot | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);

  const [detailTarget, setDetailTarget] = useState<Shoot | null>(null);

  const [toastText, setToastText] = useState('');
  const [toastOpen, setToastOpen] = useState(false);
  const flashToast = useCallback((msg: string) => {
    setToastText(msg);
    setToastOpen(true);
  }, []);

  const subscriptionActiveRef = useRef(false);

  useEffect(() => subscribeToLuigi(setCtx), []);

  const client: GraphqlClientOptions = useMemo(
    () => ({
      graphqlUrl: ctx.graphqlUrl ?? '',
      token: ctx.token,
      workspacePath: ctx.workspacePath
    }),
    [ctx.graphqlUrl, ctx.token, ctx.workspacePath]
  );

  // Shoots are cluster-scoped in the KCP workspace, so listing needs the
  // gateway URL but no namespace.
  const reload = useCallback(async () => {
    if (!ctx.graphqlUrl) return;
    setLoading(true);
    try {
      const items = await listShoots(client);
      setShoots(items);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, [client, ctx.graphqlUrl]);

  useEffect(() => {
    void reload();
  }, [reload]);

  // Auto-refresh: prefer graphql-sse subscriptions; fall back to polling.
  useEffect(() => {
    if (!ctx.graphqlUrl) return;

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
    const unsubscribe = subscribeShoots(client, () => {
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
  }, [client, ctx.graphqlUrl, reload]);

  const handleCreate = async (values: CreateValues) => {
    setCreateBusy(true);
    try {
      await createShoot(client, { name: values.name, spec: values.spec });
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
      await deleteShoot(client, deleteTarget.metadata.name);
      setDeleteTarget(null);
      await reload();
    } catch (err) {
      setError(`Delete failed: ${(err as Error).message}`);
    } finally {
      setDeleteBusy(false);
    }
  };

  const handleAction = (action: ShootAction, shoot: Shoot) => {
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
      <ShellBar primaryTitle="Dati" secondaryTitle="Gardener Shoots" />
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

          <ShootList
            shoots={shoots}
            loading={loading}
            onCreate={() => setCreateOpen(true)}
            onAction={handleAction}
          />
        </FlexBox>
      </div>

      <CreateShootDialog
        open={createOpen}
        busy={createBusy}
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

export default App;
