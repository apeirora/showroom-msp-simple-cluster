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
      <ShellBar primaryTitle="SAP Gardener" secondaryTitle="Gardener Shoots" />
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
            />
          </Tab>
          <Tab text="IronCore">
            <ProviderTab
              client={client}
              ready={ready}
              api={ironcoreShootApi}
              flavor="IronCore"
              defaultName="ironcore"
            />
          </Tab>
        </TabContainer>
      </div>
    </FlexBox>
  );
};

export default App;
