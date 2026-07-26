/*
 * CRUD + subscriptions for CloudFerro OpenStackVM resources.
 * GraphQL group: openstack_apeirora_eu (openstack.apeirora.eu, dots -> underscores)
 * Kind: OpenStackVM (plural openstackvms, list field OpenStackVMs).
 *
 * OpenStackVM is projected NAMESPACED into the KCP workspace
 * (APIResourceSchema scope: Namespaced), so the gateway exposes it WITH a
 * namespace argument — list/create/delete/subscribe all take the workspace
 * namespace from the Luigi context.
 *
 * The whole GraphQL surface is intentionally confined to this file. Field/type
 * names (createOpenStackVM/deleteOpenStackVM, the OpenStackVMs list field, the
 * openstack_apeirora_eu_v1alpha1_openstackvms subscription and the
 * OpenstackApeiroraEuV1alpha1OpenStackVM_Input create input type) follow the
 * gateway's own schema-generation rules
 * (github.com/platform-mesh/kubernetes-graphql-gateway, pinned at v1.12.0 for
 * this deployment): the input type is Pascalize(sanitizedGroup_version) + Kind +
 * "_Input". Still unexercised against a live gateway.
 *
 * Every non size-dependent field mirrors the operator's SCPC template
 * (internal/controller/scpc-cloudferro.yaml). Quantity fields (resources.cpu /
 * resources.memory) are typed as JSONString by the gateway, so they are sent
 * JSON.stringify'd.
 */

import type { OpenStackVM } from '../types';
import { graphqlFetch, subscribe, type GraphqlClientOptions, type SubscriptionHandle } from './graphql';
import { fixtureVMs, fixturesEnabled } from './fixtures';

// Hidden defaults, mirrored verbatim from scpc-cloudferro.yaml.
const CREDENTIALS_REF = { name: 'cloudferro-openstack-creds', namespace: 'cloudferro-provider' };
const NETWORK = 'cloud_080745_1';
const FLOATING_IP_POOL = 'external';
const IMAGE = 'Ubuntu 24.04 LTS';
// Single-quoted in the SCPC YAML; the backslash must be escaped in a TS string.
const FLAVOR_PATTERN = '^eo[12]a\\.';
const SECURITY_GROUPS = ['default'];

export interface MachineSize {
  id: string;
  label: string;
  cpu: string;
  memory: string;
}

// Hardcoded sizes exposed in the create dialog; drive spec.resources {cpu, memory}.
export const MACHINE_SIZES: MachineSize[] = [
  { id: 'small', label: '1 vCPU / 2 GiB', cpu: '1', memory: '2Gi' },
  { id: 'medium', label: '2 vCPU / 4 GiB', cpu: '2', memory: '4Gi' },
  { id: 'large', label: '4 vCPU / 8 GiB', cpu: '4', memory: '8Gi' }
];

export const defaultSizeId = MACHINE_SIZES[0].id;

export interface CreateInput {
  name: string;
  sizeId: string;
}

const buildObject = (namespace: string, input: CreateInput): Record<string, unknown> => {
  const size = MACHINE_SIZES.find((s) => s.id === input.sizeId) ?? MACHINE_SIZES[0];
  return {
    metadata: { name: input.name, namespace },
    spec: {
      credentialsRef: CREDENTIALS_REF,
      network: NETWORK,
      floatingIPPool: FLOATING_IP_POOL,
      floatingIP: true,
      securityGroups: SECURITY_GROUPS,
      resources: {
        // Quantity fields are JSONString on the gateway — send stringified.
        cpu: JSON.stringify(size.cpu),
        memory: JSON.stringify(size.memory),
        flavorPattern: FLAVOR_PATTERN
      },
      image: IMAGE
    }
  };
};

const INPUT_TYPE = 'OpenstackApeiroraEuV1alpha1OpenStackVM_Input';

const listQuery = `
  query List($ns: String!) {
    openstack_apeirora_eu {
      v1alpha1 {
        OpenStackVMs(namespace: $ns) {
          items {
            metadata { name namespace creationTimestamp uid resourceVersion }
            status {
              phase
              internalAddress
              externalAddress
              novaStatus
              sshSecretRef { name }
              resolvedFlavor { flavor cpu memory flavorDescription }
              conditions { type status reason message lastTransitionTime }
            }
          }
        }
      }
    }
  }
`;

const createMutation = `
  mutation Create($ns: String!, $object: ${INPUT_TYPE}!) {
    openstack_apeirora_eu {
      v1alpha1 {
        createOpenStackVM(namespace: $ns, object: $object) {
          metadata { name }
        }
      }
    }
  }
`;

const deleteMutation = `
  mutation Delete($ns: String!, $name: String!) {
    openstack_apeirora_eu {
      v1alpha1 {
        deleteOpenStackVM(namespace: $ns, name: $name)
      }
    }
  }
`;

const subscription = `
  subscription Watch($ns: String!) {
    openstack_apeirora_eu_v1alpha1_openstackvms(namespace: $ns, subscribeToAll: true) {
      type
      object { metadata { name } }
    }
  }
`;

interface ListResponse {
  openstack_apeirora_eu?: { v1alpha1?: { OpenStackVMs?: { items: OpenStackVM[] } } };
}

export interface MachineApi {
  list: (opts: GraphqlClientOptions, namespace: string) => Promise<OpenStackVM[]>;
  create: (opts: GraphqlClientOptions, namespace: string, input: CreateInput) => Promise<void>;
  remove: (opts: GraphqlClientOptions, namespace: string, name: string) => Promise<void>;
  subscribe: (opts: GraphqlClientOptions, namespace: string, onEvent: () => void) => SubscriptionHandle;
}

export const openStackVmApi: MachineApi = {
  list: async (opts, namespace) => {
    if (fixturesEnabled()) return fixtureVMs();
    const data = await graphqlFetch<ListResponse>(opts, { query: listQuery, variables: { ns: namespace } });
    return data.openstack_apeirora_eu?.v1alpha1?.OpenStackVMs?.items ?? [];
  },
  create: async (opts, namespace, input) => {
    if (fixturesEnabled()) {
      await new Promise((r) => setTimeout(r, 350));
      return;
    }
    await graphqlFetch(opts, {
      query: createMutation,
      variables: { ns: namespace, object: buildObject(namespace, input) }
    });
  },
  remove: async (opts, namespace, name) => {
    if (fixturesEnabled()) {
      await new Promise((r) => setTimeout(r, 350));
      return;
    }
    await graphqlFetch(opts, { query: deleteMutation, variables: { ns: namespace, name } });
  },
  subscribe: (opts, namespace, onEvent) => {
    if (fixturesEnabled()) return () => {};
    return subscribe(opts, subscription, { ns: namespace }, onEvent);
  }
};
