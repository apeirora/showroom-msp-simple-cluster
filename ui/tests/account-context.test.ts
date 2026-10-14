import { afterEach, describe, expect, it, vi } from 'vitest';
import { extractRuntimeContext, navigateToCluster } from '../src/lib/luigi';
import { graphqlFetch } from '../src/lib/graphql';
import { listSimpleClusters } from '../src/lib/simplecluster';

const navigation = vi.hoisted(() => ({
  getCurrentRoute: vi.fn(), navigate: vi.fn()
}));
vi.mock('@luigi-project/client', () => ({ linkManager: () => navigation }));
afterEach(() => vi.restoreAllMocks());

const gateway = 'https://portal.test/gateway/api/clusters/root:orgs:showroom/graphql';

describe('Account context', () => {
  it('uses kcpPath before a stale gateway workspace', () => {
    const context = extractRuntimeContext({
      kcpPath: 'root:orgs:showroom:account-b', token: 'test-token',
      portalContext: { crdGatewayApiUrl: gateway }
    }, 'luigi', {}, { namespace: '-all-' });
    expect(context.workspacePath).toBe('root:orgs:showroom:account-b');
    expect(context.namespace).toBeNull();
  });

  it('reads native namespace search parameters and the detail route', () => {
    const context = extractRuntimeContext({ kcpPath: 'root:orgs:showroom:a' },
      'luigi', { clusterName: 'same-name' }, { namespace: 'team-b' });
    expect(context.namespace).toBe('team-b');
    expect(context.clusterName).toBe('same-name');
  });

  it('does not treat an account display name as a workspace path', () => {
    expect(extractRuntimeContext({ accountId: 'a' }, 'luigi').workspacePath).toBeNull();
  });

  it('requests only the new account workspace with an all-namespace query', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
      data: { showroom_apeirora_eu: { v1alpha1: { SimpleClusters: { items: [] } } } }
    })));
    await listSimpleClusters({ graphqlUrl: gateway, workspacePath: 'root:orgs:showroom:b', token: null }, null);
    expect(fetch.mock.calls[0][0]).toBe('https://portal.test/gateway/api/clusters/root:orgs:showroom:b/graphql');
    const body = JSON.parse(fetch.mock.calls[0][1]?.body as string);
    expect(body.query).toContain('$ns: String)');
    expect(body.variables.ns).toBeNull();
  });

  it('rejects an old account request before transport starts', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch');
    const controller = new AbortController();
    controller.abort();
    await expect(graphqlFetch({ graphqlUrl: gateway, token: null, signal: controller.signal },
      { query: '{}' })).rejects.toThrow();
    expect(fetch).not.toHaveBeenCalled();
  });

  it('creates a raw namespace deep link and removes the old resource child', async () => {
    navigation.getCurrentRoute.mockResolvedValue('/home/accounts/a/showroom_apeirora_eu_simpleclusters/old?namespace=team-a');
    await navigateToCluster('same-name', 'team-b');
    expect(navigation.navigate).toHaveBeenCalledWith(
      '/home/accounts/a/showroom_apeirora_eu_simpleclusters/same-name?namespace=team-b');
    await navigateToCluster(null, null);
    expect(navigation.navigate).toHaveBeenLastCalledWith(
      '/home/accounts/a/showroom_apeirora_eu_simpleclusters?namespace=-all-');
  });

  it('cancels navigation when the account changes during route lookup', async () => {
    const controller = new AbortController();
    navigation.navigate.mockClear();
    navigation.getCurrentRoute.mockImplementation(async () => {
      controller.abort();
      return '/home/accounts/b/showroom_apeirora_eu_simpleclusters';
    });
    await expect(navigateToCluster('old-resource', 'team-a', controller.signal)).rejects.toThrow();
    expect(navigation.navigate).not.toHaveBeenCalled();
  });
});
