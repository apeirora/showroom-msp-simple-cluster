/*
 * Fetch the admin kubeconfig Secret referenced by a SimpleCluster and trigger a
 * browser download. The Secret lives in the same customer workspace/namespace
 * as the SimpleCluster — routed through the same graphql-gateway endpoint.
 */

import { graphqlFetch, type GraphqlClientOptions } from './graphql';
import { fixturesEnabled } from './fixtures';

// kubernetes-graphql-gateway exposes core/v1 resources directly under the
// top-level `v1` field — there is no `core` wrapper on the root Query.
// `v1.Secret(namespace, name)` returns the Secret with its base64 `data` map.
const SECRET_QUERY = `
  query Secret($ns: String!, $name: String!) {
    v1 {
      Secret(namespace: $ns, name: $name) {
        metadata { name }
        data
      }
    }
  }
`;

interface SecretResponse {
  v1: {
    Secret: {
      metadata: { name: string };
      data?: Record<string, string>;
    } | null;
  };
}

export const fetchKubeconfig = async (
  opts: GraphqlClientOptions,
  namespace: string,
  secretName: string
): Promise<string> => {
  if (fixturesEnabled()) {
    return `apiVersion: v1
kind: Config
# fixture kubeconfig for ${secretName}
clusters: []
users: []
contexts: []
`;
  }
  const data = await graphqlFetch<SecretResponse>(opts, {
    query: SECRET_QUERY,
    variables: { ns: namespace, name: secretName }
  });
  const secret = data.v1?.Secret;
  if (!secret) throw new Error(`Secret ${namespace}/${secretName} not found`);
  const b64 = secret.data?.kubeconfig ?? secret.data?.['kubeconfig.yaml'] ?? secret.data?.value;
  if (!b64) throw new Error(`Secret ${namespace}/${secretName} has no 'kubeconfig' data key`);
  try {
    return atob(b64);
  } catch (err) {
    throw new Error(`Failed to decode kubeconfig: ${(err as Error).message}`);
  }
};
