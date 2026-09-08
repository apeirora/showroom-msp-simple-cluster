import { expect, it, vi } from 'vitest';
import { createSimpleCluster, type CreateSimpleClusterInput } from '../src/lib/simplecluster';
import { standardSources } from './providerFixture';

it('sends ordinary autoscaling through the real GraphQL request without a control plane or provider inputs', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: {
    showroom_apeirora_eu: { v1alpha1: { createSimpleCluster: { metadata: { labels: {} } } } }
  } }), { status: 200 }));
  await createSimpleCluster({ graphqlUrl: 'https://gateway.test/root/graphql', token: null }, {
    namespace: 'default', name: 'example', provider: 'telekom', workerNodes: 1,
    workerAutoscaling: true, workerMin: 2, workerMax: 4, workerCpu: '2', workerMemory: '4Gi'
  });
  const request = JSON.parse(fetch.mock.calls[0][1]!.body as string);
  expect(request.variables.object.spec).toEqual({
    providerConfigRefs: ['telekom'],
    workers: { autoscaling: { min: 2, max: 4 }, requested: { cpu: '"2"', memory: '"4Gi"' } }
  });
});

it.each(standardSources)('preserves the standard fixed worker and control-plane request for $providerConfigName', async (source) => {
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: {} }), { status: 200 }));
  await createSimpleCluster({ graphqlUrl: 'https://gateway.test/root/graphql', token: null }, {
    namespace: 'default', name: 'my-cluster', provider: source.providerConfigName!, workerNodes: 2,
    workerCpu: '1', workerMemory: '2Gi', controlPlaneNodes: 1
  });
  const spec = JSON.parse(fetch.mock.calls[0][1]!.body as string).variables.object.spec;
  expect(spec).toEqual({
    providerConfigRefs: [source.providerConfigName],
    workers: { nodes: 2, requested: { cpu: '"1"', memory: '"2Gi"' } },
    controlPlane: { nodes: 1, requested: { cpu: '"1"', memory: '"2Gi"' } }
  });
});

it('does not forward obsolete provider inputs supplied by an old caller', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: {} }), { status: 200 }));
  const oldInput = {
    namespace: 'default', name: 'my-cluster', provider: 'telekom', workerNodes: 2,
    workerCpu: '2', workerMemory: '4Gi', inputs: { region: 'mdb', machineType: 'unsupported' }
  };
  await createSimpleCluster({ graphqlUrl: 'https://gateway.test/root/graphql', token: null }, oldInput as CreateSimpleClusterInput);
  const spec = JSON.parse(fetch.mock.calls[0][1]!.body as string).variables.object.spec;
  expect(spec).not.toHaveProperty('inputs');
  expect(spec.workers).toEqual({ nodes: 2, requested: { cpu: '"2"', memory: '"4Gi"' } });
});
