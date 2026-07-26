/*
 * Fetch the generated root-password Secret (kubernetes.io/basic-auth) the
 * operator writes for a machine and return the decoded password. The Secret is
 * mirrored by the sync-agent into the machine's workspace/namespace — routed
 * through the same graphql-gateway endpoint. Mirrors
 * showroom-msp-simple-cluster/ui/lib/secret.ts.
 */

import { graphqlFetch, type GraphqlClientOptions } from './graphql';
import { fixturesEnabled } from './fixtures';

// kubernetes-graphql-gateway exposes core/v1 resources directly under the
// top-level `v1` field. `v1.Secret(namespace, name)` returns the Secret with
// its base64 `data` map.
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

export interface Credential {
  username: string;
  password: string;
}

export const fetchPassword = async (
  opts: GraphqlClientOptions,
  namespace: string,
  secretName: string
): Promise<Credential> => {
  if (fixturesEnabled()) {
    return { username: 'root', password: `fixture-password-${secretName}` };
  }
  const data = await graphqlFetch<SecretResponse>(opts, {
    query: SECRET_QUERY,
    variables: { ns: namespace, name: secretName }
  });
  const secret = data.v1?.Secret;
  if (!secret) throw new Error(`Secret ${namespace}/${secretName} not found`);
  // kubernetes.io/basic-auth keys.
  const pwB64 = secret.data?.password;
  if (!pwB64) throw new Error(`Secret ${namespace}/${secretName} has no 'password' data key`);
  const userB64 = secret.data?.username;
  try {
    return { username: userB64 ? atob(userB64) : 'root', password: atob(pwB64) };
  } catch (err) {
    throw new Error(`Failed to decode password: ${(err as Error).message}`);
  }
};
