import { useEffect, useState } from 'react';
import {
  Bar,
  Button,
  Dialog,
  FlexBox,
  FlexBoxDirection,
  Input,
  Label,
  MessageStrip,
  TextArea
} from '@ui5/webcomponents-react';

export interface CreateValues {
  name: string;
  spec: string;
}

interface Props {
  open: boolean;
  busy: boolean;
  onClose: () => void;
  onSubmit: (values: CreateValues) => void;
}

// A DNS-1123 subdomain, the constraint Kubernetes object names must satisfy.
const NAME_RE = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

// Ready-to-use default Gardener Shoot manifest, mirroring the dati provider's
// built-in template with its input defaults resolved. Editable; clearing it
// falls back to the operator's built-in default.
const DEFAULT_SPEC = `apiVersion: core.gardener.cloud/v1beta1
kind: Shoot
spec:
  cloudProfile:
    name: openstack
    kind: CloudProfile
  credentialsBindingName: my-openstack-secret
  region: RegionOne
  kubernetes:
    version: "1.33.9"
  networking:
    type: calico
    nodes: 10.250.0.0/16
    pods: 100.128.0.0/11
    services: 100.72.0.0/13
  provider:
    type: openstack
    infrastructureConfig:
      apiVersion: openstack.provider.extensions.gardener.cloud/v1alpha1
      kind: InfrastructureConfig
      floatingPoolName: external
      networks:
        workers: 10.250.0.0/19
    controlPlaneConfig:
      apiVersion: openstack.provider.extensions.gardener.cloud/v1alpha1
      kind: ControlPlaneConfig
      loadBalancerProvider: amphora
    workers:
      - name: worker-main
        machine:
          type: 4vCPU-8192RAM-100STORAGE
          image:
            name: gardenlinux
            version: "2150.0.0"
        minimum: 1
        maximum: 1
        zones:
          - nova
  maintenance:
    autoUpdate:
      kubernetesVersion: false
      machineImageVersion: false
    timeWindow:
      begin: "220000+0000"
      end: "230000+0000"
  hibernation:
    enabled: false
`;

export const CreateShootDialog = ({ open, busy, onClose, onSubmit }: Props) => {
  const [name, setName] = useState('dati');
  const [spec, setSpec] = useState(DEFAULT_SPEC);

  useEffect(() => {
    if (open) {
      setName('dati');
      setSpec(DEFAULT_SPEC);
    }
  }, [open]);

  const nameValid = NAME_RE.test(name);
  const canSubmit = nameValid && !busy;

  return (
    <Dialog
      open={open}
      headerText="New Shoot"
      onClose={onClose}
      style={{ width: '640px', maxWidth: '95vw' }}
      footer={
        <Bar
          endContent={
            <>
              <Button design="Transparent" onClick={onClose}>
                Cancel
              </Button>
              <Button
                design="Emphasized"
                disabled={!canSubmit}
                onClick={() => onSubmit({ name: name.trim(), spec })}
              >
                {busy ? 'Creating…' : 'Create'}
              </Button>
            </>
          }
        />
      }
    >
      <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '1rem', padding: '0.5rem 0' }}>
        <MessageStrip design="Information" hideCloseButton>
          A Shoot is an optional Gardener resource provided by dati. The manifest below is pre-filled
          with a ready-to-use default; edit it as needed, or clear it to fall back to the operator's
          built-in default template.
        </MessageStrip>

        <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.35rem' }}>
          <Label required>Name</Label>
          <Input
            value={name}
            onInput={(e) => setName((e.target as unknown as HTMLInputElement).value)}
            valueState={name.length === 0 || nameValid ? 'None' : 'Negative'}
            placeholder="dati"
          />
          {!nameValid && name.length > 0 && (
            <span style={{ fontSize: '0.75rem', color: 'var(--sapNegativeColor)' }}>
              Lowercase letters, digits and dashes only (DNS-1123).
            </span>
          )}
        </FlexBox>

        <FlexBox direction={FlexBoxDirection.Column} style={{ gap: '0.35rem' }}>
          <Label>Gardener Shoot manifest</Label>
          <TextArea
            value={spec}
            rows={12}
            growing={false}
            placeholder="# Leave empty to use the built-in dati template"
            onInput={(e) => setSpec((e.target as unknown as HTMLTextAreaElement).value)}
            style={{ fontFamily: 'var(--sapCode_FontFamily, monospace)', width: '100%' }}
          />
        </FlexBox>
      </FlexBox>
    </Dialog>
  );
};
