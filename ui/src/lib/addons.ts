/*
 * Extensions offered while a cluster is being created.
 *
 * Two independent questions, kept apart on purpose:
 *
 *   catalogue   — which extensions exist and what label switches each one on. Global,
 *                 published by each extension itself and aggregated into a file this
 *                 UI fetches. Nothing account-specific is in it.
 *   entitlement — whether *this* account has a given extension. Per account, and only
 *                 KCP can answer it, so it is asked through the gateway.
 */

import { graphqlFetch, GraphqlError, type GraphqlClientOptions } from './graphql';
import { fixtureCatalog, fixtureEntitlements, fixturesEnabled } from './fixtures';
import type { AddonCatalogEntry, AddonEntitlement, EntitlementState } from '../types';

/** Written by simple-cluster-operator, mounted into this pod as a file. */
const CATALOG_URL = '/addons/simple-cluster-addons.json';

export interface AddonCatalog {
  entries: AddonCatalogEntry[];
  /**
   * Whether the catalogue was actually read. An empty list with available=true means
   * no extensions are installed, which is a legitimate answer; available=false means
   * nobody answered, which is a deployment fault.
   */
  available: boolean;
}

const EMPTY: AddonCatalog = { entries: [], available: false };

/**
 * Reads the aggregated catalogue.
 *
 * The file is mounted from an optional ConfigMap, so when the aggregator has not run
 * the directory is empty and this fetch gets nginx's 404 page — HTML, not JSON. That
 * failure and "no extensions installed" would otherwise render identically (no
 * Extensions section at all), which is exactly the kind of silence that survives a
 * demo, so the two are logged differently and only the second is a normal outcome.
 */
export const loadAddonCatalog = async (url = CATALOG_URL): Promise<AddonCatalog> => {
  if (fixturesEnabled()) return { entries: fixtureCatalog(), available: true };

  let body: unknown;
  try {
    const resp = await fetch(url, { cache: 'no-cache' });
    if (!resp.ok) {
      console.warn(
        `[simple-cluster-ui] addon catalogue unavailable (HTTP ${resp.status}). ` +
          'No extensions will be offered. Expected simple-cluster-operator to publish ' +
          'the simple-cluster-addons ConfigMap into this namespace.'
      );
      return EMPTY;
    }
    body = await resp.json();
  } catch (err) {
    console.warn('[simple-cluster-ui] addon catalogue could not be read', err);
    return EMPTY;
  }

  const entries = (body as { addons?: unknown })?.addons;
  if (!Array.isArray(entries)) {
    console.warn('[simple-cluster-ui] addon catalogue is not a catalogue document', body);
    return EMPTY;
  }

  // One malformed entry costs only itself: the catalogue is assembled from ConfigMaps
  // several charts publish, and no one of them owns the file.
  const usable = (entries as AddonCatalogEntry[]).filter((e) => e?.id && e?.label);
  return { entries: usable, available: true };
};

/** Extensions that may be switched on while creating this kind of object. */
export const entriesForKind = (entries: AddonCatalogEntry[], kind: string): AddonCatalogEntry[] =>
  entries.filter((e) => !e.targetKinds?.length || e.targetKinds.some((t) => t.kind === kind));

// Kubernetes group -> GraphQL identifier, the same rule provider discovery uses.
const groupId = (group: string): string => group.replace(/[.-]/g, '_');

// kcp serves APIBindings in every workspace; whether this gateway exposes them is not
// something the UI can assume, so a schema error here falls through to the probe below
// rather than being reported as "not entitled".
const BINDINGS_QUERY = `
  query BoundExports {
    apis_kcp_io {
      v1alpha1 {
        APIBindings {
          items {
            metadata { name }
          }
        }
      }
    }
  }
`;

interface NamedItem {
  metadata?: { name?: string | null } | null;
}

const bindingNames = (data: unknown): string[] => {
  const list = (data as Record<string, any>)?.apis_kcp_io?.v1alpha1?.APIBindings?.items;
  if (!Array.isArray(list)) return [];
  return (list as NamedItem[]).map((i) => i?.metadata?.name ?? '').filter(Boolean);
};

