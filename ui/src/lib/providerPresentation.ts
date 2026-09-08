import type { ClusterConfig, ProviderSource } from '../types';

export const providerSource = (config: ClusterConfig, name: string): ProviderSource | undefined =>
  config.providerSources.find((source) => source.providerConfigName === name);

export const safeLogoUrl = (url?: string): string | undefined =>
  url && (/^https?:\/\//i.test(url) || (url.startsWith('/') && !url.startsWith('//')) ||
    /^data:image\/svg\+xml;base64,[A-Za-z0-9+/]+={0,2}$/.test(url)) ? url : undefined;
