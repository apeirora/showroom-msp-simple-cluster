/*
 * Fetch the per-VM SSH keypair Secret the operator generates and return the
 * decoded private key. The Secret is mirrored by the sync-agent into the same
 * workspace/namespace as the IronCoreVM — routed through the same
 * graphql-gateway endpoint. Mirrors the CloudFerro provider's ui/src/lib/secret.ts.
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

export const fetchSshPrivateKey = async (
  opts: GraphqlClientOptions,
  namespace: string,
  secretName: string
): Promise<string> => {
  if (fixturesEnabled()) {
    return `-----BEGIN OPENSSH PRIVATE KEY-----
fixture-private-key-for-${secretName}
-----END OPENSSH PRIVATE KEY-----
`;
  }
  const data = await graphqlFetch<SecretResponse>(opts, {
    query: SECRET_QUERY,
    variables: { ns: namespace, name: secretName }
  });
  const secret = data.v1?.Secret;
  if (!secret) throw new Error(`Secret ${namespace}/${secretName} not found`);
  const b64 = secret.data?.['ssh-privatekey'];
  if (!b64) throw new Error(`Secret ${namespace}/${secretName} has no 'ssh-privatekey' data key`);
  try {
    return atob(b64);
  } catch (err) {
    throw new Error(`Failed to decode private key: ${(err as Error).message}`);
  }
};
