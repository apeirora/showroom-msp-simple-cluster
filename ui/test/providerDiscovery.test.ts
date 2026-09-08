import { expect, it, vi } from 'vitest';
import { fetchAvailableProviders } from '../src/lib/providers';
import { standardSources } from './providerFixture';

it.each(standardSources)('discovers $providerConfigName from its bound API without an existing resource', async (source) => {
  const group = source.group.replace(/[.-]/g, '_');
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
    data: { [group]: { [source.version]: { [source.listField]: { items: [] } } } }
  }), { status: 200 }));
  const result = await fetchAvailableProviders({ graphqlUrl: 'https://gateway.test/root/graphql', token: null },
    'account-namespace', [source]);
  expect(result).toEqual({ providers: [source.providerConfigName], ran: true });
  const request = JSON.parse(fetch.mock.calls[0][1]!.body as string);
  expect(request.query).toContain(source.namespaced ? `${source.listField}(namespace: $ns)` : `${source.listField} {`);
  expect(request.variables).toEqual(source.namespaced ? { ns: 'account-namespace' } : {});
  expect(request.query).not.toContain('status { providerConfigs }');
});

it('does not offer Telekom when its API query fails', async () => {
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
    errors: [{ message: 'Cannot query field gardener_t_systems_com' }]
  }), { status: 200 }));
  const result = await fetchAvailableProviders({ graphqlUrl: 'https://gateway.test/root/graphql', token: null },
    'default', [standardSources[5]]);
  expect(result).toEqual({ providers: [], ran: true });
});

it('reports discovery as inconclusive after a transport failure', async () => {
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Failed to fetch'));
  const result = await fetchAvailableProviders({ graphqlUrl: 'https://gateway.test/root/graphql', token: null },
    'default', standardSources);
  expect(result).toEqual({ providers: [], ran: false });
});
