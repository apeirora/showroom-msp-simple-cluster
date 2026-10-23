import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ClusterConfig, KeyStoreInfo, SimpleCluster } from '../src/types';
import { keyStoreSummary } from '../src/lib/encryption';
import { loadClusterConfig } from '../src/lib/config';

// UI5 web components do not render in jsdom; each becomes the native element
// the components' handlers and the assertions read from.
vi.mock('@ui5/webcomponents-react', () => ({
  Bar: ({ startContent, endContent }: any) => <div>{startContent}{endContent}</div>,
  BusyIndicator: () => <span>Loading</span>,
  Button: ({ children, onClick, disabled }: any) => <button onClick={onClick} disabled={disabled}>{children}</button>,
  CheckBox: ({ id, text, checked, disabled, onChange }: any) => (
    <label><input id={id} type="checkbox" aria-label={text} checked={checked} disabled={disabled} onChange={onChange} />{text}</label>
  ),
  Dialog: ({ open, headerText, children, footer }: any) => open ? <div role="dialog"><h2>{headerText}</h2>{children}{footer}</div> : null,
  FlexBox: ({ children }: any) => <div>{children}</div>,
  FlexBoxDirection: {},
  Input: ({ id, value, placeholder, onInput, readonly }: any) => (
    <input id={id} value={value} placeholder={placeholder} onInput={onInput} readOnly={Boolean(readonly)} onChange={() => undefined} />
  ),
  Label: ({ children, ...rest }: any) => <label htmlFor={rest.for}>{children}</label>,
  MessageStrip: ({ children }: any) => <div>{children}</div>,
  ObjectStatus: ({ children }: any) => <span>{children}</span>,
  Option: ({ value, children }: any) => <option value={value}>{children}</option>,
  Panel: ({ children, headerText }: any) => <section><h3>{headerText}</h3>{children}</section>,
  Select: ({ id, onChange, children }: any) => (
    <select id={id} onChange={(e) => onChange({ detail: { selectedOption: (e.target as HTMLSelectElement).selectedOptions[0] } })}>{children}</select>
  ),
  StepInput: ({ id, value, onChange }: any) => <input id={id} type="number" value={value} onChange={onChange} />,
  Title: ({ children }: any) => <h2>{children}</h2>
}));

import { CreateClusterDialog } from '../src/components/CreateClusterDialog';
import { ClusterDetail } from '../src/components/ClusterDetail';

const openBao: KeyStoreInfo = {
  address: 'https://openbao.cc-d2.showroom.apeirora.eu',
  namespace: 'simple-cluster',
  keys: 'One Transit key per cluster, created with the cluster and removed with it.'
};

describe('keyStoreSummary', () => {
  it('names the store, its host and namespace from configuration', () => {
    expect(keyStoreSummary('OpenBao', openBao)).toBe(
      'OpenBao at openbao.cc-d2.showroom.apeirora.eu, namespace simple-cluster'
    );
    expect(keyStoreSummary('OpenBao', { address: 'https://bao.example:8200/' })).toBe('OpenBao at bao.example:8200');
    expect(keyStoreSummary('OpenBao', { name: 'Platform OpenBao', address: 'not a url' })).toBe(
      'Platform OpenBao at not a url'
    );
    expect(keyStoreSummary('OpenBao', { namespace: 'tenant-x' })).toBe('OpenBao, namespace tenant-x');
  });

  it('never shows an empty host for an address the URL parser reads differently', () => {
    expect(keyStoreSummary('OpenBao', { address: 'host:8200' })).toBe('OpenBao at host:8200');
    expect(keyStoreSummary('OpenBao', { address: 'javascript:alert(1)' })).toBe('OpenBao at javascript:alert(1)');
    expect(keyStoreSummary('OpenBao', { address: 'https://user:pw@host/x' })).toBe('OpenBao at host');
    expect(keyStoreSummary('OpenBao', { address: 'http://[::1]:8200' })).toBe('OpenBao at [::1]:8200');
    for (const a of ['', ' ', '//host', 'https://']) expect(() => keyStoreSummary('OpenBao', { address: a })).not.toThrow();
  });

  it('says so when nothing is configured instead of guessing', () => {
    const line = keyStoreSummary('OpenBao');
    expect(line).toMatch(/^OpenBao, operated by the platform/);
    expect(line).toMatch(/not published/);
    expect(keyStoreSummary('OpenBao', {})).toBe(line);
    expect(keyStoreSummary('OpenBao', { keys: 'per cluster' })).toBe(line);
  });
});

describe('loadClusterConfig and encryptionKeyStores', () => {
  const fetchMock = vi.fn();
  beforeEach(() => { vi.stubGlobal('fetch', fetchMock); fetchMock.mockReset(); });
  afterEach(() => vi.unstubAllGlobals());
  const reply = (body: unknown) => ({ ok: true, status: 200, statusText: 'OK', json: async () => body });

  it('keeps trimmed string fields for admitted types only, and defaults to none', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    fetchMock.mockResolvedValue(reply({ encryptionKeyStores: {
      ' OpenBao ': { address: ' https://bao ', namespace: 'ns', keys: '', extra: 'x', name: 7 },
      OtherKMS: { address: 'https://other' },
      '': { address: 'https://none' },
      broken: 'https://string'
    } }));
    expect((await loadClusterConfig()).encryptionKeyStores).toEqual({ OpenBao: { address: 'https://bao', namespace: 'ns' } });
    expect(warn.mock.calls.map((c) => String(c[0]))).toEqual([expect.stringContaining('encryptionKeyStores.OtherKMS')]);
    fetchMock.mockResolvedValue(reply({ encryptionKeyStores: { OpenBao: { address: '   ' } } }));
    expect((await loadClusterConfig()).encryptionKeyStores).toEqual({});
    fetchMock.mockResolvedValue(reply({ cpu: ['1'] }));
    expect((await loadClusterConfig()).encryptionKeyStores).toEqual({});
    fetchMock.mockResolvedValue(reply({ encryptionKeyStores: [] }));
    expect((await loadClusterConfig()).encryptionKeyStores).toEqual({});
    // Prototype-named keys in the served JSON must neither survive nor poison Object.prototype.
    fetchMock.mockResolvedValue(reply(JSON.parse('{"encryptionKeyStores":{"__proto__":{"address":"https://evil"},"constructor":{"address":"https://evil"},"OpenBao":{"address":"https://ok","__proto__":{"keys":"x"}}}}')));
    const poisoned = await loadClusterConfig();
    expect(Object.keys(poisoned.encryptionKeyStores)).toEqual(['OpenBao']);
    expect(poisoned.encryptionKeyStores.OpenBao).toEqual({ address: 'https://ok' });
    expect(({} as { address?: string }).address).toBeUndefined();
    warn.mockRestore();
  });
});

