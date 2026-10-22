import { describe, expect, it } from 'vitest';
import { keyRotationPeriodValid, parseGoDuration } from '../src/lib/duration';

const H = 3_600_000_000_000n;
const M = 60_000_000_000n;
const S = 1_000_000_000n;

describe('Go duration grammar for keyRotationPeriod', () => {
  it('parses the forms time.ParseDuration parses, in nanoseconds', () => {
    expect(parseGoDuration('720h')).toBe(720n * H);
    expect(parseGoDuration('1h30m')).toBe(90n * M);
    expect(parseGoDuration('90m')).toBe(90n * M);
    expect(parseGoDuration('1.5h')).toBe(90n * M);
    expect(parseGoDuration('1.h')).toBe(H);
    expect(parseGoDuration('.5h')).toBe(30n * M);
    expect(parseGoDuration('3600s')).toBe(H);
    expect(parseGoDuration('1500ms')).toBe(1_500_000_000n);
    expect(parseGoDuration('10us')).toBe(10_000n);
    expect(parseGoDuration('10µs')).toBe(10_000n);
    expect(parseGoDuration('5ns')).toBe(5n);
    expect(parseGoDuration('0')).toBe(0n);
    expect(parseGoDuration('+1h')).toBe(H);
    expect(parseGoDuration('-1h')).toBe(-H);
    expect(parseGoDuration('2562047h47m16.854775807s')).toBe(9_223_372_036_854_775_807n);
  });

  it('rejects what time.ParseDuration rejects', () => {
    expect(parseGoDuration('')).toBeNull();
    expect(parseGoDuration('720')).toBeNull();
    expect(parseGoDuration('h')).toBeNull();
    expect(parseGoDuration('.h')).toBeNull();
    expect(parseGoDuration('1d')).toBeNull();
    expect(parseGoDuration('1H')).toBeNull();
    expect(parseGoDuration('30 days')).toBeNull();
    expect(parseGoDuration('1h x')).toBeNull();
    expect(parseGoDuration('1hh')).toBeNull();
    expect(parseGoDuration('1h-30m')).toBeNull();
    expect(parseGoDuration('+')).toBeNull();
    expect(parseGoDuration('2562048h')).toBeNull();
    expect(parseGoDuration('2562047h47m16.854775808s')).toBeNull();
    expect(parseGoDuration('9'.repeat(400) + 'h')).toBeNull();
  });

  it('truncates fractional nanoseconds like Go instead of rounding up', () => {
    expect(parseGoDuration('3599.9999999999999s')).toBe(3_599_999_999_999n);
    expect(keyRotationPeriodValid('3599.9999999999999s')).toBe(false);
  });

  it('accepts empty (no rotation) and anything from one hour up, within range', () => {
    expect(keyRotationPeriodValid('')).toBe(true);
    expect(keyRotationPeriodValid('   ')).toBe(true);
    expect(keyRotationPeriodValid('1h')).toBe(true);
    expect(keyRotationPeriodValid('+1h')).toBe(true);
    expect(keyRotationPeriodValid('60m')).toBe(true);
    expect(keyRotationPeriodValid('3600s')).toBe(true);
    expect(keyRotationPeriodValid('720h')).toBe(true);
    expect(keyRotationPeriodValid(' 24h ')).toBe(true);
    expect(keyRotationPeriodValid('2562047h')).toBe(true);
    expect(keyRotationPeriodValid('59m')).toBe(false);
    expect(keyRotationPeriodValid('30m')).toBe(false);
    expect(keyRotationPeriodValid('3599s')).toBe(false);
    expect(keyRotationPeriodValid('0')).toBe(false);
    expect(keyRotationPeriodValid('-1h')).toBe(false);
    expect(keyRotationPeriodValid('1d')).toBe(false);
    expect(keyRotationPeriodValid('720')).toBe(false);
    expect(keyRotationPeriodValid('2562048h')).toBe(false);
  });
});
