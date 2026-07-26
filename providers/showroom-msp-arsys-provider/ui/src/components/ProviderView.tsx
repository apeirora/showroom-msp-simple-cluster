import { useCallback, useEffect, useRef, useState } from 'react';
import { FlexBox, FlexBoxDirection, MessageStrip } from '@ui5/webcomponents-react';

import { MachineList } from './MachineList';
import { CreateMachineDialog } from './CreateMachineDialog';
import { DeleteConfirmDialog } from './DeleteConfirmDialog';
import { PasswordDialog } from './PasswordDialog';
import type { GraphqlClientOptions } from '../lib/graphql';
import { arsysMachineApi, passwordSecretName, type CreateInput } from '../lib/machine';
import { fetchPassword, type Credential } from '../lib/secret';
import type { ArsysUbuntuMachine } from '../types';

const POLL_INTERVAL_MS = 5000;

interface Props {
  client: GraphqlClientOptions;
  namespace: string | null;
  ready: boolean;
}

export const ProviderView = ({ client, namespace, ready }: Props) => {
  const [machines, setMachines] = useState<ArsysUbuntuMachine[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [createOpen, setCreateOpen] = useState(false);
  const [createBusy, setCreateBusy] = useState(false);

  const [deleteTarget, setDeleteTarget] = useState<ArsysUbuntuMachine | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);

  const [passwordOpen, setPasswordOpen] = useState(false);
  const [passwordMachine, setPasswordMachine] = useState<string | null>(null);
  const [credential, setCredential] = useState<Credential | null>(null);

  const subscriptionActiveRef = useRef(false);

  const reload = useCallback(async () => {
    if (!ready || !namespace) return;
    setLoading(true);
    try {
      const items = await arsysMachineApi.list(client, namespace);
      setMachines(items);
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
    const unsubscribe = arsysMachineApi.subscribe(client, namespace, () => {
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
      await arsysMachineApi.create(client, namespace, values);
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
      await arsysMachineApi.remove(client, namespace, deleteTarget.metadata.name);
      setDeleteTarget(null);
      await reload();
    } catch (err) {
      setError(`Delete failed: ${(err as Error).message}`);
    } finally {
      setDeleteBusy(false);
    }
  };

  const handleShowPassword = async (machine: ArsysUbuntuMachine) => {
    if (!namespace) return;
    setPasswordMachine(machine.metadata.name);
    setCredential(null);
    setPasswordOpen(true);
    try {
      const cred = await fetchPassword(client, namespace, passwordSecretName(machine.metadata.name));
      setCredential(cred);
    } catch (err) {
      setPasswordOpen(false);
      setError(`Fetch password failed: ${(err as Error).message}`);
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
        machines={machines}
        loading={loading}
        onCreate={() => setCreateOpen(true)}
        onRefresh={() => void reload()}
        onDelete={(machine) => setDeleteTarget(machine)}
        onShowPassword={(machine) => void handleShowPassword(machine)}
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

      <PasswordDialog
        open={passwordOpen}
        machineName={passwordMachine}
        credential={credential}
        onClose={() => setPasswordOpen(false)}
      />
    </FlexBox>
  );
};
