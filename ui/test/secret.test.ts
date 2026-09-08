import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fetchKubeconfig } from '../src/lib/secret';

// Deliberately inert data: no cluster endpoint, user identity or credentials.
const syntheticConfig = 'apiVersion: v1\nkind: Config\nclusters: []\nusers: []\ncontexts: []\n';
const namespace = 'synthetic-namespace';
const secretName = 'synthetic-kubeconfig';
const opts = {
  graphqlUrl: 'https://gateway.example.invalid/kubernetes-graphql-gateway/root/graphql',
  workspacePath: 'root:orgs:synthetic:workspace',
  token: 'synthetic-test-token'
};
const fetchMock = vi.fn<typeof fetch>();
const respond = (secret: unknown) => {
  fetchMock.mockResolvedValue(new Response(JSON.stringify({ data: { v1: { Secret: secret } } }), { status: 200 }));
};

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock);
  vi.stubEnv('VITE_FIXTURES', 'false');
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe('named kubeconfig Secret fetch', () => {
  it('requests only the named Secret in the current namespace and workspace with the provided user auth', async () => {
    respond({ metadata: { name: secretName }, data: { kubeconfig: btoa(syntheticConfig) } });
    expect(await fetchKubeconfig(opts, namespace, secretName)).toBe(syntheticConfig);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://gateway.example.invalid/kubernetes-graphql-gateway/root:orgs:synthetic:workspace/graphql');
    expect(init?.method).toBe('POST');
    expect(init?.headers).toEqual({ 'Content-Type': 'application/json', Authorization: 'Bearer synthetic-test-token' });
    const body = JSON.parse(init?.body as string);
    expect(body.variables).toEqual({ ns: namespace, name: secretName });
    expect(body.query).toContain('query Secret($ns: String!, $name: String!)');
    expect(body.query).toContain('v1 {');
    expect(body.query).toContain('Secret(namespace: $ns, name: $name)');
    expect(body.query).not.toContain('Secrets(');
  });

  it.each(['kubeconfig', 'kubeconfig.yaml', 'value'])('decodes the supported %s data key', async (key) => {
    respond({ data: { [key]: btoa(syntheticConfig) } });
    expect(await fetchKubeconfig(opts, namespace, secretName)).toBe(syntheticConfig);
  });

  it('prefers kubeconfig over alternative data keys', async () => {
    respond({ data: { kubeconfig: btoa(syntheticConfig), 'kubeconfig.yaml': btoa('unused alternative'), value: btoa('unused value') } });
    expect(await fetchKubeconfig(opts, namespace, secretName)).toBe(syntheticConfig);
  });

  it('does not invent authorization when the current user token is absent', async () => {
    respond({ data: { kubeconfig: btoa(syntheticConfig) } });
    await fetchKubeconfig({ ...opts, token: null }, namespace, secretName);
    expect(fetchMock.mock.calls[0][1]?.headers).toEqual({ 'Content-Type': 'application/json' });
  });

  it('reports a null Secret as not found', async () => {
    respond(null);
    await expect(fetchKubeconfig(opts, namespace, secretName)).rejects.toThrow('Secret synthetic-namespace/synthetic-kubeconfig not found');
  });

  it.each([
    { label: 'absent data', secret: {} },
    { label: 'empty data', secret: { data: {} } },
    { label: 'unrelated data key', secret: { data: { unrelated: btoa('synthetic') } } },
    { label: 'empty kubeconfig', secret: { data: { kubeconfig: '' } } }
  ])('reports $label without returning content', async ({ secret }) => {
    respond(secret);
    await expect(fetchKubeconfig(opts, namespace, secretName)).rejects.toThrow("Secret synthetic-namespace/synthetic-kubeconfig has no 'kubeconfig' data key");
  });

  it('reports invalid base64 instead of returning undecoded content', async () => {
    respond({ data: { kubeconfig: '%%%invalid-base64%%%' } });
    await expect(fetchKubeconfig(opts, namespace, secretName)).rejects.toThrow('Failed to decode kubeconfig:');
  });

  it.each([401, 403, 404])('propagates HTTP %s without a second request', async (status) => {
    fetchMock.mockResolvedValue(new Response(null, { status }));
    await expect(fetchKubeconfig(opts, namespace, secretName)).rejects.toThrow(`GraphQL HTTP ${status}`);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('propagates a GraphQL authorization error even with a partial Secret response', async () => {
    fetchMock.mockResolvedValue(new Response(JSON.stringify({
      data: { v1: { Secret: null } }, errors: [{ message: 'synthetic Secret access denied' }]
    }), { status: 200 }));
    await expect(fetchKubeconfig(opts, namespace, secretName)).rejects.toThrow('synthetic Secret access denied');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('propagates network failure without converting it into Secret absence', async () => {
    const failure = new TypeError('synthetic network unavailable');
    fetchMock.mockRejectedValue(failure);
    await expect(fetchKubeconfig(opts, namespace, secretName)).rejects.toBe(failure);
  });
});
