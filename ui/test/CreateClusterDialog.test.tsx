import { Children, isValidElement, type ReactNode } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CreateClusterDialog } from '../src/components/CreateClusterDialog';
import { config, obsoleteSource, source, standardSources } from './providerFixture';

// Exercise React state/events without UI5 shadow-DOM implementation details.
// Select adapters retain the UI5 selectedOption event shape.
vi.mock('@ui5/webcomponents-react', () => ({
  Dialog: ({ open, children, footer }: { open: boolean; children: ReactNode; footer: ReactNode }) =>
    open ? <div role="dialog">{children}{footer}</div> : null,
  FlexBox: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  FlexBoxDirection: { Column: 'column' },
  Bar: ({ endContent }: { endContent: ReactNode }) => <div>{endContent}</div>,
  BusyIndicator: () => <span>Loading</span>,
  MessageStrip: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  Label: ({ for: htmlFor, children }: { for: string; children: ReactNode }) => <label htmlFor={htmlFor}>{children}</label>,
  Button: ({ children, disabled, onClick }: { children: ReactNode; disabled: boolean; onClick: () => void }) =>
    <button disabled={disabled} onClick={onClick}>{children}</button>,
  Input: ({ id, value, onInput }: { id: string; value: string; onInput: () => void }) =>
    <input id={id} value={value} onInput={onInput} onChange={() => {}} />,
  StepInput: ({ id, value, onChange }: { id: string; value: number; onChange: () => void }) =>
    <input type="number" id={id} value={value} onChange={onChange} />,
  CheckBox: ({ text, checked, onChange }: { text: string; checked: boolean; onChange: () => void }) =>
    <label><input type="checkbox" checked={checked} onChange={onChange} />{text}</label>,
  Select: ({ id, children, onChange }: { id: string; children: ReactNode; onChange: (event: unknown) => void }) => {
    const selected = Children.toArray(children).find((child) => isValidElement(child) && child.props.selected);
    const value = isValidElement(selected) ? selected.props.value : '';
    return <select id={id} value={value} onChange={(event) =>
      onChange({ detail: { selectedOption: event.target.selectedOptions[0] } })}>{children}</select>;
  },
  Option: ({ value, children }: { value: string; children: ReactNode }) => <option value={value}>{children}</option>
}));

afterEach(cleanup);

const props = () => ({
  open: true, busy: false, config, providers: ['example', 'legacy'], providersLoading: false,
  addons: [], entitlements: new Map(), addonsLoading: false, catalogUnavailable: false,
  onClose: vi.fn(), onSubmit: vi.fn()
});

const enterName = (name = 'my-cluster') =>
  fireEvent.input(screen.getByLabelText('Name'), { target: { value: name } });
const change = (id: string, value: string) =>
  fireEvent.change(document.getElementById(id)!, { target: { value } });
const create = () => screen.getByRole('button', { name: 'Create' }) as HTMLButtonElement;

