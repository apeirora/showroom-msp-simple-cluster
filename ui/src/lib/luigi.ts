/*
 * Luigi bootstrap. Pulls graphqlUrl + token + workspacePath from the portal
 * context, with a dev-mode mock so the SPA runs offline.
 */

export type ConfigSource = 'pending' | 'luigi' | 'mock';

export interface RuntimeContext {
  source: ConfigSource;
  graphqlUrl: string | null;
  token: string | null;
  workspacePath: string | null;
  namespace: string | null;
  clusterName: string | null;
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
    context?.kcpPath ?? context?.portalContext?.workspacePath ?? context?.workspacePath
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
  normaliseString(context?.kcpPath ?? context?.portalContext?.kcpPath) ??
  normaliseString(context?.workspacePath ?? context?.portalContext?.workspacePath) ??
  workspaceFromUrl(graphqlUrl);

const resolveNamespace = (context: any, searchParams?: any): string | null => {
  const namespace = normaliseString(searchParams?.namespace ?? context?.namespaceId);
  return namespace === '-all-' ? null : namespace;
};

export const extractRuntimeContext = (
  context: any,
  source: ConfigSource,
  pathParams?: any,
  searchParams?: any
): RuntimeContext => {
  const graphqlUrl = resolveGraphqlUrl(context);
  return {
    source,
    graphqlUrl,
    token: normaliseString(context?.token ?? context?.portalContext?.token),
    workspacePath: resolveWorkspacePath(context, graphqlUrl),
    namespace: resolveNamespace(context, searchParams),
    clusterName: normaliseString(pathParams?.clusterName ?? context?.clusterName)
  };
};

export const emptyRuntimeContext = (): RuntimeContext => ({
  source: 'pending',
  graphqlUrl: null,
  token: null,
  workspacePath: null,
  namespace: null,
  clusterName: null
});

interface LuigiClientLike {
  addInitListener: (cb: (context: any) => void) => string | number;
  removeInitListener?: (id: string | number) => void;
  addContextUpdateListener: (cb: (context: any) => void) => string | number;
  removeContextUpdateListener?: (id: string | number) => void;
  getPathParams?: () => any;
  getCoreSearchParams?: () => any;
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
    namespaceId: mockNamespace,
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
    const params = () => {
      try {
        return c.getPathParams?.() ?? null;
      } catch {
        return null;
      }
    };
    initId = c.addInitListener((context) => {
      onContext(extractRuntimeContext(context, source, params(), c.getCoreSearchParams?.()));
    });
    updateId = c.addContextUpdateListener((context) => {
      onContext(extractRuntimeContext(context, source, params(), c.getCoreSearchParams?.()));
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
      console.warn('[simple-cluster-ui] Luigi client unavailable', err);
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

export const navigateToCluster = async (name: string | null, namespace: string | null, signal?: AbortSignal): Promise<void> => {
  signal?.throwIfAborted();
  const client = await import('@luigi-project/client');
  const route = await client.linkManager().getCurrentRoute();
  signal?.throwIfAborted();
  const url = new URL(route, window.location.origin);
  const segment = '/showroom_apeirora_eu_simpleclusters';
  const index = url.pathname.lastIndexOf(segment);
  if (index < 0) throw new Error('The portal provider route is unavailable.');
  url.pathname = url.pathname.slice(0, index + segment.length) +
    (name ? `/${encodeURIComponent(name)}` : '');
  url.searchParams.set('namespace', namespace ?? '-all-');
  client.linkManager().navigate(`${url.pathname}${url.search}`, undefined, true);
};
