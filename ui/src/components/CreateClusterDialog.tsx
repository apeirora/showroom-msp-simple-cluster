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
import type { AddonCatalogEntry, AddonEntitlement, ClusterConfig, KeyStoreInfo } from '../types';
import { keyRotationPeriodValid } from '../lib/duration';
import { keyStoreSummary } from '../lib/encryption';

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
  /**
   * Encryption at rest, only when the user ticked it for a provider that
   * offers it. Absent otherwise, so the created object carries no
   * spec.encryption and provisions exactly as before.
   */
  encryption?: { provider: string; keyRotationPeriod?: string };
}

interface Props {
  open: boolean;
  busy: boolean;
  config: ClusterConfig;
  // Live provider list (discovered from installed provider CRs when dynamic
  // discovery is enabled, otherwise the static config list).
  providers: string[];
  providersLoading: boolean;
  // Key management providers per provider config name (see
  // ClusterConfig.encryptionProviders). Decides whether the encryption
  // control is shown for the selected provider.
  encryptionProviders: Record<string, string[]>;
  // Where each key management provider type keeps its keys (see
  // ClusterConfig.encryptionKeyStores). Display only.
  encryptionKeyStores: Record<string, KeyStoreInfo>;
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
  encryptionProviders,
  encryptionKeyStores,
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

  // Keep the selection valid as the discovered provider list arrives or
  // changes (and whenever the dialog reopens).
  useEffect(() => {
    if (!open) return;
    if (!providers.includes(provider)) {
      setProvider(providers[0] ?? '');
    }
  }, [open, providers, provider]);
  // Encryption at rest is offered only for a provider config that admits a
  // key management provider. The selection follows the provider: switching to
  // one without the option drops the request, so what is submitted is always
  // consistent with what is on screen.
  const offeredEncryption = useMemo(
    () =>
      (Object.prototype.hasOwnProperty.call(encryptionProviders, provider)
        ? encryptionProviders[provider]
        : []
      ).filter(
        (t) => typeof t === 'string' && t.trim() !== ''
      ),
    [encryptionProviders, provider]
  );
  const [encrypt, setEncrypt] = useState(false);
  const [encryptionType, setEncryptionType] = useState('');
  const [keyRotationPeriod, setKeyRotationPeriod] = useState('');
  const [keyRotationTouched, setKeyRotationTouched] = useState(false);
  useEffect(() => {
    if (offeredEncryption.length === 0) {
      setEncrypt(false);
      setEncryptionType('');
      return;
    }
    if (!offeredEncryption.includes(encryptionType)) setEncryptionType(offeredEncryption[0]);
  }, [offeredEncryption, encryptionType]);
  // The type whose key store is described: the chosen one once the box is
  // ticked, otherwise the first offered, so the answer to "where would the
  // keys be?" is on screen before the user commits to anything.
  const describedType = offeredEncryption.includes(encryptionType)
    ? encryptionType
    : offeredEncryption[0] ?? '';
  const keyStore = Object.prototype.hasOwnProperty.call(encryptionKeyStores, describedType)
    ? encryptionKeyStores[describedType]
    : undefined;
  const keyRotationValid = keyRotationPeriodValid(keyRotationPeriod);
  const encryptionValid =
    !encrypt || (offeredEncryption.includes(encryptionType) && keyRotationValid);

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
  const workersValid = workerAutoscaling ? workerMin >= 1 && workerMax >= workerMin : workerNodes >= 1;
  const noProviders = !providersLoading && providers.length === 0;
  const canSubmit =
    nameValid &&
    Boolean(provider) &&
    !providersLoading &&
    workersValid &&
    Boolean(workerCpu) &&
    Boolean(workerMemory) &&
    controlPlaneNodes >= 1 &&
    Boolean(controlPlaneCpu) &&
    Boolean(controlPlaneMemory) &&
    encryptionValid;

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
    setEncrypt(false);
    setEncryptionType('');
    setKeyRotationPeriod('');
    setKeyRotationTouched(false);
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
    if (!canSubmit) return;
    // Sent only when ticked for a provider that offers it: a stale tick from a
    // previously selected provider must not travel with the request.
    const period = keyRotationPeriod.trim();
    const encryption =
      encrypt && offeredEncryption.includes(encryptionType)
        ? { provider: encryptionType, ...(period ? { keyRotationPeriod: period } : {}) }
        : undefined;
    onSubmit({
      name,
      provider,
      labels: addonLabels,
      ...(encryption ? { encryption } : {}),
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
                  {p}
                </Option>
              ))}
            </Select>
          )}
        </FlexBox>

        {/*
          * Only for a provider config that admits a key management provider.
          * Nothing is shown otherwise: the operator would refuse the request, so
          * offering a box that cannot work helps nobody.
          */}
        {offeredEncryption.length > 0 && (
          <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
            <Label>Encryption at rest</Label>
            <CheckBox
              id="sc-encrypt"
              text={
                offeredEncryption.length === 1
                  ? `Encrypt Kubernetes Secrets with ${offeredEncryption[0]} (KMS)`
                  : 'Encrypt Kubernetes Secrets with an external key management provider (KMS)'
              }
              checked={encrypt}
              onChange={(e) => setEncrypt(Boolean((e.target as unknown as HTMLInputElement).checked))}
            />
            {encrypt && offeredEncryption.length > 1 && (
              <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
                <Label for="sc-encrypt-provider" required>
                  Key management provider
                </Label>
                <Select id="sc-encrypt-provider" onChange={(e) => setEncryptionType(selectValue(e))}>
                  {offeredEncryption.map((t) => (
                    <Option key={t} value={t} selected={t === encryptionType}>
                      {t}
                    </Option>
                  ))}
                </Select>
              </FlexBox>
            )}
            {encrypt && (
              <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
                <Label for="sc-key-rotation">Key rotation period (optional)</Label>
                <Input
                  id="sc-key-rotation"
                  value={keyRotationPeriod}
                  placeholder="e.g. 720h — empty means no automatic rotation"
                  valueState={keyRotationTouched && !keyRotationValid ? 'Negative' : 'None'}
                  onInput={(e) => {
                    setKeyRotationPeriod(String((e.target as unknown as HTMLInputElement).value ?? ''));
                    setKeyRotationTouched(true);
                  }}
                />
                {keyRotationTouched && !keyRotationValid && (
                  <span style={{ color: 'var(--sapNegativeColor)', fontSize: '0.75rem' }}>
                    Use a duration of at least 1h, such as 24h, 720h or 1h30m.
                  </span>
                )}
              </FlexBox>
            )}
            {/*
              * Read-only: the key store is a platform decision, not a per-cluster
              * choice, but a bare checkbox leaves "where are the keys stored?"
              * unanswered. Shown whether or not the box is ticked.
              */}
            {describedType && (
              <FlexBox direction={FlexBoxDirection.Column} style={{ gap: 4 }}>
                <Label for="sc-key-store">Key storage</Label>
                <Input id="sc-key-store" value={keyStoreSummary(describedType, keyStore)} readonly />
                {keyStore?.keys && (
                  <span style={{ fontSize: '0.75rem', color: 'var(--sapContent_LabelColor)' }}>
                    {keyStore.keys}
                  </span>
                )}
              </FlexBox>
            )}
            <span style={{ fontSize: '0.75rem', color: 'var(--sapContent_LabelColor)' }}>
              Secrets are encrypted from the first write with a key the platform's key
              management holds for this cluster. Cannot be switched on or off after the
              cluster is created.
            </span>
          </FlexBox>
        )}

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