const config: ClusterConfig = {
  providers: [], providerSources: [], encryptionProviders: {}, encryptionKeyStores: {},
  cpu: ['2'], memory: ['4Gi'], controlPlane: { min: 1, max: 3, default: 1 }, workers: { min: 1, max: 3, default: 1 }
};
const openDialog = (encryptionProviders: Record<string, string[]>, encryptionKeyStores: Record<string, KeyStoreInfo>) =>
  render(
    <CreateClusterDialog
      open busy={false} config={config} providers={['cloudferro', 'dati']} providersLoading={false}
      encryptionProviders={encryptionProviders} encryptionKeyStores={encryptionKeyStores}
      addons={[]} entitlements={new Map()} addonsLoading={false} catalogUnavailable={false}
      onClose={() => undefined} onSubmit={() => undefined}
    />
  );
const keyStoreField = () => document.getElementById('sc-key-store') as HTMLInputElement | null;

describe('create dialog key storage field', () => {
  afterEach(cleanup);

  it('shows where the keys are stored, read-only, before the box is ticked', () => {
    openDialog({ cloudferro: ['OpenBao'] }, { OpenBao: openBao });
    const field = keyStoreField();
    expect(field).not.toBeNull();
    expect(field!.readOnly).toBe(true);
    expect(field!.value).toBe('OpenBao at openbao.cc-d2.showroom.apeirora.eu, namespace simple-cluster');
    expect(screen.getByText(openBao.keys!)).toBeTruthy();
    expect(screen.getByText('Key storage')).toBeTruthy();
  });

  it('says the location is not published when nothing is configured', () => {
    openDialog({ cloudferro: ['OpenBao'] }, {});
    expect(keyStoreField()!.value).toMatch(/not published/);
  });

  it('is absent together with the encryption section', () => {
    openDialog({}, { OpenBao: openBao });
    expect(keyStoreField()).toBeNull();
    fireEvent.change(document.getElementById('sc-provider')!, { target: { value: 'dati' } });
    expect(keyStoreField()).toBeNull();
  });

  it('follows the chosen type when a provider admits several', () => {
    openDialog({ cloudferro: ['OpenBao', 'OtherKMS'] }, { OpenBao: openBao, OtherKMS: { address: 'https://other.example' } });
    expect(keyStoreField()!.value).toMatch(/^OpenBao at/);
    fireEvent.click(screen.getByLabelText(/Encrypt Kubernetes Secrets/));
    fireEvent.change(document.getElementById('sc-encrypt-provider')!, { target: { value: 'OtherKMS' } });
    expect(keyStoreField()!.value).toBe('OtherKMS at other.example');
  });
});

describe('detail view key storage line', () => {
  afterEach(cleanup);
  // The operator reports EncryptionReady only while encryption is requested,
  // so the unencrypted fixture carries no such condition.
  const cluster = (spec: SimpleCluster['spec']): SimpleCluster => ({
    apiVersion: 'showroom.apeirora.eu/v1alpha1', kind: 'SimpleCluster',
    metadata: { name: 'c1', namespace: 'default' },
    spec,
    status: { phase: 'Ready', conditions: spec.encryption ? [{ type: 'EncryptionReady', status: 'True', reason: 'Encrypted', message: 'Secrets are encrypted at rest with OpenBao (verified on cluster)', lastTransitionTime: '2026-10-23T00:00:00Z' }] : [] }
  } as SimpleCluster);

  it('tells where the keys of an encrypted cluster live', () => {
    render(<ClusterDetail open cluster={cluster({ encryption: { provider: { type: 'OpenBao' } } } as SimpleCluster['spec'])} onClose={() => undefined} encryptionKeyStores={{ OpenBao: openBao }} />);
    expect(screen.getByText(/Keys: OpenBao at openbao\.cc-d2\.showroom\.apeirora\.eu, namespace simple-cluster\. One Transit key per cluster/)).toBeTruthy();
  });

  it('says the location is not published without configuration, and nothing without a request', () => {
    render(<ClusterDetail open cluster={cluster({ encryption: { provider: { type: 'OpenBao' } } } as SimpleCluster['spec'])} onClose={() => undefined} />);
    expect(screen.getByText(/Keys: OpenBao, operated by the platform/)).toBeTruthy();
    cleanup();
    render(<ClusterDetail open cluster={cluster({} as SimpleCluster['spec'])} onClose={() => undefined} encryptionKeyStores={{ OpenBao: openBao }} />);
    expect(screen.queryByText(/Keys:/)).toBeNull();
    expect(screen.getByText('Not requested')).toBeTruthy();
  });
});