/**
 * A binding for `bindName` exists.
 *
 * Matched on a prefix as well as exactly, because kcp appends a generated suffix when
 * it has to disambiguate a binding name -- `core.platform-mesh.io-8hfib` is a real one.
 */
const hasBinding = (names: string[], bindName: string): boolean =>
  names.some((n) => n === bindName || n.startsWith(`${bindName}-`));

const probeQuery = (e: AddonCatalogEntry): string => {
  const ent = e.entitlement!;
  const header = ent.namespaced ? 'query Entitlement($ns: String!)' : 'query Entitlement';
  const call = ent.namespaced ? `${ent.listField}(namespace: $ns)` : ent.listField;
  return `
  ${header} {
    ${groupId(ent.group)} {
      ${ent.version} {
        ${call} {
          items { metadata { name } }
        }
      }
    }
  }
`;
};

/**
 * Whether this account has the extension, by asking for a Kind only an account that
 * bound it can see.
 *
 * Data or a GraphQL-level error is a definitive answer (bound / not bound); only a
 * transport error leaves it unknown. Note the direction: for provider discovery an
 * inconclusive answer falls back to *showing* the options, which is harmless there.
 * Here it must not — an offered checkbox is a promise, so anything short of a clear
 * yes leaves the box disabled.
 */
const probe = async (
  opts: GraphqlClientOptions,
  namespace: string,
  entry: AddonCatalogEntry
): Promise<EntitlementState> => {
  if (!entry.entitlement?.group || !entry.entitlement?.listField) return 'unknown';
  try {
    await graphqlFetch<unknown>(opts, {
      query: probeQuery(entry),
      variables: entry.entitlement.namespaced ? { ns: namespace } : {}
    });
    return 'entitled';
  } catch (err) {
    if (err instanceof GraphqlError && err.kind === 'graphql') return 'not-entitled';
    console.warn(`[simple-cluster-ui] entitlement probe for ${entry.id} was inconclusive`, err);
    return 'unknown';
  }
};

/**
 * Resolves entitlement for every catalogue entry.
 *
 * The binding list is asked for once and reused: it answers for every extension at
 * once, and covers those that publish no Kind of their own (a bind-only provider has
 * nothing to probe). Extensions are only probed individually when that route is not
 * available.
 */
export const resolveEntitlements = async (
  opts: GraphqlClientOptions,
  namespace: string,
  entries: AddonCatalogEntry[]
): Promise<Map<string, AddonEntitlement>> => {
  const out = new Map<string, AddonEntitlement>();
  if (!entries.length) return out;
  if (fixturesEnabled()) return fixtureEntitlements(entries);

  let bindings: string[] | null = null;
  if (!opts.graphqlUrl) {
    for (const e of entries) {
      out.set(e.id, { state: 'unknown', detail: 'The portal session is not ready yet.' });
    }
    return out;
  }

  try {
    bindings = bindingNames(await graphqlFetch<unknown>(opts, { query: BINDINGS_QUERY }));
    if (!bindings.length) {
      // Every account binds something (its core export at the very least), so an empty
      // list is far more likely to be RBAC than the truth -- and it would disable every
      // extension without saying why. Probe instead.
      console.warn('[simple-cluster-ui] no APIBindings are visible here; falling back to probes');
      bindings = null;
    }
  } catch (err) {
    console.warn(
      '[simple-cluster-ui] APIBindings are not readable here; falling back to per-extension probes',
      err
    );
  }

  await Promise.all(
    entries.map(async (entry) => {
      // A binding that matches is conclusive; one that does not is not. Binding names
      // are the unverified half of this mechanism -- the match is by prefix because kcp
      // generates suffixes, and an extension may name its export differently again --
      // so a miss falls through to the probe rather than greying out an extension the
      // account actually has. Only both saying no makes it a no.
      if (bindings && entry.bindName && hasBinding(bindings, entry.bindName)) {
        out.set(entry.id, { state: 'entitled', detail: '' });
        return;
      }

      const state = await probe(opts, namespace, entry);
      out.set(entry.id, {
        state,
        detail:
          state === 'entitled'
            ? ''
            : state === 'not-entitled'
              ? `Not available in this workspace. Install ${entry.displayName ?? entry.id} from the marketplace first.`
              : 'Could not check whether this workspace has the extension.'
      });
    })
  );

  return out;
};
