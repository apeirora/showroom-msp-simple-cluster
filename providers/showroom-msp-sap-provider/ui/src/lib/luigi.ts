/*
 * Luigi bootstrap. Pulls graphqlUrl + token + workspacePath from the portal
 * context, with a dev-mode mock so the SPA runs offline.
 *
 * Copied faithfully from showroom-msp-simple-cluster/ui — keep in sync.
 */

export type ConfigSource = 'pending' | 'luigi' | 'mock';

export interface RuntimeContext {
  source: ConfigSource;
  graphqlUrl: string | null;
  token: string | null;
  workspacePath: string | null;
  namespace: string | null;
}

const normaliseString = (value: unknown): string | null => {
  if (typeof value !== 'string') return null;
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : null;
};

const absolutizeAgainstPortal = (url: string): string => {
  if (/^https?:\/\//i.test(url)) return url;
  const base =
    (typeof document !== 'undefined' && document.referrer) ||
    (typeof window !== 'undefined' ? window.location.href : '');
  if (!base) return url;
  try {
    return new URL(url, base).toString();
  } catch {
    return url;
  }
};

const substituteLuigiPlaceholders = (url: string, context: any): string => {
  const accountPath = normaliseString(
    context?.portalContext?.workspacePath ?? context?.workspacePath
  );
  const account = normaliseString(context?.accountId ?? context?.portalContext?.accountId);
  const namespace = normaliseString(
    context?.namespaceId ?? context?.namespace ?? context?.portalContext?.namespace
  );
  let out = url;
  if (accountPath) out = out.replace(':accountId', accountPath);
  else if (account) out = out.replace(':accountId', account);
  if (namespace) out = out.replace(':namespaceId', namespace);
  return out;
};

const resolveGraphqlUrl = (context: any): string | null => {
  const candidates = [
    context?.portalContext?.crdGatewayApiUrl,
    context?.crdGatewayApiUrl,
    context?.portalContext?.graphqlUrl,
    context?.graphqlUrl
  ];
  for (const c of candidates) {
    const v = normaliseString(c);
    if (v) return absolutizeAgainstPortal(substituteLuigiPlaceholders(v, context));
  }
  return null;
};

const workspaceFromUrl = (url: string | null): string | null => {
  if (!url) return null;
  const m = url.match(/\/(?:kubernetes-graphql-gateway|gateway\/api\/clusters)\/([^/]+)\/graphql/);
  if (!m) return null;
  const path = m[1];
  return path.startsWith(':') ? null : path;
};

const resolveWorkspacePath = (context: any, graphqlUrl: string | null): string | null =>
  workspaceFromUrl(graphqlUrl) ??
  normaliseString(
    context?.portalContext?.workspacePath ?? context?.workspacePath ?? context?.accountId
  );

const resolveNamespace = (context: any): string | null =>
  normaliseString(
    context?.namespaceId ??
      context?.namespace ??
      context?.portalContext?.namespace ??
      context?.portalContext?.entityContext?.core_platform_mesh_io_account?.namespace
  );

export const extractRuntimeContext = (context: any, source: ConfigSource): RuntimeContext => {
  const graphqlUrl = resolveGraphqlUrl(context);
  return {
    source,
    graphqlUrl,
    token: normaliseString(context?.token ?? context?.portalContext?.token),
    workspacePath: resolveWorkspacePath(context, graphqlUrl),
    namespace: resolveNamespace(context)
  };
};

export const emptyRuntimeContext = (): RuntimeContext => ({
  source: 'pending',
  graphqlUrl: null,
  token: null,
  workspacePath: null,
  namespace: null
});

interface LuigiClientLike {
  addInitListener: (cb: (context: any) => void) => string | number;
  removeInitListener?: (id: string | number) => void;
  addContextUpdateListener: (cb: (context: any) => void) => string | number;
  removeContextUpdateListener?: (id: string | number) => void;
  __isMock?: boolean;
}

const installDevMock = () => {
  if (typeof window === 'undefined') return;
  const env = (import.meta as any).env ?? {};
  const shouldMock = Boolean(env.DEV) || env.VITE_ENABLE_LUIGI_MOCK === 'true';
  if (!shouldMock) return;
  const globalAny = window as any;
  if (globalAny.LuigiClient) return;

  const mockWorkspace = env.VITE_LUIGI_MOCK_WORKSPACE || 'root:orgs:showroom:ig-9';
  const mockNamespace = env.VITE_LUIGI_MOCK_NAMESPACE || 'default';
  const mockGraphql = env.VITE_LUIGI_MOCK_GRAPHQL_URL || '/graphql';
  const mockToken = env.VITE_LUIGI_MOCK_TOKEN ?? null;

  const context = {
    token: mockToken,
    workspacePath: mockWorkspace,
    namespace: mockNamespace,
    portalContext: {
      graphqlUrl: mockGraphql,
      workspacePath: mockWorkspace,
      namespace: mockNamespace
    }
  };

  const client: LuigiClientLike = {
    __isMock: true,
    addInitListener(cb) {
      queueMicrotask(() => cb(context));
      return 'mock-init';
    },
    removeInitListener() {},
    addContextUpdateListener() {
      return 'mock-update';
    },
    removeContextUpdateListener() {}
  };

  globalAny.LuigiClient = client;
};

export const subscribeToLuigi = (
  onContext: (ctx: RuntimeContext) => void
): (() => void) => {
  installDevMock();

  let cancelled = false;
  let client: LuigiClientLike | null = null;
  let initId: string | number | null = null;
  let updateId: string | number | null = null;

  const attach = (c: LuigiClientLike) => {
    if (cancelled || !c) return;
    client = c;
    const source: ConfigSource = c.__isMock ? 'mock' : 'luigi';
    initId = c.addInitListener((context) => {
      onContext(extractRuntimeContext(context, source));
    });
    updateId = c.addContextUpdateListener((context) => {
      onContext(extractRuntimeContext(context, source));
    });
  };

  const boot = async () => {
    const globalAny = typeof window !== 'undefined' ? (window as any) : undefined;
    if (globalAny?.LuigiClient) {
      attach(globalAny.LuigiClient);
      return;
    }
    try {
      const mod: any = await import('@luigi-project/client');
      const resolved: LuigiClientLike = mod?.default ?? mod;
      if (globalAny && !globalAny.LuigiClient) {
        globalAny.LuigiClient = resolved;
      }
      attach(resolved);
    } catch (err) {
      console.warn('[sapprovider-ui] Luigi client unavailable', err);
    }
  };

  void boot();

  return () => {
    cancelled = true;
    if (client) {
      if (initId != null && client.removeInitListener) client.removeInitListener(initId);
      if (updateId != null && client.removeContextUpdateListener) client.removeContextUpdateListener(updateId);
    }
  };
};
