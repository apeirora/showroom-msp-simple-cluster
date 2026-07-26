import { useEffect, useMemo, useState } from 'react';
import {
  BusyIndicator,
  FlexBox,
  FlexBoxAlignItems,
  FlexBoxDirection,
  FlexBoxJustifyContent,
  IllustratedMessage,
  ShellBar,
  Tab,
  TabContainer
} from '@ui5/webcomponents-react';
import '@ui5/webcomponents-fiori/dist/illustrations/UnableToLoad.js';

import { ProviderTab } from './components/ProviderTab';
import {
  emptyRuntimeContext,
  subscribeToLuigi,
  type RuntimeContext
} from './lib/luigi';
import type { GraphqlClientOptions } from './lib/graphql';
import { ironcoreShootApi, openstackShootApi } from './lib/shoot';

// Ready-to-use default Gardener Shoot manifests, one per flavor, mirroring the
// operator's embedded templates with their input defaults resolved (smallest
// profile machine type, single-node worker pool). Editable; clearing the
// textarea falls back to the operator's built-in default.
const OPENSTACK_DEFAULT_SPEC = `apiVersion: core.gardener.cloud/v1beta1
kind: Shoot
spec:
  cloudProfile:
    name: converged-cloud-ora-fp
    kind: CloudProfile
  secretBindingName: cc-eu-de-1
  region: eu-de-1
  kubernetes:
    version: "1.33.9"
  addons:
    nginxIngress:
      enabled: true
      loadBalancerSourceRanges:
        - 0.0.0.0/0
  provider:
    type: openstack
    infrastructureConfig:
      apiVersion: openstack.provider.extensions.gardener.cloud/v1alpha1
      kind: InfrastructureConfig
      floatingPoolName: FloatingIP-external-ora-01
      networks:
        workers: 10.250.0.0/16
    controlPlaneConfig:
      apiVersion: openstack.provider.extensions.gardener.cloud/v1alpha1
      kind: ControlPlaneConfig
      loadBalancerProvider: f5
    workers:
      - name: worker-main
        minimum: 1
        maximum: 1
        machine:
          type: g_c2_m4
          image:
            name: gardenlinux
        zones:
          - eu-de-1a
  networking:
    type: calico
    nodes: 10.250.0.0/16
    pods: 100.64.0.0/12
    services: 100.104.0.0/13
`;

const IRONCORE_DEFAULT_SPEC = `apiVersion: core.gardener.cloud/v1beta1
kind: Shoot
spec:
  cloudProfile:
    name: ironcore
    kind: CloudProfile
  credentialsBindingName: my-ironcore-secret
  region: wdf4
  kubernetes:
    version: "1.34.5"
  addons:
    nginxIngress:
      enabled: true
      loadBalancerSourceRanges:
        - 0.0.0.0/0
  provider:
    type: ironcore
    workers:
      - name: worker-main
        minimum: 1
        maximum: 1
        machine:
          type: memory-intel1-1cpu
          image:
            name: gardenlinux
        zones:
          - wdf4a
  networking:
    type: calico
    nodes: 10.250.0.0/16
    pods: 100.64.0.0/12
    services: 100.104.0.0/13
`;

const App = () => {
  const [ctx, setCtx] = useState<RuntimeContext>(emptyRuntimeContext);

  useEffect(() => subscribeToLuigi(setCtx), []);

  const client: GraphqlClientOptions = useMemo(
    () => ({
      graphqlUrl: ctx.graphqlUrl ?? '',
      token: ctx.token,
      workspacePath: ctx.workspacePath
    }),
    [ctx.graphqlUrl, ctx.token, ctx.workspacePath]
  );

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

  const ready = Boolean(ctx.graphqlUrl);

  return (
    <FlexBox direction={FlexBoxDirection.Column} style={{ height: '100vh' }}>
      <ShellBar primaryTitle="SAP Shoots" secondaryTitle="Gardener Shoots" />
      <div
        style={{
          flex: 1,
          overflow: 'auto',
          background: 'var(--sapBackgroundColor)'
        }}
      >
        <TabContainer contentBackgroundDesign="Transparent">
          <Tab text="OpenStack" selected>
            <ProviderTab
              client={client}
              ready={ready}
              api={openstackShootApi}
              flavor="OpenStack"
              defaultName="openstack"
              defaultSpec={OPENSTACK_DEFAULT_SPEC}
            />
          </Tab>
          <Tab text="IronCore">
            <ProviderTab
              client={client}
              ready={ready}
              api={ironcoreShootApi}
              flavor="IronCore"
              defaultName="ironcore"
              defaultSpec={IRONCORE_DEFAULT_SPEC}
            />
          </Tab>
        </TabContainer>
      </div>
    </FlexBox>
  );
};

export default App;
