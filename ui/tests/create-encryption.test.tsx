import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { ClusterConfig } from '../src/types';

// The UI5 web components do not render in jsdom; each is replaced by the
// native element the dialog's handlers read from (e.target.value / .checked,
// e.detail.selectedOption.getAttribute('value')).
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
  Input: ({ id, value, placeholder, onInput }: any) => (
    <input id={id} value={value} placeholder={placeholder} onInput={onInput} onChange={() => undefined} />
  ),
  Label: ({ children, ...rest }: any) => <label htmlFor={rest.for}>{children}</label>,
  MessageStrip: ({ children }: any) => <div>{children}</div>,
  Option: ({ value, children }: any) => <option value={value}>{children}</option>,
  Select: ({ id, onChange, children }: any) => (
    <select id={id} onChange={(e) => onChange({ detail: { selectedOption: (e.target as HTMLSelectElement).selectedOptions[0] } })}>{children}</select>
  ),
  StepInput: ({ id, value, onChange }: any) => <input id={id} type="number" value={value} onChange={onChange} />
}));

import { CreateClusterDialog, type CreateValues } from '../src/components/CreateClusterDialog';

const config: ClusterConfig = {
  providers: [], providerSources: [], encryptionProviders: {},
  cpu: ['2'], memory: ['4Gi'], controlPlane: { min: 1, max: 3, default: 1 }, workers: { min: 1, max: 3, default: 1 }
};

const open = (encryptionProviders: Record<string, string[]>, providers = ['cloudferro', 'dati']) => {
  const onSubmit = vi.fn<(values: CreateValues) => void>();
  render(
    <CreateClusterDialog
      open busy={false} config={config} providers={providers} providersLoading={false}
      encryptionProviders={encryptionProviders}
      addons={[]} entitlements={new Map()} addonsLoading={false} catalogUnavailable={false}
      onClose={() => undefined} onSubmit={onSubmit}
    />
  );
  fireEvent.input(screen.getByPlaceholderText('e.g. demo-playground'), { target: { value: 'demo' } });
  return onSubmit;
};
const encryptBox = () => screen.queryByLabelText(/Encrypt Kubernetes Secrets/) as HTMLInputElement | null;
const createButton = () => screen.getByText('Create') as HTMLButtonElement;
const pickProvider = (name: string) => fireEvent.change(document.getElementById('sc-provider')!, { target: { value: name } });

afterEach(cleanup);

it('offers no encryption control when no provider admits one', () => {
  const onSubmit = open({});
  expect(encryptBox()).toBeNull();
  fireEvent.click(createButton());
  expect(onSubmit).toHaveBeenCalledTimes(1);
  expect(onSubmit.mock.calls[0][0]).not.toHaveProperty('encryption');
});

it('offers the control only for a provider that admits it, and submits nothing unless ticked', () => {
  const onSubmit = open({ cloudferro: ['OpenBao'] });
  expect(encryptBox()).not.toBeNull();
  expect(encryptBox()!.checked).toBe(false);
  expect(screen.getByText(/Encrypt Kubernetes Secrets with OpenBao \(KMS\)/)).toBeTruthy();
  expect(document.getElementById('sc-key-rotation')).toBeNull();
  fireEvent.click(createButton());
  expect(onSubmit.mock.calls[0][0]).not.toHaveProperty('encryption');
});

it('submits the provider type when ticked, without a rotation period unless given', () => {
  const onSubmit = open({ cloudferro: ['OpenBao'] });
  fireEvent.click(encryptBox()!);
  expect(encryptBox()!.checked).toBe(true);
  expect(document.getElementById('sc-key-rotation')).not.toBeNull();
  fireEvent.click(createButton());
  expect(onSubmit.mock.calls[0][0].encryption).toEqual({ provider: 'OpenBao' });
  expect(onSubmit.mock.calls[0][0].provider).toBe('cloudferro');
});

it('blocks a rotation period the API would refuse and sends a valid one', () => {
  const onSubmit = open({ cloudferro: ['OpenBao'] });
  fireEvent.click(encryptBox()!);
  const rotation = document.getElementById('sc-key-rotation')!;
  fireEvent.input(rotation, { target: { value: '30m' } });
  expect(createButton().disabled).toBe(true);
  expect(screen.getByText(/at least 1h/)).toBeTruthy();
  fireEvent.input(rotation, { target: { value: '1d' } });
  expect(createButton().disabled).toBe(true);
  fireEvent.input(rotation, { target: { value: '720h' } });
  expect(createButton().disabled).toBe(false);
  fireEvent.click(createButton());
  expect(onSubmit.mock.calls[0][0].encryption).toEqual({ provider: 'OpenBao', keyRotationPeriod: '720h' });
});

it('drops the request when the provider changes to one without the option', () => {
  const onSubmit = open({ cloudferro: ['OpenBao'] });
  fireEvent.click(encryptBox()!);
  fireEvent.input(document.getElementById('sc-key-rotation')!, { target: { value: '30m' } });
  expect(createButton().disabled).toBe(true);
  pickProvider('dati');
  expect(encryptBox()).toBeNull();
  expect(createButton().disabled).toBe(false);
  fireEvent.click(createButton());
  expect(onSubmit.mock.calls[0][0].provider).toBe('dati');
  expect(onSubmit.mock.calls[0][0]).not.toHaveProperty('encryption');
  // Back to the capable provider: the box starts unticked again.
  pickProvider('cloudferro');
  expect(encryptBox()!.checked).toBe(false);
});

it('ignores entries that are not usable provider types', () => {
  const onSubmit = open({ cloudferro: ['', '  '] });
  expect(encryptBox()).toBeNull();
  fireEvent.click(createButton());
  expect(onSubmit.mock.calls[0][0]).not.toHaveProperty('encryption');
});

// The component takes whatever list loadClusterConfig admitted; that the list
// only ever holds API-admitted types is loadClusterConfig's job (tested in
// encryption-payload.test.ts), so a second name here only exercises the select.
it('lets the user pick the type when a provider admits several', () => {
  const onSubmit = open({ cloudferro: ['OpenBao', 'OtherKMS'] });
  fireEvent.click(encryptBox()!);
  fireEvent.change(document.getElementById('sc-encrypt-provider')!, { target: { value: 'OtherKMS' } });
  fireEvent.click(createButton());
  expect(onSubmit.mock.calls[0][0].encryption).toEqual({ provider: 'OtherKMS' });
});
