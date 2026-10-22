import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { GraphqlError } from '../src/lib/graphql';
import { createSimpleCluster, listSimpleClusters, resetSchemaProbe } from '../src/lib/simplecluster';
import { encryptionView } from '../src/lib/encryption';
import { loadClusterConfig } from '../src/lib/config';
import type { SimpleCluster } from '../src/types';

const gateway = { graphqlUrl: 'https://portal.test/gateway/api/clusters/root/graphql', token: 't', workspacePath: 'root:orgs:showroom:a' };

const fetchMock = vi.fn();
beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock);
  fetchMock.mockReset();
  resetSchemaProbe();
});
afterEach(() => vi.unstubAllGlobals());

const reply = (body: unknown) => ({ ok: true, status: 200, statusText: 'OK', json: async () => body });
const sentBody = (call = 0) => JSON.parse(fetchMock.mock.calls[call][1].body as string) as {
  query: string; variables: { object?: { spec: Record<string, unknown> } };
};

const base = {
  namespace: 'team-a', name: 'c1', provider: 'cloudferro',
  workerNodes: 1, workerCpu: '2', workerMemory: '4Gi',
  controlPlaneNodes: 1, controlPlaneCpu: '2', controlPlaneMemory: '4Gi'
};

describe('createSimpleCluster and spec.encryption', () => {
  it('sends no encryption field unless asked', async () => {
    fetchMock.mockResolvedValue(reply({ data: { showroom_apeirora_eu: { v1alpha1: { createSimpleCluster: { metadata: {} } } } } }));
    await createSimpleCluster(gateway, base);
    const spec = sentBody().variables.object!.spec;
    expect(spec).not.toHaveProperty('encryption');
    expect(spec.providerConfigRefs).toEqual(['cloudferro']);
  });

  it('sends provider type only when no rotation period is given', async () => {
    fetchMock.mockResolvedValue(reply({ data: { showroom_apeirora_eu: { v1alpha1: { createSimpleCluster: { metadata: {} } } } } }));
    await createSimpleCluster(gateway, { ...base, encryption: { provider: 'OpenBao' } });
    expect(sentBody().variables.object!.spec.encryption).toEqual({ provider: { type: 'OpenBao' } });
  });

  it('sends the rotation period trimmed, and drops a blank one', async () => {
    fetchMock.mockResolvedValue(reply({ data: { showroom_apeirora_eu: { v1alpha1: { createSimpleCluster: { metadata: {} } } } } }));
    await createSimpleCluster(gateway, { ...base, encryption: { provider: 'OpenBao', keyRotationPeriod: ' 720h ' } });
    expect(sentBody(0).variables.object!.spec.encryption).toEqual({ provider: { type: 'OpenBao', keyRotationPeriod: '720h' } });
    await createSimpleCluster(gateway, { ...base, encryption: { provider: 'OpenBao', keyRotationPeriod: '   ' } });
    expect(sentBody(1).variables.object!.spec.encryption).toEqual({ provider: { type: 'OpenBao' } });
  });

  it('surfaces a gateway refusal instead of creating anything', async () => {
    fetchMock.mockResolvedValue(reply({ errors: [{ message: 'Field "encryption" is not defined by type ShowroomApeiroraEuV1alpha1SimpleClusterSpec_Input' }] }));
    await expect(createSimpleCluster(gateway, { ...base, encryption: { provider: 'OpenBao' } })).rejects.toThrow(/encryption/);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});

describe('listSimpleClusters on a gateway schema without spec.encryption', () => {
  const items = [{ metadata: { name: 'c1', namespace: 'team-a' }, spec: {} }];
  const listReply = reply({ data: { showroom_apeirora_eu: { v1alpha1: { SimpleClusters: { items } } } } });
  const unknownField = reply({ errors: [{ message: 'Cannot query field "encryption" on type "ShowroomApeiroraEuV1alpha1SimpleClusterSpec".' }] });
  const selectsEncryption = (call: number) => sentBody(call).query.includes('encryption { provider { type keyRotationPeriod } }');
  let warn: ReturnType<typeof vi.spyOn>;
  beforeEach(() => { warn = vi.spyOn(console, 'warn').mockImplementation(() => {}); });
  afterEach(() => warn.mockRestore());

  it('selects encryption by default and keeps doing so once the gateway accepted it', async () => {
    fetchMock.mockResolvedValue(listReply);
    expect(await listSimpleClusters(gateway, 'team-a')).toEqual(items);
    expect(selectsEncryption(0)).toBe(true);
    await listSimpleClusters({ ...gateway, workspacePath: 'root:orgs:showroom:b' }, 'team-b');
    expect(selectsEncryption(1)).toBe(true);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('falls back once per gateway on the unknown-field error and remembers across accounts', async () => {
    fetchMock.mockResolvedValueOnce(unknownField).mockResolvedValue(listReply);
    expect(await listSimpleClusters(gateway, 'team-a')).toEqual(items);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(selectsEncryption(1)).toBe(false);
    expect(warn).toHaveBeenCalledTimes(1);
    // Another account on the same gateway: no new probe.
    await listSimpleClusters({ ...gateway, workspacePath: 'root:orgs:showroom:b' }, null);
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(selectsEncryption(2)).toBe(false);
    // Another gateway is probed on its own and may well have the field.
    await listSimpleClusters({ ...gateway, graphqlUrl: 'https://other.test/gateway/api/clusters/root/graphql' }, 'team-a');
    expect(fetchMock).toHaveBeenCalledTimes(4);
    expect(selectsEncryption(3)).toBe(true);
  });

  it('shares one probe between concurrent first calls', async () => {
    let finishProbe!: (v: unknown) => void;
    fetchMock
      .mockImplementationOnce(() => new Promise((resolve) => { finishProbe = resolve; }))
      .mockResolvedValue(listReply);
    const first = listSimpleClusters(gateway, 'team-a');
    const second = listSimpleClusters(gateway, 'team-b');
    const third = listSimpleClusters(gateway, null);
    await Promise.resolve();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    finishProbe(unknownField);
    expect(await first).toEqual(items);
    expect(await second).toEqual(items);
    expect(await third).toEqual(items);
    // probe + its fallback + one fetch each for the two waiters
    expect(fetchMock).toHaveBeenCalledTimes(4);
    expect([1, 2, 3].map(selectsEncryption)).toEqual([false, false, false]);
    expect(warn).toHaveBeenCalledTimes(1);
  });

  it('propagates every other error unchanged and learns nothing from it', async () => {
    for (const message of [
      'unauthorized',
      'resolver encryption service temporarily unavailable',
      'Cannot query field "encryptionX" on type "Foo"',
      'Cannot query field "nodes" on type "ShowroomApeiroraEuV1alpha1SimpleClusterStatus".'
    ]) {
      fetchMock.mockReset();
      resetSchemaProbe();
      fetchMock.mockResolvedValue(reply({ errors: [{ message }] }));
      await expect(listSimpleClusters(gateway, 'team-a')).rejects.toBeInstanceOf(GraphqlError);
      expect(fetchMock).toHaveBeenCalledTimes(1);
      // Still unknown: the next call probes again with the field selected.
      fetchMock.mockResolvedValue(listReply);
      await listSimpleClusters(gateway, 'team-a');
      expect(selectsEncryption(1)).toBe(true);
    }
    expect(warn).not.toHaveBeenCalled();
    fetchMock.mockReset();
    resetSchemaProbe();
    fetchMock.mockResolvedValue({ ok: false, status: 502, statusText: 'Bad Gateway', json: async () => ({}) });
    await expect(listSimpleClusters(gateway, 'team-a')).rejects.toThrow(/502/);
    fetchMock.mockResolvedValue(listReply);
    await listSimpleClusters(gateway, 'team-a');
    expect(selectsEncryption(1)).toBe(true);
  });

  it('a waiter probes itself when the shared probe failed for an unrelated reason', async () => {
    let failProbe!: (v: unknown) => void;
    fetchMock
      .mockImplementationOnce(() => new Promise((resolve) => { failProbe = resolve; }))
      .mockResolvedValue(listReply);
    const first = listSimpleClusters(gateway, 'team-a');
    const second = listSimpleClusters(gateway, 'team-b');
    await Promise.resolve();
    failProbe(reply({ errors: [{ message: 'unauthorized' }] }));
    await expect(first).rejects.toThrow(/unauthorized/);
    expect(await second).toEqual(items);
    expect(selectsEncryption(1)).toBe(true);
  });
});

describe('encryptionView', () => {
  const sc = (spec: SimpleCluster['spec'], conditions: NonNullable<SimpleCluster['status']>['conditions'] = []): SimpleCluster =>
    ({ metadata: { name: 'x' }, spec, status: { conditions } });

  it('is not requested without spec.encryption and without encryption conditions', () => {
    expect(encryptionView(sc({}))).toMatchObject({ requested: false, state: 'None', label: '—' });
    expect(encryptionView(sc({ encryption: { provider: { type: '' } } }))).toMatchObject({ requested: false });
    expect(encryptionView(sc({}, [{ type: 'Ready', status: 'True' }, { type: 'Error', status: 'True', reason: 'RenderError' }]))).toMatchObject({ requested: false });
  });

  it('reports the request from the conditions alone when the response carries no spec.encryption', () => {
    // The list fell back to a selection without the field: the operator's conditions still prove the request.
    expect(encryptionView(sc({}, [{ type: 'EncryptionReady', status: 'True', reason: 'Encrypted', message: 'ok' }])))
      .toEqual({ requested: true, provider: undefined, keyRotationPeriod: undefined, state: 'Positive', label: 'Requested', detail: 'ok' });
    expect(encryptionView(sc({}, [{ type: 'EncryptionReady', status: 'False', reason: 'Provisioning', message: 'waiting' }])))
      .toMatchObject({ requested: true, state: 'Critical', label: 'Requested' });
    expect(encryptionView(sc({}, [{ type: 'Error', status: 'True', reason: 'EncryptionUnsupported', message: 'no' }])))
      .toMatchObject({ requested: true, state: 'Negative', label: 'Requested', detail: 'no' });
  });

  it('is pending until the operator verifies it', () => {
    expect(encryptionView(sc({ encryption: { provider: { type: 'OpenBao' } } }))).toMatchObject({ requested: true, state: 'Critical', label: 'OpenBao' });
    expect(encryptionView(sc({ encryption: { provider: { type: 'OpenBao' } } },
      [{ type: 'EncryptionReady', status: 'False', reason: 'Provisioning', message: 'waiting' }]))).toMatchObject({ state: 'Critical', detail: 'waiting' });
  });

  it('is verified on EncryptionReady=True and carries the rotation period', () => {
    expect(encryptionView(sc({ encryption: { provider: { type: 'OpenBao', keyRotationPeriod: '720h' } } },
      [{ type: 'EncryptionReady', status: 'True', reason: 'Encrypted', message: 'ok' }]))).toMatchObject({ state: 'Positive', keyRotationPeriod: '720h', detail: 'ok' });
  });

  it('is negative when refused or not verified by the provider', () => {
    expect(encryptionView(sc({ encryption: { provider: { type: 'OpenBao' } } },
      [{ type: 'Error', status: 'True', reason: 'EncryptionUnsupported', message: 'SCPC "dati" does not support OpenBao encryption; nothing was created' }]))).toMatchObject({ state: 'Negative', detail: expect.stringContaining('does not support') });
    expect(encryptionView(sc({ encryption: { provider: { type: 'OpenBao' } } },
      [{ type: 'EncryptionReady', status: 'False', reason: 'ProviderNotVerified', message: 'not verified' }]))).toMatchObject({ state: 'Negative' });
    expect(encryptionView(sc({ encryption: { provider: { type: 'OpenBao' } } },
      [{ type: 'EncryptionReady', status: 'False', reason: 'UnsupportedByProvider', message: 'no' }]))).toMatchObject({ state: 'Negative' });
  });
});

describe('loadClusterConfig and encryptionProviders', () => {
  it('keeps only provider types the API admits, trimmed and deduplicated, and defaults to none', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    fetchMock.mockResolvedValue(reply({ encryptionProviders: {
      cloudferro: ['OpenBao', '', 3, ' OpenBao ', 'OpenBao'],
      'arsys-virtual': ['OtherKMS', 'openbao'],
      dati: [], broken: 'OpenBao', '': ['OpenBao'], ' spaced ': ['OpenBao']
    } }));
    expect((await loadClusterConfig()).encryptionProviders).toEqual({ cloudferro: ['OpenBao'], spaced: ['OpenBao'] });
    expect(warn.mock.calls.map((c) => String(c[0]))).toEqual([
      expect.stringContaining('arsys-virtual: "OtherKMS"'),
      expect.stringContaining('arsys-virtual: "openbao"')
    ]);
    fetchMock.mockResolvedValue(reply({ cpu: ['1'] }));
    expect((await loadClusterConfig()).encryptionProviders).toEqual({});
    fetchMock.mockResolvedValue(reply({ encryptionProviders: ['OpenBao'] }));
    expect((await loadClusterConfig()).encryptionProviders).toEqual({});
    warn.mockRestore();
  });
});
