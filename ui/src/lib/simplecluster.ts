/*
 * CRUD + subscriptions for SimpleCluster resources.
 * GroupVersionKind: showroom.apeirora.eu/v1alpha1 SimpleCluster
 * GraphQL root: showroom_apeirora_eu.v1alpha1.SimpleClusters
 */

import type { SimpleCluster } from '../types';
import { graphqlFetch, subscribe, type GraphqlClientOptions, type SubscriptionHandle } from './graphql';
import { fixtureSimpleClusters, fixturesEnabled } from './fixtures';

const CLUSTER_LIST_QUERY = `
  query SimpleClusters($ns: String!) {
    showroom_apeirora_eu {
      v1alpha1 {
        SimpleClusters(namespace: $ns) {
          items {
            metadata { name namespace creationTimestamp uid resourceVersion }
            spec {
              providerConfigRefs
              controlPlane { nodes requested { cpu memory } }
              workers { nodes autoscaling { min max } requested { cpu memory } }
            }
            status {
              phase
              cp
              wkr
              providerConfigRef { name }
              targetCluster
              kubeconfigSecretRef { name }
              kubeconfigExpiresAt
              kubeconfigIssuedAt
              observedGeneration
              resources { name phase message }
              conditions { type status reason message lastTransitionTime }
              nodes { name role externalIP internalIP ready }
            }
          }
        }
      }
    }
  }
`;

interface SimpleClusterListResponse {
  showroom_apeirora_eu: {
    v1alpha1: { SimpleClusters: { items: SimpleCluster[] } };
  };
}

export const listSimpleClusters = async (
  opts: GraphqlClientOptions,
  namespace: string
): Promise<SimpleCluster[]> => {
  if (fixturesEnabled()) return fixtureSimpleClusters();
  const data = await graphqlFetch<SimpleClusterListResponse>(opts, {
    query: CLUSTER_LIST_QUERY,
    variables: { ns: namespace }
  });
  return data.showroom_apeirora_eu?.v1alpha1?.SimpleClusters?.items ?? [];
};

export interface CreateSimpleClusterInput {
  namespace: string;
  name: string;
  provider: string;
  workerNodes: number;
  workerAutoscaling?: boolean;
  workerMin?: number;
  workerMax?: number;
  workerCpu: string;
  workerMemory: string;
  controlPlaneNodes?: number;
  controlPlaneCpu?: string;
  controlPlaneMemory?: string;
}

const CREATE_SIMPLECLUSTER_MUTATION = `
  mutation CreateSimpleCluster($ns: String!, $object: SimpleClusterInput!) {
    showroom_apeirora_eu {
      v1alpha1 {
        createSimpleCluster(namespace: $ns, object: $object) {
          metadata { name namespace }
        }
      }
    }
  }
`;

export const createSimpleCluster = async (
  opts: GraphqlClientOptions,
  input: CreateSimpleClusterInput
): Promise<void> => {
  if (fixturesEnabled()) {
    await new Promise((r) => setTimeout(r, 350));
    return;
  }
  // Workers are sized either as a fixed count or an autoscaled min/max
  // range — the CRD forbids sending both.
  const workers: Record<string, unknown> = {
    requested: { cpu: input.workerCpu, memory: input.workerMemory }
  };
  if (input.workerAutoscaling && input.workerMin != null && input.workerMax != null) {
    workers.autoscaling = { min: input.workerMin, max: input.workerMax };
  } else {
    workers.nodes = input.workerNodes;
  }
  const spec: Record<string, unknown> = {
    providerConfigRefs: [input.provider],
    workers
  };
  // Control plane is optional — providers like Gardener manage it themselves.
  if (input.controlPlaneNodes && input.controlPlaneNodes > 0) {
    spec.controlPlane = {
      nodes: input.controlPlaneNodes,
      requested: {
        cpu: input.controlPlaneCpu ?? input.workerCpu,
        memory: input.controlPlaneMemory ?? input.workerMemory
      }
    };
  }

  const object = {
    metadata: { name: input.name, namespace: input.namespace },
    spec
  };
  await graphqlFetch(opts, {
    query: CREATE_SIMPLECLUSTER_MUTATION,
    variables: { ns: input.namespace, object }
  });
};

const DELETE_SIMPLECLUSTER_MUTATION = `
  mutation DeleteSimpleCluster($ns: String!, $name: String!) {
    showroom_apeirora_eu {
      v1alpha1 {
        deleteSimpleCluster(namespace: $ns, name: $name)
      }
    }
  }
`;

export const deleteSimpleCluster = async (
  opts: GraphqlClientOptions,
  namespace: string,
  name: string
): Promise<void> => {
  if (fixturesEnabled()) {
    await new Promise((r) => setTimeout(r, 350));
    return;
  }
  await graphqlFetch(opts, {
    query: DELETE_SIMPLECLUSTER_MUTATION,
    variables: { ns: namespace, name }
  });
};

const SIMPLECLUSTER_SUBSCRIPTION = `
  subscription SimpleClusters($ns: String!) {
    showroom_apeirora_eu_v1alpha1_simpleclusters(
      namespace: $ns
      subscribeToAll: true
    ) {
      type
      object { metadata { name } }
    }
  }
`;

export const subscribeSimpleClusters = (
  opts: GraphqlClientOptions,
  namespace: string,
  onEvent: () => void
): SubscriptionHandle => {
  if (fixturesEnabled()) return () => {};
  return subscribe(opts, SIMPLECLUSTER_SUBSCRIPTION, { ns: namespace }, onEvent);
};
