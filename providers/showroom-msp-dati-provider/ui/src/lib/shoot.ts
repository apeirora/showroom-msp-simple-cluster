/*
 * CRUD + subscriptions for dati Shoot resources.
 * GroupVersionKind: dati.gardener/v1alpha1 Shoot
 * GraphQL root: dati_gardener.v1alpha1.Shoots
 *
 * The Shoot is projected CLUSTER-SCOPED into the KCP workspace
 * (PublishedResource projection.scope: Cluster), so the gateway exposes it
 * WITHOUT a namespace argument — list/create/delete/subscribe take no ns.
 * Passing one would be a schema error. This mirrors the cluster-scoped query
 * shape already used by simple-cluster-ui/src/lib/providers.ts.
 *
 * The whole GraphQL surface for Shoots is intentionally confined to this file:
 * if the live gateway schema differs, this is the single place to adjust.
 * Field/type names below (createShoot/deleteShoot, the Shoots list field, the
 * dati_gardener_v1alpha1_shoots subscription, and the create input type) are
 * derived from the gateway's own schema-generation source
 * (github.com/platform-mesh/kubernetes-graphql-gateway, pinned at v1.12.0 for
 * this deployment) — not guessed. Still unexercised against a live gateway.
 */

import type { Shoot } from '../types';
import { graphqlFetch, subscribe, type GraphqlClientOptions, type SubscriptionHandle } from './graphql';
import { fixtureShoots, fixturesEnabled } from './fixtures';

const SHOOT_LIST_QUERY = `
  query Shoots {
    dati_gardener {
      v1alpha1 {
        Shoots {
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

interface ShootListResponse {
  dati_gardener: {
    v1alpha1: { Shoots: { items: Shoot[] } };
  };
}

export const listShoots = async (opts: GraphqlClientOptions): Promise<Shoot[]> => {
  if (fixturesEnabled()) return fixtureShoots();
  const data = await graphqlFetch<ShootListResponse>(opts, { query: SHOOT_LIST_QUERY });
  return data.dati_gardener?.v1alpha1?.Shoots?.items ?? [];
};

export interface CreateShootInput {
  name: string;
  // Free-text Gardener Shoot manifest used as the SCPC resource template.
  // Empty => the operator's built-in default template is used.
  spec?: string;
}

// The gateway names each resource's input type after its unique GraphQL type
// name (group+version+kind, PascalCased) plus an "_Input" suffix — not a bare
// "<Kind>Input" — to avoid collisions between same-named kinds across groups.
// For dati.gardener/v1alpha1 Shoot that is DatiGardenerV1alpha1Shoot_Input.
// Confirmed against the pinned gateway version (v1.12.0, which already
// contains the type-naming change) via the gateway's own source/e2e tests.
const CREATE_SHOOT_MUTATION = `
  mutation CreateShoot($object: DatiGardenerV1alpha1Shoot_Input!) {
    dati_gardener {
      v1alpha1 {
        createShoot(object: $object) {
          metadata { name }
        }
      }
    }
  }
`;

export const createShoot = async (
  opts: GraphqlClientOptions,
  input: CreateShootInput
): Promise<void> => {
  if (fixturesEnabled()) {
    await new Promise((r) => setTimeout(r, 350));
    return;
  }
  const object: Record<string, unknown> = { metadata: { name: input.name } };
  const spec = input.spec?.trim();
  if (spec) object.spec = spec;
  await graphqlFetch(opts, {
    query: CREATE_SHOOT_MUTATION,
    variables: { object }
  });
};

const DELETE_SHOOT_MUTATION = `
  mutation DeleteShoot($name: String!) {
    dati_gardener {
      v1alpha1 {
        deleteShoot(name: $name)
      }
    }
  }
`;

export const deleteShoot = async (opts: GraphqlClientOptions, name: string): Promise<void> => {
  if (fixturesEnabled()) {
    await new Promise((r) => setTimeout(r, 350));
    return;
  }
  await graphqlFetch(opts, {
    query: DELETE_SHOOT_MUTATION,
    variables: { name }
  });
};

const SHOOT_SUBSCRIPTION = `
  subscription Shoots {
    dati_gardener_v1alpha1_shoots(subscribeToAll: true) {
      type
      object { metadata { name } }
    }
  }
`;

export const subscribeShoots = (
  opts: GraphqlClientOptions,
  onEvent: () => void
): SubscriptionHandle => {
  if (fixturesEnabled()) return () => {};
  return subscribe(opts, SHOOT_SUBSCRIPTION, {}, onEvent);
};
