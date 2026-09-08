import { useEffect, useMemo, useState } from 'react';
import {
  Bar,
  BusyIndicator,
  Button,
  CheckBox,
  Dialog,
  FlexBox,
  FlexBoxDirection,
  Input,
  Label,
  MessageStrip,
  Option,
  Select,
  StepInput
} from '@ui5/webcomponents-react';
import type { AddonCatalogEntry, AddonEntitlement, ClusterConfig } from '../types';
import { providerSource, safeLogoUrl } from '../lib/providerPresentation';

const RFC_1123 = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/;

export interface CreateValues {
  name: string;
  provider: string;
  /** Labels for the new object; one per extension ticked. */
  labels: Record<string, string>;
  workerNodes: number;
  workerAutoscaling: boolean;
  workerMin: number;
  workerMax: number;
  workerCpu: string;
  workerMemory: string;
  controlPlaneNodes: number;
  controlPlaneCpu: string;
  controlPlaneMemory: string;
}

interface Props {
  open: boolean;
  busy: boolean;
  config: ClusterConfig;
  // Live provider list (discovered from installed provider CRs when dynamic
  // discovery is enabled, otherwise the static config list).
  providers: string[];
  providersLoading: boolean;
  providersUnavailable?: boolean;
  // Installable extensions offered for this kind, and whether this workspace has each
  // one. Empty hides the section entirely.
  addons: AddonCatalogEntry[];
  entitlements: Map<string, AddonEntitlement>;
  addonsLoading: boolean;
  // The catalogue could not be read at all — a deployment fault, not an empty landscape.
  catalogUnavailable: boolean;
  onClose: () => void;
  onSubmit: (values: CreateValues) => void;
}

const selectValue = (e: { detail: { selectedOption: unknown } }): string =>
  String((e.detail.selectedOption as unknown as HTMLElement).getAttribute('value') ?? '');

