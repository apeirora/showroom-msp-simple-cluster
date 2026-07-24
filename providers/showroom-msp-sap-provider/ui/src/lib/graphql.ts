/*
 * GraphQL client for kubernetes-graphql-gateway. Uses a Bearer token from
 * Luigi context. The gateway URL handed to us is typically rooted at
 * ".../root/graphql"; to hit a non-root workspace we swap the path segment
 * before "/graphql".
 *
 * Copied faithfully from showroom-msp-simple-cluster/ui — keep in sync.
 */

import { createClient, type Client as SseClient } from 'graphql-sse';

export interface GraphqlEnvelope<T> {
  data?: T;
  errors?: Array<{ message: string }>;
}

// kind distinguishes a transport failure (gateway unreachable — inconclusive)
// from a GraphQL-level error (gateway answered; e.g. the queried type is not
// in the schema because that provider is not installed — a definitive answer).
export class GraphqlError extends Error {
  constructor(
    message: string,
    public readonly kind: 'http' | 'graphql'
  ) {
    super(message);
    this.name = 'GraphqlError';
  }
}

export interface GraphqlClientOptions {
  graphqlUrl: string;
  token: string | null;
  workspacePath?: string | null;
}

export const buildWorkspaceUrl = (
  graphqlUrl: string,
  workspacePath: string | null | undefined
): string => {
  if (!workspacePath) return graphqlUrl;
  const marker = '/graphql';
  const idx = graphqlUrl.indexOf(marker);
  if (idx === -1) return graphqlUrl;
  const head = graphqlUrl.slice(0, idx);
  const lastSlash = head.lastIndexOf('/');
  if (lastSlash === -1) return graphqlUrl;
  return `${head.slice(0, lastSlash)}/${workspacePath}${marker}${graphqlUrl.slice(idx + marker.length)}`;
};

export const graphqlFetch = async <T>(
  opts: GraphqlClientOptions,
  body: { query?: string; mutation?: string; variables?: Record<string, unknown> }
): Promise<T> => {
  const url = buildWorkspaceUrl(opts.graphqlUrl, opts.workspacePath);
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (opts.token) headers.Authorization = `Bearer ${opts.token}`;
  const resp = await fetch(url, {
    method: 'POST',
    headers,
    body: JSON.stringify(body)
  });
  if (!resp.ok) {
    throw new GraphqlError(`GraphQL HTTP ${resp.status} ${resp.statusText}`, 'http');
  }
  const envelope = (await resp.json()) as GraphqlEnvelope<T>;
  if (envelope.errors?.length) {
    throw new GraphqlError(envelope.errors.map((e) => e.message).join('; '), 'graphql');
  }
  if (!envelope.data) {
    throw new GraphqlError('GraphQL response missing data', 'graphql');
  }
  return envelope.data;
};

export const subscriptionClient = (opts: GraphqlClientOptions): SseClient =>
  createClient({
    url: buildWorkspaceUrl(opts.graphqlUrl, opts.workspacePath),
    headers: () => {
      const headers: Record<string, string> = {};
      if (opts.token) headers.Authorization = `Bearer ${opts.token}`;
      return headers;
    }
  });

export type SubscriptionHandle = () => void;

export const subscribe = (
  opts: GraphqlClientOptions,
  query: string,
  variables: Record<string, unknown>,
  onEvent: () => void
): SubscriptionHandle => {
  try {
    const client = subscriptionClient(opts);
    return client.subscribe(
      { query, variables },
      {
        next: () => onEvent(),
        error: (error) => console.warn('[sapprovider-ui] subscription error', error),
        complete: () => undefined
      }
    );
  } catch (err) {
    console.warn('[sapprovider-ui] failed to open subscription, will fall back to polling', err);
    return () => {};
  }
};