describe('standard create dialog', () => {
  it('shows provider branding and submits ordinary user-selected sizing', () => {
    const options = props();
    render(<CreateClusterDialog {...options} />);
    expect(screen.getByRole('option', { name: 'Example Cloud' })).toBeTruthy();
    expect(screen.getByRole('img', { name: 'Example Cloud logo' }).getAttribute('src')).toBe('/provider.svg');
    expect(screen.getByText('Control plane')).toBeTruthy();
    expect(screen.getByText('Workers')).toBeTruthy();
    enterName();
    change('sc-wkr-nodes', '3');
    change('sc-wkr-cpu', '2');
    change('sc-wkr-mem', '4Gi');
    change('sc-cp-nodes', '2');
    fireEvent.click(create());
    expect(options.onSubmit).toHaveBeenCalledWith({
      name: 'my-cluster', provider: 'example', labels: {},
      workerNodes: 3, workerAutoscaling: false, workerMin: 1, workerMax: 3,
      workerCpu: '2', workerMemory: '4Gi', controlPlaneNodes: 2,
      controlPlaneCpu: '1', controlPlaneMemory: '2Gi'
    });
  });

  it.each(standardSources)('retains the same generic controls for $providerConfigName', (provider) => {
    const options = props();
    render(<CreateClusterDialog {...options} providers={[provider.providerConfigName!]}
      config={{ ...config, providerSources: [provider] }} />);
    enterName();
    expect(screen.getAllByLabelText('Nodes')).toHaveLength(2);
    expect(screen.getAllByLabelText('CPU / node')).toHaveLength(2);
    expect(screen.getAllByLabelText('Memory / node')).toHaveLength(2);
    expect(screen.queryByLabelText('Region')).toBeNull();
    expect(screen.queryByLabelText('Cluster size')).toBeNull();
    fireEvent.click(create());
    expect(options.onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      provider: provider.providerConfigName, workerNodes: 1, workerCpu: '1', workerMemory: '2Gi', controlPlaneNodes: 1
    }));
    expect(options.onSubmit.mock.calls[0][0]).not.toHaveProperty('inputs');
  });

  it.each([
    obsoleteSource,
    { ...source, inputs: null, sizingPresets: null },
    { ...source, inputs: {}, sizingPresets: {} },
    { ...source, inputs: [null], sizingPresets: [null] },
    { ...source, inputs: [], sizingPresets: [] }
  ])('ignores obsolete provider configuration without replacing or blocking generic sizing: %j', (oldSource) => {
    const options = props();
    render(<CreateClusterDialog {...options} config={{ ...config, providerSources: [oldSource] }} />);
    enterName();
    expect(screen.queryByLabelText('Region')).toBeNull();
    expect(screen.queryByLabelText('Cluster size')).toBeNull();
    change('sc-wkr-nodes', '3');
    expect(create().disabled).toBe(false);
    fireEvent.click(create());
    expect(options.onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      workerNodes: 3, workerAutoscaling: false, workerCpu: '1', workerMemory: '2Gi', controlPlaneNodes: 1
    }));
    expect(options.onSubmit.mock.calls[0][0]).not.toHaveProperty('inputs');
  });

  it('retains selected generic sizing when switching providers', () => {
    const options = props();
    render(<CreateClusterDialog {...options} config={{ ...config, providerSources: [obsoleteSource] }} />);
    enterName();
    change('sc-wkr-nodes', '4');
    change('sc-wkr-cpu', '2');
    fireEvent.change(screen.getByLabelText('Provider'), { target: { value: 'legacy' } });
    expect(screen.queryByRole('img')).toBeNull();
    fireEvent.click(create());
    expect(options.onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      provider: 'legacy', workerNodes: 4, workerCpu: '2', workerAutoscaling: false
    }));
  });

  it('retains editable autoscaling with validated generic min/max', () => {
    const options = props();
    render(<CreateClusterDialog {...options} />);
    enterName();
    fireEvent.click(screen.getByRole('checkbox', { name: /Autoscaling/ }));
    change('sc-wkr-min', '4');
    change('sc-wkr-max', '3');
    expect(create().disabled).toBe(true);
    change('sc-wkr-max', '6');
    fireEvent.click(create());
    expect(options.onSubmit).toHaveBeenCalledWith(expect.objectContaining({
      workerAutoscaling: true, workerMin: 4, workerMax: 6
    }));
  });

  it.each(['', '-cluster', 'cluster-', 'cluster.name', 'a'.repeat(64)])('rejects an invalid cluster name: %s', (name) => {
    const options = props();
    render(<CreateClusterDialog {...options} />);
    enterName(name);
    expect(create().disabled).toBe(true);
    fireEvent.click(create());
    expect(options.onSubmit).not.toHaveBeenCalled();
  });

  it.each(['a', '9', 'a'.repeat(63)])('accepts a valid cluster name: %s', (name) => {
    const options = props();
    render(<CreateClusterDialog {...options} />);
    enterName(name);
    fireEvent.click(create());
    expect(options.onSubmit).toHaveBeenCalledWith(expect.objectContaining({ name }));
  });

  it.each([
    ['sc-wkr-nodes', '0'], ['sc-wkr-nodes', '11'], ['sc-wkr-nodes', '1.5'],
    ['sc-cp-nodes', '0'], ['sc-cp-nodes', '4'], ['sc-cp-nodes', '1.5']
  ])('rejects invalid generic count %s=%s', (id, value) => {
    const options = props();
    render(<CreateClusterDialog {...options} />);
    enterName();
    change(id, value);
    expect(create().disabled).toBe(true);
    fireEvent.click(create());
    expect(options.onSubmit).not.toHaveBeenCalled();
  });

  it('blocks submission while availability is unknown, empty, or loading and while creation is busy', () => {
    const options = props();
    const { rerender } = render(<CreateClusterDialog {...options} providersUnavailable />);
    enterName();
    expect(create().disabled).toBe(true);
    expect(screen.getByText(/availability could not be confirmed/)).toBeTruthy();
    rerender(<CreateClusterDialog {...options} providers={[]} />);
    expect(create().disabled).toBe(true);
    rerender(<CreateClusterDialog {...options} providersLoading />);
    expect(create().disabled).toBe(true);
    rerender(<CreateClusterDialog {...options} busy />);
    expect((screen.getByRole('button', { name: 'Creating…' }) as HTMLButtonElement).disabled).toBe(true);
    expect(options.onSubmit).not.toHaveBeenCalled();
  });
});