export const CreateClusterDialog = ({
  open,
  busy,
  config,
  providers,
  providersLoading,
  providersUnavailable = false,
  addons,
  entitlements,
  addonsLoading,
  catalogUnavailable,
  onClose,
  onSubmit
}: Props) => {
  const [name, setName] = useState('');
  const [enabledAddons, setEnabledAddons] = useState<Set<string>>(new Set());
  const [provider, setProvider] = useState(providers[0] ?? '');
  const details = providerSource(config, provider);
  const logo = safeLogoUrl(details?.logoUrl);

  // Keep the selection valid as the discovered provider list arrives or
  // changes (and whenever the dialog reopens).
  useEffect(() => {
    if (!open) return;
    if (!providers.includes(provider)) {
      setProvider(providers[0] ?? '');
    }
  }, [open, providers, provider]);
  const [workerNodes, setWorkerNodes] = useState(config.workers.default);
  const [workerAutoscaling, setWorkerAutoscaling] = useState(false);
  const [workerMin, setWorkerMin] = useState(config.workers.default);
  const [workerMax, setWorkerMax] = useState(Math.min(config.workers.default + 2, config.workers.max));
  const [workerCpu, setWorkerCpu] = useState(config.cpu[0] ?? '');
  const [workerMemory, setWorkerMemory] = useState(config.memory[0] ?? '');
  const [controlPlaneNodes, setControlPlaneNodes] = useState(config.controlPlane.default);
  const [controlPlaneCpu, setControlPlaneCpu] = useState(config.cpu[0] ?? '');
  const [controlPlaneMemory, setControlPlaneMemory] = useState(config.memory[0] ?? '');
  const [nameTouched, setNameTouched] = useState(false);

  const nameValid = useMemo(() => RFC_1123.test(name) && name.length <= 63, [name]);
  const inRange = (value: number, min: number, max: number) =>
    Number.isSafeInteger(value) && value >= min && value <= max;
  const workersValid = workerAutoscaling
    ? inRange(workerMin, config.workers.min, config.workers.max) &&
      inRange(workerMax, workerMin, config.workers.max)
    : inRange(workerNodes, config.workers.min, config.workers.max);
  const noProviders = !providersLoading && providers.length === 0;
  const canSubmit =
    nameValid &&
    providers.includes(provider) &&
    !providersLoading &&
    !providersUnavailable &&
    workersValid &&
    config.cpu.includes(workerCpu) && config.memory.includes(workerMemory) &&
    inRange(controlPlaneNodes, config.controlPlane.min, config.controlPlane.max) &&
    config.cpu.includes(controlPlaneCpu) && config.memory.includes(controlPlaneMemory);

  // Only a box that is both ticked and entitled becomes a label. Entitlement can change
  // between opening the dialog and submitting it (the check is asynchronous), and a
  // label written for an extension the workspace does not have would install nothing
  // while looking like it had.
  const addonLabels = useMemo(() => {
    const out: Record<string, string> = {};
    for (const entry of addons) {
      if (!enabledAddons.has(entry.id)) continue;
      if (entitlements.get(entry.id)?.state !== 'entitled') continue;
      out[entry.label] = 'true';
    }
    return out;
  }, [addons, enabledAddons, entitlements]);

  const toggleAddon = (id: string, on: boolean) =>
    setEnabledAddons((prev) => {
      const next = new Set(prev);
      if (on) {
        next.add(id);
      } else {
        next.delete(id);
      }
      return next;
    });

  const reset = () => {
    setName('');
    setNameTouched(false);
    setEnabledAddons(new Set());
    setProvider(providers[0] ?? '');
    setWorkerNodes(config.workers.default);
    setWorkerAutoscaling(false);
    setWorkerMin(config.workers.default);
    setWorkerMax(Math.min(config.workers.default + 2, config.workers.max));
    setWorkerCpu(config.cpu[0] ?? '');
    setWorkerMemory(config.memory[0] ?? '');
    setControlPlaneNodes(config.controlPlane.default);
    setControlPlaneCpu(config.cpu[0] ?? '');
    setControlPlaneMemory(config.memory[0] ?? '');
  };

  const handleClose = () => {
    reset();
    onClose();
  };

  const handleSubmit = () => {
    if (!canSubmit || busy) return;
    onSubmit({
      name,
      provider,
      labels: addonLabels,
      workerNodes,
      workerAutoscaling,
      workerMin,
      workerMax,
      workerCpu,
      workerMemory,
      controlPlaneNodes,
      controlPlaneCpu,
      controlPlaneMemory
    });
  };

  return (
    <Dialog
      open={open}
      headerText="New SimpleCluster"
      onClose={handleClose}
      style={{ maxWidth: '95vw' }}
      footer={
        <Bar
          endContent={
            <>
              <Button design="Transparent" onClick={handleClose}>
                Cancel
              </Button>
              <Button design="Emphasized" disabled={!canSubmit || busy} onClick={handleSubmit}>
                {busy ? 'Creating…' : 'Create'}
              </Button>
            </>
          }
        />
      }
    >
      <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '1rem', minWidth: 420, padding: '0.5rem 0' }}>
        <MessageStrip design="Information" hideCloseButton>
          The cluster is provisioned from the selected SimpleClusterProviderConfig.
        </MessageStrip>

        <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
          <Label for="sc-name" required>
            Name
          </Label>
          <Input
            id="sc-name"
            value={name}
            placeholder="e.g. demo-playground"
            valueState={nameTouched && !nameValid ? 'Negative' : 'None'}
            onInput={(e) => {
              setName(String((e.target as unknown as HTMLInputElement).value ?? '').toLowerCase());
              setNameTouched(true);
            }}
          />
          {nameTouched && !nameValid && (
            <span style={{ color: 'var(--sapNegativeColor)', fontSize: '0.75rem' }}>
              Must be a valid RFC 1123 label (lowercase letters, digits, dashes; max 63 chars).
            </span>
          )}
        </FlexBox>

        <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
          <Label for="sc-provider" required>
            Provider
          </Label>
          {providersLoading ? (
            <FlexBox style={{ gap: 8, alignItems: 'center' }}>
              <BusyIndicator active size="S" />
              <span style={{ fontSize: '0.85rem' }}>Loading available providers…</span>
            </FlexBox>
          ) : providersUnavailable ? (
            <MessageStrip design="Critical" hideCloseButton>
              Provider availability could not be confirmed. Check the connection and reopen this dialog.
            </MessageStrip>
          ) : noProviders ? (
            <MessageStrip design="Critical" hideCloseButton>
              No providers are available in this workspace. Install a provider (for
              example Dati) from the marketplace and create its resource, then reopen
              this dialog.
            </MessageStrip>
          ) : (
            <Select id="sc-provider" onChange={(e) => setProvider(selectValue(e))}>
              {providers.map((p) => (
                <Option key={p} value={p} selected={p === provider}>
                  {providerSource(config, p)?.label ?? p}
                </Option>
              ))}
            </Select>
          )}
        </FlexBox>

        {logo && <img src={logo} alt={`${details?.label ?? provider} logo`}
          style={{ width: 120, maxHeight: 56, objectFit: 'contain', alignSelf: 'flex-start' }} />}

        {/*
          * Hidden entirely when no extension is installed on the landscape, so the
          * common case sees no change. Extensions this workspace does not have are
          * shown disabled rather than omitted: "why is Crypto Broker not offered?"
          * then has an answer on screen instead of being invisible.
          */}
        {(addonsLoading || addons.length > 0 || catalogUnavailable) && (
          <>
            <Label>Extensions</Label>
            {catalogUnavailable ? (
              <MessageStrip design="Information" hideCloseButton>
                The list of installable extensions could not be read, so none are offered
                here. The cluster itself is unaffected.
              </MessageStrip>
            ) : addonsLoading ? (
              <FlexBox style={{ gap: 8, alignItems: 'center' }}>
                <BusyIndicator active size="S" />
                <span style={{ fontSize: '0.85rem' }}>Checking available extensions…</span>
              </FlexBox>
            ) : (
              <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 2 }}>
                {addons.map((entry) => {
                  const entitlement = entitlements.get(entry.id);
                  const entitled = entitlement?.state === 'entitled';
                  const hint = entitled ? entry.description : entitlement?.detail;
                  return (
                    <span key={entry.id} title={hint ?? undefined}>
                      <CheckBox
                        text={entry.displayName ?? entry.id}
                        checked={enabledAddons.has(entry.id)}
                        disabled={!entitled}
                        onChange={(e) =>
                          toggleAddon(
                            entry.id,
                            Boolean((e.target as unknown as HTMLInputElement).checked)
                          )
                        }
                      />
                    </span>
                  );
                })}
                <span style={{ fontSize: '0.75rem', color: 'var(--sapContent_LabelColor)' }}>
                  Installed once the cluster is ready. Can be changed later from the
                  extension's own page.
                </span>
              </FlexBox>
            )}
          </>
        )}

        <Label>Control plane</Label>
        <FlexBox style={{ gap: '0.75rem', flexWrap: 'wrap' }}>
          <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
            <Label for="sc-cp-nodes" required>Nodes</Label>
            <StepInput
              id="sc-cp-nodes"
              value={controlPlaneNodes}
              min={config.controlPlane.min}
              max={config.controlPlane.max}
              step={1}
              onChange={(e) => setControlPlaneNodes(Number((e.target as unknown as HTMLInputElement).value))}
            />
          </FlexBox>
          <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
            <Label for="sc-cp-cpu" required>CPU / node</Label>
            <Select id="sc-cp-cpu" onChange={(e) => setControlPlaneCpu(selectValue(e))}>
              {config.cpu.map((v) => (
                <Option key={v} value={v} selected={v === controlPlaneCpu}>
                  {v}
                </Option>
              ))}
            </Select>
          </FlexBox>
          <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
            <Label for="sc-cp-mem" required>Memory / node</Label>
            <Select id="sc-cp-mem" onChange={(e) => setControlPlaneMemory(selectValue(e))}>
              {config.memory.map((v) => (
                <Option key={v} value={v} selected={v === controlPlaneMemory}>
                  {v}
                </Option>
              ))}
            </Select>
          </FlexBox>
        </FlexBox>

        <Label>Workers</Label>
        <CheckBox
          text="Autoscaling — the provider scales the pool between min and max"
          checked={workerAutoscaling}
          onChange={(e) => setWorkerAutoscaling(Boolean((e.target as unknown as HTMLInputElement).checked))}
        />
        <FlexBox style={{ gap: '0.75rem', flexWrap: 'wrap' }}>
          {workerAutoscaling ? (
            <>
              <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
                <Label for="sc-wkr-min" required>
                  Min nodes
                </Label>
                <StepInput
                  id="sc-wkr-min"
                  value={workerMin}
                  min={config.workers.min}
                  max={config.workers.max}
                  step={1}
                  onChange={(e) => setWorkerMin(Number((e.target as unknown as HTMLInputElement).value))}
                />
              </FlexBox>
              <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
                <Label for="sc-wkr-max" required>
                  Max nodes
                </Label>
                <StepInput
                  id="sc-wkr-max"
                  value={workerMax}
                  min={config.workers.min}
                  max={config.workers.max}
                  step={1}
                  valueState={workerMax < workerMin ? 'Negative' : 'None'}
                  onChange={(e) => setWorkerMax(Number((e.target as unknown as HTMLInputElement).value))}
                />
              </FlexBox>
            </>
          ) : (
            <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
              <Label for="sc-wkr-nodes" required>
                Nodes
              </Label>
              <StepInput
                id="sc-wkr-nodes"
                value={workerNodes}
                min={config.workers.min}
                max={config.workers.max}
                step={1}
                onChange={(e) => setWorkerNodes(Number((e.target as unknown as HTMLInputElement).value))}
              />
            </FlexBox>
          )}
          <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
            <Label for="sc-wkr-cpu" required>
              CPU / node
            </Label>
            <Select id="sc-wkr-cpu" onChange={(e) => setWorkerCpu(selectValue(e))}>
              {config.cpu.map((v) => (
                <Option key={v} value={v} selected={v === workerCpu}>
                  {v}
                </Option>
              ))}
            </Select>
          </FlexBox>
          <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
            <Label for="sc-wkr-mem" required>
              Memory / node
            </Label>
            <Select id="sc-wkr-mem" onChange={(e) => setWorkerMemory(selectValue(e))}>
              {config.memory.map((v) => (
                <Option key={v} value={v} selected={v === workerMemory}>
                  {v}
                </Option>
              ))}
            </Select>
          </FlexBox>
        </FlexBox>
      </FlexBox>
    </Dialog>
  );
};
