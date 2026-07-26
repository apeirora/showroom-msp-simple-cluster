/*
 * CRUD + subscriptions for IronCoreVM resources served by
 * simple-cluster-operator-ironcore.
 * GraphQL group: ironcore_apeirora_eu (ironcore.apeirora.eu, dots -> underscores)
 * Kind: IronCoreVM (plural ironcorevms, list field IronCoreVMs).
 *
 * IronCoreVM is projected NAMESPACED into the KCP workspace (APIResourceSchema
 * scope: Namespaced), so the gateway exposes it WITH a namespace argument —
 * list/create/delete/subscribe all take the workspace namespace from the Luigi
 * context. Mirrors the CloudFerro OpenStackVM surface (ui/src/lib/machine.ts).
 *
 * The whole GraphQL surface is intentionally confined to this file. Field/type
 * names (createIronCoreVM/deleteIronCoreVM, the IronCoreVMs list field, the
 * ironcore_apeirora_eu_v1alpha1_ironcorevms subscription and the
 * IroncoreApeiroraEuV1alpha1IronCoreVM_Input create input type) follow the
 * gateway's own schema-generation rules
 * (github.com/platform-mesh/kubernetes-graphql-gateway, pinned at v1.12.0 for
 * this deployment): the input type is Pascalize(sanitizedGroup_version) + Kind +
 * "_Input". Still unexercised against a live gateway.
 *
 * The machine size is pinned via spec.cloudProfile (an exact MachineClass name):
 * the operator resolves it with a direct Get (internal/ironcore/machineclass.go
 * GetMachineClass), gated by `case vm.Spec.CloudProfile != ""` in the
 * controller. spec.cloudProfile and spec.resources are mutually exclusive (CRD
 * XValidation), so resources is omitted entirely — there are no Quantity leaves
 * in the payload and nothing needs JSON.stringify'ing.
 */

import type { IronCoreVM } from '../types';
import { graphqlFetch, subscribe, type GraphqlClientOptions, type SubscriptionHandle } from './graphql';
import { fixtureVMs, fixturesEnabled } from './fixtures';

// Hidden defaults for the create payload.
const IRONCORE_KUBECONFIG = { name: 'ironcore-kubeconfig', namespace: 'sap-provider' };
const NETWORK = 'sample-net';
const IMAGE = 'ghcr.io/ironcore-dev/gardenlinux:latest';
const PUBLIC_IP = true;
const SSH_USER = 'root';

export interface MachineClassOption {
  // MachineClass name, used verbatim as spec.cloudProfile (exact-name pin).
  id: string;
  label: string;
}

// The three IronCore memory-intel1 MachineClasses exposed in the create dialog.
// id is the exact MachineClass name pinned via spec.cloudProfile.
export const MACHINE_CLASSES: MachineClassOption[] = [
  { id: 'memory-intel1-1cpu', label: '1 vCPU / 8 Gi' },
  { id: 'memory-intel1-2cpu', label: '2 vCPU / 16 Gi' },
  { id: 'memory-intel1-4cpu', label: '4 vCPU / 32 Gi' }
];

export const defaultMachineClassId = MACHINE_CLASSES[0].id;

export interface CreateInput {
  name: string;
  machineClassId: string;
}

const buildObject = (namespace: string, input: CreateInput): Record<string, unknown> => {
  const cls = MACHINE_CLASSES.find((c) => c.id === input.machineClassId) ?? MACHINE_CLASSES[0];
  return {
    metadata: { name: input.name, namespace },
    spec: {
      ironcoreKubeconfig: IRONCORE_KUBECONFIG,
      network: NETWORK,
      image: IMAGE,
      publicIP: PUBLIC_IP,
      sshUser: SSH_USER,
      // Exact MachineClass pin; mutually exclusive with spec.resources.
      cloudProfile: cls.id
    }
  };
};

const INPUT_TYPE = 'IroncoreApeiroraEuV1alpha1IronCoreVM_Input';

const listQuery = `
  query List($ns: String!) {
    ironcore_apeirora_eu {
      v1alpha1 {
        IronCoreVMs(namespace: $ns) {
          items {
            metadata { name namespace creationTimestamp uid resourceVersion }
            status {
              phase
              machineState
              internalAddress
              externalAddress
              sshSecretRef { name }
              resolvedMachineClass { name cpu memory description }
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
    ironcore_apeirora_eu {
      v1alpha1 {
        createIronCoreVM(namespace: $ns, object: $object) {
          metadata { name }
        }
      }
    }
  }
`;

const deleteMutation = `
  mutation Delete($ns: String!, $name: String!) {
    ironcore_apeirora_eu {
      v1alpha1 {
        deleteIronCoreVM(namespace: $ns, name: $name)
      }
    }
  }
`;

const subscription = `
  subscription Watch($ns: String!) {
    ironcore_apeirora_eu_v1alpha1_ironcorevms(namespace: $ns, subscribeToAll: true) {
      type
      object { metadata { name } }
    }
  }
`;

interface ListResponse {
  ironcore_apeirora_eu?: { v1alpha1?: { IronCoreVMs?: { items: IronCoreVM[] } } };
}

export interface VmApi {
  list: (opts: GraphqlClientOptions, namespace: string) => Promise<IronCoreVM[]>;
  create: (opts: GraphqlClientOptions, namespace: string, input: CreateInput) => Promise<void>;
  remove: (opts: GraphqlClientOptions, namespace: string, name: string) => Promise<void>;
  subscribe: (opts: GraphqlClientOptions, namespace: string, onEvent: () => void) => SubscriptionHandle;
}

export const ironCoreVmApi: VmApi = {
  list: async (opts, namespace) => {
    if (fixturesEnabled()) return fixtureVMs();
    const data = await graphqlFetch<ListResponse>(opts, { query: listQuery, variables: { ns: namespace } });
    return data.ironcore_apeirora_eu?.v1alpha1?.IronCoreVMs?.items ?? [];
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
