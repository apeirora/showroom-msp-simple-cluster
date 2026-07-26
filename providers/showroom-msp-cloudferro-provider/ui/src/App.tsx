import { useEffect, useMemo, useState } from 'react';
import {
  BusyIndicator,
  FlexBox,
  FlexBoxAlignItems,
  FlexBoxDirection,
  FlexBoxJustifyContent,
  IllustratedMessage,
  ShellBar
} from '@ui5/webcomponents-react';
import '@ui5/webcomponents-fiori/dist/illustrations/UnableToLoad.js';

import { ProviderView } from './components/ProviderView';
import { emptyRuntimeContext, subscribeToLuigi, type RuntimeContext } from './lib/luigi';
import type { GraphqlClientOptions } from './lib/graphql';

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

  return (
    <FlexBox direction={FlexBoxDirection.Column} style={{ height: '100vh' }}>
      <ShellBar primaryTitle="CloudFerro" secondaryTitle="OpenStack VMs" />
      <div style={{ flex: 1, overflow: 'auto', background: 'var(--sapBackgroundColor)' }}>
        <ProviderView
          client={client}
          namespace={ctx.namespace}
          ready={Boolean(ctx.graphqlUrl && ctx.namespace)}
        />
      </div>
    </FlexBox>
  );
};

export default App;
