/*
 * CRUD + subscriptions for SAP Integration provider Shoot resources.
 * GraphQL group: sap_gardener.v1alpha1
 * Two Kinds share the same shape and surface:
 *   - OpenStackShoot (list field OpenStackShoots)
 *   - IronCoreShoot  (list field IronCoreShoots)
 *
 * Both Shoots are projected CLUSTER-SCOPED into the KCP workspace
 * (PublishedResource projection.scope: Cluster), so the gateway exposes them
 * WITHOUT a namespace argument — list/create/delete/subscribe take no ns.
 * Passing one would be a schema error.
 *
 * The whole GraphQL surface is intentionally confined to this file and built by
 * a single factory (makeShootApi): if the live gateway schema differs, this is
 * the single place to adjust. Field/type names (create<Kind>/delete<Kind>, the
 * <Kind>s list field, the sap_gardener_v1alpha1_<kind>s subscription and the
 * SapGardenerV1alpha1<Kind>_Input create input type) follow the gateway's own
 * schema-generation rules (github.com/platform-mesh/kubernetes-graphql-gateway,
 * pinned at v1.12.0 for this deployment). Still unexercised against a live
 * gateway.
 */

import type { ProviderShoot } from '../types';
import { graphqlFetch, subscribe, type GraphqlClientOptions, type SubscriptionHandle } from './graphql';
import { fixtureShoots, fixturesEnabled, type ShootFlavor } from './fixtures';

export interface CreateShootInput {
  name: string;
  // Free-text Gardener Shoot manifest used as the SCPC resource template.
  // Empty => the operator's built-in default template is used.
  spec?: string;
}

export interface ShootApi {
  list: (opts: GraphqlClientOptions) => Promise<ProviderShoot[]>;
  create: (opts: GraphqlClientOptions, input: CreateShootInput) => Promise<void>;
  remove: (opts: GraphqlClientOptions, name: string) => Promise<void>;
  subscribe: (opts: GraphqlClientOptions, onEvent: () => void) => SubscriptionHandle;
}

interface ShootApiConfig {
  // Fixture flavor + offline default resource name.
  flavor: ShootFlavor;
  // Kind name, e.g. "OpenStackShoot" — drives create<Kind>/delete<Kind>.
  kind: string;
  // Plural GraphQL list field, e.g. "OpenStackShoots".
  listField: string;
  // Create input type, e.g. "SapGardenerV1alpha1OpenStackShoot_Input".
  inputType: string;
  // Subscription root field, e.g. "sap_gardener_v1alpha1_openstackshoots".
  subscription: string;
}

interface ShootListResponse {
  sap_gardener?: { v1alpha1?: Record<string, { items: ProviderShoot[] }> };
}

const makeShootApi = (cfg: ShootApiConfig): ShootApi => {
  const listQuery = `
    query List {
      sap_gardener {
        v1alpha1 {
          ${cfg.listField} {
            items {
              metadata { name creationTimestamp uid resourceVersion }
              spec
              status {
                providerConfigs
                observedGeneration
                conditions { type status reason message lastTransitionTime }
              }
            }
          }
        }
      }
    }
  `;

  const createMutation = `
    mutation Create($object: ${cfg.inputType}!) {
      sap_gardener {
        v1alpha1 {
          create${cfg.kind}(object: $object) {
            metadata { name }
          }
        }
      }
    }
  `;

  const deleteMutation = `
    mutation Delete($name: String!) {
      sap_gardener {
        v1alpha1 {
          delete${cfg.kind}(name: $name)
        }
      }
    }
  `;

  const subscription = `
    subscription Watch {
      ${cfg.subscription}(subscribeToAll: true) {
        type
        object { metadata { name } }
      }
    }
  `;

  return {
    list: async (opts) => {
      if (fixturesEnabled()) return fixtureShoots(cfg.flavor);
      const data = await graphqlFetch<ShootListResponse>(opts, { query: listQuery });
      return data.sap_gardener?.v1alpha1?.[cfg.listField]?.items ?? [];
    },
    create: async (opts, input) => {
      if (fixturesEnabled()) {
        await new Promise((r) => setTimeout(r, 350));
        return;
      }
      const object: Record<string, unknown> = { metadata: { name: input.name } };
      const spec = input.spec?.trim();
      if (spec) object.spec = spec;
      await graphqlFetch(opts, { query: createMutation, variables: { object } });
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
};

export const openstackShootApi = makeShootApi({
  flavor: 'openstack',
  kind: 'OpenStackShoot',
  listField: 'OpenStackShoots',
  inputType: 'SapGardenerV1alpha1OpenStackShoot_Input',
  subscription: 'sap_gardener_v1alpha1_openstackshoots'
});

export const ironcoreShootApi = makeShootApi({
  flavor: 'ironcore',
  kind: 'IronCoreShoot',
  listField: 'IronCoreShoots',
  inputType: 'SapGardenerV1alpha1IronCoreShoot_Input',
  subscription: 'sap_gardener_v1alpha1_ironcoreshoots'
});
