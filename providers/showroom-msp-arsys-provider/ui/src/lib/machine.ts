/*
 * CRUD + subscriptions for Arsys ArsysUbuntuMachine resources.
 * GraphQL group: arsys_apeirora_eu (arsys.apeirora.eu, dots -> underscores)
 * Kind: ArsysUbuntuMachine (plural arsysubuntumachines, list field ArsysUbuntuMachines).
 *
 * ArsysUbuntuMachine is projected NAMESPACED into the KCP workspace
 * (APIResourceSchema scope: Namespaced) even though the CRD itself is
 * cluster-scoped on the service cluster, so the gateway exposes it WITH a
 * namespace argument — list/create/delete/subscribe all take the workspace
 * namespace from the Luigi context.
 *
 * The whole GraphQL surface is intentionally confined to this file. Field/type
 * names (createArsysUbuntuMachine/deleteArsysUbuntuMachine, the ArsysUbuntuMachines
 * list field, the arsys_apeirora_eu_v1alpha1_arsysubuntumachines subscription and
 * the ArsysApeiroraEuV1alpha1ArsysUbuntuMachine_Input create input type) follow
 * the gateway's own schema-generation rules
 * (github.com/platform-mesh/kubernetes-graphql-gateway, pinned at v1.12.0 for
 * this deployment): the input type is Pascalize(sanitizedGroup_version) + Kind +
 * "_Input". Still unexercised against a live gateway.
 *
 * Every non size-dependent field mirrors the operator's SCPC template
 * (internal/controller/scpc-arsys.yaml). server.{vcore,coresPerProcessor,ram,disk}
 * are integer fields — sent as numbers, not JSON-encoded strings.
 */

import type { ArsysUbuntuMachine } from '../types';
import { graphqlFetch, subscribe, type GraphqlClientOptions, type SubscriptionHandle } from './graphql';
import { fixtureMachines, fixturesEnabled } from './fixtures';

// Hidden defaults, mirrored verbatim from scpc-arsys.yaml.
const TOKEN_SECRET_REF = { name: 'arsys-token', namespace: 'arsys-provider' };
// Service-side namespace the operator WRITES the generated password Secret to;
// the sync-agent mirrors it back into the machine's workspace namespace so the
// portal can read it. Must match the passwordSecretRef namespace resolved by the
// arsysprovider-sync-agent PublishedResource.
const PASSWORD_SECRET_NAMESPACE = 'simple-cluster-arsys';
const APPLIANCE_ID = 'E994A2B0F5B5EE36281F16214D1185D8';
const DATACENTER_ID = '9B94281B5B67A755F2B76165E25F6A65';
const SERVER_TYPE = 'cloud';
const SSH_USER = 'root';

// The synced password Secret is named after the machine (see buildObject); the
// UI reads it back from the workspace namespace under this convention.
export const passwordSecretName = (machineName: string): string => `${machineName}-password`;

export interface MachineSize {
  id: string;
  label: string;
  vcore: number;
  coresPerProcessor: number;
  ram: number;
  disk: number;
}

// Hardcoded sizes exposed in the create dialog; drive spec.server.
export const MACHINE_SIZES: MachineSize[] = [
  { id: 'small', label: '2 vCore / 4 GiB / 50 GiB', vcore: 2, coresPerProcessor: 1, ram: 4, disk: 50 },
  { id: 'medium', label: '4 vCore / 8 GiB / 50 GiB', vcore: 4, coresPerProcessor: 2, ram: 8, disk: 50 },
  { id: 'large', label: '8 vCore / 16 GiB / 100 GiB', vcore: 8, coresPerProcessor: 2, ram: 16, disk: 100 }
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
      tokenSecretRef: TOKEN_SECRET_REF,
      // passwordSecretRef is required; the operator writes the generated root
      // password here. Derived from the machine name like the SCPC does.
      passwordSecretRef: { name: passwordSecretName(input.name), namespace: PASSWORD_SECRET_NAMESPACE },
      server: {
        type: SERVER_TYPE,
        applianceID: APPLIANCE_ID,
        datacenterID: DATACENTER_ID,
        vcore: size.vcore,
        coresPerProcessor: size.coresPerProcessor,
        ram: size.ram,
        disk: size.disk
      },
      ssh: { user: SSH_USER }
    }
  };
};

const INPUT_TYPE = 'ArsysApeiroraEuV1alpha1ArsysUbuntuMachine_Input';

const listQuery = `
  query List($ns: String!) {
    arsys_apeirora_eu {
      v1alpha1 {
        ArsysUbuntuMachines(namespace: $ns) {
          items {
            metadata { name namespace creationTimestamp uid resourceVersion }
            status {
              phase
              address
              provisioningPercent
              serverName
              serverID
              machineRef
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
    arsys_apeirora_eu {
      v1alpha1 {
        createArsysUbuntuMachine(namespace: $ns, object: $object) {
          metadata { name }
        }
      }
    }
  }
`;

const deleteMutation = `
  mutation Delete($ns: String!, $name: String!) {
    arsys_apeirora_eu {
      v1alpha1 {
        deleteArsysUbuntuMachine(namespace: $ns, name: $name)
      }
    }
  }
`;

const subscription = `
  subscription Watch($ns: String!) {
    arsys_apeirora_eu_v1alpha1_arsysubuntumachines(namespace: $ns, subscribeToAll: true) {
      type
      object { metadata { name } }
    }
  }
`;

interface ListResponse {
  arsys_apeirora_eu?: { v1alpha1?: { ArsysUbuntuMachines?: { items: ArsysUbuntuMachine[] } } };
}

export interface MachineApi {
  list: (opts: GraphqlClientOptions, namespace: string) => Promise<ArsysUbuntuMachine[]>;
  create: (opts: GraphqlClientOptions, namespace: string, input: CreateInput) => Promise<void>;
  remove: (opts: GraphqlClientOptions, namespace: string, name: string) => Promise<void>;
  subscribe: (opts: GraphqlClientOptions, namespace: string, onEvent: () => void) => SubscriptionHandle;
}

export const arsysMachineApi: MachineApi = {
  list: async (opts, namespace) => {
    if (fixturesEnabled()) return fixtureMachines();
    const data = await graphqlFetch<ListResponse>(opts, { query: listQuery, variables: { ns: namespace } });
    return data.arsys_apeirora_eu?.v1alpha1?.ArsysUbuntuMachines?.items ?? [];
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
