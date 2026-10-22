/*
 * Live provider discovery. Each installed provider ships its own CR family
 * (e.g. dati.eu/Shoot). A source's type resolving in the gateway schema
 * means its APIExport is bound, i.e. the provider is installed — that alone
 * is enough to offer src.providerConfigName, whether or not any CR instance
 * exists yet. Existing instances additionally advertise the
 * SimpleClusterProviderConfig names they support via status.providerConfigs.
 * We query every configured source and take the union of both — that is the
 * live provider drop-down.
 */

import type { ProviderSource } from '../types';
import { graphqlFetch, GraphqlError, type GraphqlClientOptions } from './graphql';
import { fixturesEnabled } from './fixtures';

export interface ProviderDiscovery {
  providers: string[];
  // true once the gateway gave a definitive answer for at least one source —
  // either data, or a GraphQL-level error meaning that provider is simply not
  // installed. false only when the gateway was unreachable (transport error)
  // or discovery could not run, in which case callers keep the static fallback.
  ran: boolean;
}

// Kubernetes group -> GraphQL identifier: dots and dashes become underscores
// (e.g. "dati.eu" -> "dati_eu").
const groupId = (group: string): string => group.replace(/[.-]/g, '_');

interface ProviderItem {
  status?: { providerConfigs?: string[] | null };
}

// Cluster-scoped list fields (the default for provider CRs, which are
// projected cluster-scoped in KCP) take no namespace argument; the gateway
// only adds one for namespaced resources, so passing it would be a schema
// error. Only namespaced sources get the $ns argument.
//
// Sources that set providerConfigName only need their type to resolve in the
// gateway schema — that alone means the provider is installed (see header
// comment). Some provider CR kinds (the VM kinds) have no status field at
// all, so selecting status.providerConfigs on them is a GraphQL validation
// error that would wrongly mark the provider as not installed. For those
// sources we select only metadata { name }. Sources without
// providerConfigName keep the status-union selection.
const buildQuery = (src: ProviderSource): string => {
  const header = src.namespaced ? 'query ProviderConfigs($ns: String!)' : 'query ProviderConfigs';
  const listCall = src.namespaced ? `${src.listField}(namespace: $ns)` : src.listField;
  const itemFields = src.providerConfigName
    ? 'metadata { name }'
    : 'metadata { name }\n            status { providerConfigs }';
  return `
  ${header} {
    ${groupId(src.group)} {
      ${src.version} {
        ${listCall} {
          items {
            ${itemFields}
          }
        }
      }
    }
  }
`;
};

const itemsFrom = (data: unknown, src: ProviderSource): ProviderItem[] => {
  const group = (data as Record<string, unknown>)?.[groupId(src.group)] as
    | Record<string, unknown>
    | undefined;
  const version = group?.[src.version] as Record<string, unknown> | undefined;
  const list = version?.[src.listField] as { items?: ProviderItem[] } | undefined;
  return list?.items ?? [];
};

export const fetchAvailableProviders = async (
  opts: GraphqlClientOptions,
  namespace: string,
  sources: ProviderSource[]
): Promise<ProviderDiscovery> => {
  if (fixturesEnabled()) {
    return { providers: ['cloudferro', 'dati'], ran: true };
  }
  // Namespace is only needed for namespaced sources; cluster-scoped provider
  // CRs (the default) list workspace-wide.
  if (!sources.length || !opts.graphqlUrl) {
    return { providers: [], ran: false };
  }

  const found = new Set<string>();
  let ran = false;

  await Promise.all(
    sources.map(async (src) => {
      try {
        const data = await graphqlFetch<unknown>(opts, {
          query: buildQuery(src),
          variables: src.namespaced ? { ns: namespace } : {}
        });
        ran = true;
        // The query succeeded, so this source's type is present in the
        // schema — its APIExport is bound and the provider is installed,
        // regardless of whether any instance exists yet.
        if (src.providerConfigName) found.add(src.providerConfigName);
        for (const item of itemsFrom(data, src)) {
          for (const name of item.status?.providerConfigs ?? []) {
            if (name) found.add(name);
          }
        }
      } catch (err) {
        // A GraphQL-level error means the gateway answered but this provider's
        // type is absent from the schema — it is simply not installed, which is
        // a definitive (empty) result. Only a transport error is inconclusive.
        if (err instanceof GraphqlError && err.kind === 'graphql') {
          ran = true;
        }
        console.warn(
          `[simple-cluster-ui] provider discovery skipped ${src.group}/${src.version} ${src.listField}`,
          err
        );
      }
    })
  );

  return { providers: [...found].sort(), ran };
};
