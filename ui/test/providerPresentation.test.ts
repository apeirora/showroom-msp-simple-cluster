import { describe, expect, it } from 'vitest';
import { providerSource, safeLogoUrl } from '../src/lib/providerPresentation';
import { offeredProviders } from '../src/lib/providers';
import { config, source } from './providerFixture';

describe('provider presentation', () => {
  it('resolves presentation only for an exact provider config', () => {
    expect(providerSource(config, 'example')).toBe(source);
    expect(providerSource(config, 'legacy')).toBeUndefined();
  });

  it('never enables dynamic providers from an unconfirmed static fallback', () => {
    expect(offeredProviders(config, null)).toEqual([]);
    expect(offeredProviders(config, [])).toEqual([]);
    expect(offeredProviders(config, ['example'])).toEqual(['example']);
    expect(offeredProviders({ ...config, providerSources: [] }, null)).toEqual(['example', 'legacy']);
  });

  it('restricts logo sources to deployed paths, HTTP URLs, or embedded base64 SVG', () => {
    expect(safeLogoUrl('/logos/provider.svg')).toBe('/logos/provider.svg');
    expect(safeLogoUrl('https://example.test/logo.svg')).toBe('https://example.test/logo.svg');
    expect(safeLogoUrl('javascript:alert(1)')).toBeUndefined();
    expect(safeLogoUrl('//example.test/logo.svg')).toBeUndefined();
    const embedded = 'data:image/svg+xml;base64,PHN2Zy8+';
    expect(safeLogoUrl(embedded)).toBe(embedded);
    expect(safeLogoUrl('data:text/html;base64,PHNjcmlwdD4=')).toBeUndefined();
    expect(safeLogoUrl('data:image/svg+xml,<svg/>')).toBeUndefined();
  });
});
