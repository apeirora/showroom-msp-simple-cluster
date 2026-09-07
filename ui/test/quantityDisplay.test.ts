import { describe, expect, it } from 'vitest';
import { displayQuantity } from '../src/lib/quantityDisplay';

describe('Quantity presentation', () => {
  it.each(['2', '500m', '4Gi', '1.5', '1e3', '100Mi'])('unwraps one JSONString layer for %s', (value) => {
    expect(displayQuantity(JSON.stringify(value))).toBe(value);
  });

  it.each(['2', '500m', '4Gi', '1.5', '1e3', '100Mi'])('preserves plain quantity spelling %s', (value) => {
    expect(displayQuantity(value)).toBe(value);
  });

  it.each([0, 2, 1.5])('preserves an already numeric scalar %s', (value) => {
    expect(displayQuantity(value)).toBe(String(value));
  });

  it.each([null, undefined])('shows the existing placeholder for missing %s', (value) => {
    expect(displayQuantity(value)).toBe('—');
  });

  it.each(['"4Gi', 'null', 'true', '{"cpu":"2"}', '["4Gi"]', ''])('does not reinterpret unexpected input %s', (value) => {
    expect(displayQuantity(value)).toBe(value);
  });

  it('does not recursively normalize double-encoded or malformed quantities', () => {
    expect(displayQuantity(JSON.stringify(JSON.stringify('4Gi')))).toBe('"4Gi"');
  });
});
