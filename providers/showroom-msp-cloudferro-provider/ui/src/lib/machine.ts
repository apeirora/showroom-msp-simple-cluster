/*
 * CRUD + subscriptions for CloudFerro OpenStackVM resources.
 * GraphQL group: openstack_apeirora_eu (openstack.apeirora.eu, dots -> underscores)
 * Kind: OpenStackVM (plural openstackvms, list field OpenStackVMs).
 *
 * OpenStackVM is projected CLUSTER-SCOPED into the KCP workspace
 * (APIResourceSchema scope: Cluster), so the gateway exposes it WITHOUT a
 * namespace argument — list/create/delete/subscribe take no ns.
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

const buildObject = (input: CreateInput): Record<string, unknown> => {
  const size = MACHINE_SIZES.find((s) => s.id === input.sizeId) ?? MACHINE_SIZES[0];
  return {
    metadata: { name: input.name },
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
  query List {
    openstack_apeirora_eu {
      v1alpha1 {
        OpenStackVMs {
          items {
            metadata { name creationTimestamp uid resourceVersion }
            status {
              phase
              internalAddress
              externalAddress
              novaStatus
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
  mutation Create($object: ${INPUT_TYPE}!) {
    openstack_apeirora_eu {
      v1alpha1 {
        createOpenStackVM(object: $object) {
          metadata { name }
        }
      }
    }
  }
`;

const deleteMutation = `
  mutation Delete($name: String!) {
    openstack_apeirora_eu {
      v1alpha1 {
        deleteOpenStackVM(name: $name)
      }
    }
  }
`;

const subscription = `
  subscription Watch {
    openstack_apeirora_eu_v1alpha1_openstackvms(subscribeToAll: true) {
      type
      object { metadata { name } }
    }
  }
`;

interface ListResponse {
  openstack_apeirora_eu?: { v1alpha1?: { OpenStackVMs?: { items: OpenStackVM[] } } };
}

export interface MachineApi {
  list: (opts: GraphqlClientOptions) => Promise<OpenStackVM[]>;
  create: (opts: GraphqlClientOptions, input: CreateInput) => Promise<void>;
  remove: (opts: GraphqlClientOptions, name: string) => Promise<void>;
  subscribe: (opts: GraphqlClientOptions, onEvent: () => void) => SubscriptionHandle;
}

export const openStackVmApi: MachineApi = {
  list: async (opts) => {
    if (fixturesEnabled()) return fixtureVMs();
    const data = await graphqlFetch<ListResponse>(opts, { query: listQuery });
    return data.openstack_apeirora_eu?.v1alpha1?.OpenStackVMs?.items ?? [];
  },
  create: async (opts, input) => {
    if (fixturesEnabled()) {
      await new Promise((r) => setTimeout(r, 350));
      return;
    }
    await graphqlFetch(opts, { query: createMutation, variables: { object: buildObject(input) } });
  },
  remove: async (opts, name) => {
    if (fixturesEnabled()) {
      await new Promise((r) => setTimeout(r, 350));
      return;
    }
    await graphqlFetch(opts, { query: deleteMutation, variables: { name } });
  },
  subscribe: (opts, onEvent) => {
    if (fixturesEnabled()) return () => {};
    return subscribe(opts, subscription, {}, onEvent);
  }
};
