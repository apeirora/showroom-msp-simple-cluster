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
            metadata { name namespace creationTimestamp deletionTimestamp uid resourceVersion }
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
  namespace: string,
  signal?: AbortSignal
): Promise<SimpleCluster[]> => {
  if (fixturesEnabled()) return fixtureSimpleClusters();
  const data = await graphqlFetch<SimpleClusterListResponse>(opts, {
    query: CLUSTER_LIST_QUERY,
    variables: { ns: namespace }
  }, signal);
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
  /**
   * Labels to create the object with. This is how an extension is switched on at
   * creation time: an operator on the MSP side watches for the label and installs the
   * addon once the cluster is ready.
   */
  labels?: Record<string, string>;
}

const CREATE_SIMPLECLUSTER_MUTATION = `
  mutation CreateSimpleCluster($ns: String!, $object: ShowroomApeiroraEuV1alpha1SimpleCluster_Input!) {
    showroom_apeirora_eu {
      v1alpha1 {
        createSimpleCluster(namespace: $ns, object: $object) {
          metadata { name namespace labels }
        }
      }
    }
  }
`;

// The gateway turns `object` into a JSON merge patch, so this sends only the labels and
// leaves everything else on the object alone.
const LABEL_SIMPLECLUSTER_MUTATION = `
  mutation LabelSimpleCluster($ns: String!, $name: String!, $object: ShowroomApeiroraEuV1alpha1SimpleCluster_Input!) {
    showroom_apeirora_eu {
      v1alpha1 {
        updateSimpleCluster(namespace: $ns, name: $name, object: $object) {
          metadata { name labels }
        }
      }
    }
  }
`;

// The gateway schema types Quantity fields (cpu/memory) as JSONString: the
// value must itself be valid JSON. Plain "2Gi" is not ("1" only passes by
// parsing as a number), so string quantities are JSON-encoded on input.
const quantity = (v: string): string => JSON.stringify(v);

interface CreateResponse {
  showroom_apeirora_eu?: {
    v1alpha1?: {
      createSimpleCluster?: {
        metadata?: { labels?: Record<string, string> | null } | null;
      } | null;
    } | null;
  } | null;
}

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
    requested: { cpu: quantity(input.workerCpu), memory: quantity(input.workerMemory) }
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
        cpu: quantity(input.controlPlaneCpu ?? input.workerCpu),
        memory: quantity(input.controlPlaneMemory ?? input.workerMemory)
      }
    };
  }

  const labels = input.labels ?? {};
  const wanted = Object.keys(labels);
  const object = {
    metadata: {
      name: input.name,
      namespace: input.namespace,
      ...(wanted.length ? { labels } : {})
    },
    spec
  };
  const created = await graphqlFetch<CreateResponse>(opts, {
    query: CREATE_SIMPLECLUSTER_MUTATION,
    variables: { ns: input.namespace, object }
  });

  // Labels are how an extension gets switched on, so losing them silently would mean a
  // ticked checkbox that installs nothing. Rather than trusting that the create
  // resolver persists them, look at what came back and set them separately if it did
  // not. Costs one extra round trip only in the case where it is the difference
  // between working and not.
  const persisted = created?.showroom_apeirora_eu?.v1alpha1?.createSimpleCluster?.metadata?.labels ?? {};
  const missing = wanted.filter((k) => persisted[k] !== labels[k]);
  if (missing.length) {
    await graphqlFetch(opts, {
      query: LABEL_SIMPLECLUSTER_MUTATION,
      variables: { ns: input.namespace, name: input.name, object: { metadata: { labels } } }
    });
  }
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
