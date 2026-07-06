import { useMemo, useState } from 'react';
import {
  Bar,
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
import type { ClusterConfig } from '../types';

const RFC_1123 = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/;

export interface CreateValues {
  name: string;
  provider: string;
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
  onClose: () => void;
  onSubmit: (values: CreateValues) => void;
}

const selectValue = (e: { detail: { selectedOption: unknown } }): string =>
  String((e.detail.selectedOption as unknown as HTMLElement).getAttribute('value') ?? '');

export const CreateClusterDialog = ({ open, busy, config, onClose, onSubmit }: Props) => {
  const [name, setName] = useState('');
  const [provider, setProvider] = useState(config.providers[0] ?? '');
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
  const canSubmit =
    nameValid &&
    Boolean(provider) &&
    workersValid &&
    Boolean(workerCpu) &&
    Boolean(workerMemory) &&
    controlPlaneNodes >= 1 &&
    Boolean(controlPlaneCpu) &&
    Boolean(controlPlaneMemory);

  const reset = () => {
    setName('');
    setNameTouched(false);
    setProvider(config.providers[0] ?? '');
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
    onSubmit({
      name,
      provider,
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
          <Select id="sc-provider" onChange={(e) => setProvider(selectValue(e))}>
            {config.providers.map((p) => (
              <Option key={p} value={p} selected={p === provider}>
                {p}
              </Option>
            ))}
          </Select>
        </FlexBox>

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
