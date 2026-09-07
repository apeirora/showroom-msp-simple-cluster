import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { listSimpleClusters } from '../src/lib/simplecluster';

const opts = { graphqlUrl: 'https://gateway.example.invalid/root/graphql', token: null };
const fetchMock = vi.fn<typeof fetch>();
const emptyList = () => new Response(JSON.stringify({
  data: { showroom_apeirora_eu: { v1alpha1: { SimpleClusters: { items: [] } } } }
}), { status: 200 });

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock);
  vi.stubEnv('VITE_FIXTURES', 'false');
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe('cluster list request cancellation', () => {
  it('forwards the caller signal through the real list and GraphQL helpers', async () => {
    fetchMock.mockResolvedValueOnce(emptyList());
    const controller = new AbortController();
    expect(await listSimpleClusters(opts, 'synthetic-namespace', controller.signal)).toEqual([]);
    expect(fetchMock).toHaveBeenCalledOnce();
    const [, init] = fetchMock.mock.calls[0];
    expect(init?.signal).toBe(controller.signal);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string).variables).toEqual({ ns: 'synthetic-namespace' });
  });

  it('propagates cancellation instead of returning an empty successful inventory', async () => {
    fetchMock.mockImplementationOnce((_url, init) => new Promise((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => reject(new DOMException('Synthetic abort', 'AbortError')), { once: true });
    }));
    const controller = new AbortController();
    const pending = listSimpleClusters(opts, 'synthetic-namespace', controller.signal);
    const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' });
    controller.abort();
    await rejected;
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it('preserves callers that do not provide a cancellation signal', async () => {
    fetchMock.mockResolvedValueOnce(emptyList());
    expect(await listSimpleClusters(opts, 'synthetic-namespace')).toEqual([]);
    expect(fetchMock.mock.calls[0][1]).not.toHaveProperty('signal');
  });
});
