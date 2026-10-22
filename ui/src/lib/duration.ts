/*
 * Go duration strings, the format of SimpleCluster.spec.encryption.provider
 * .keyRotationPeriod. The API validates the field with CEL's duration(),
 * which parses like Go's time.ParseDuration: an optional sign, then a
 * sequence of decimal numbers each with a unit (h, m, s, ms, us, ns), such
 * as "720h" or "1h30m"; "0" alone is allowed; the total must fit a signed
 * 64-bit nanosecond count; fractional nanoseconds are truncated. Validating
 * here with the same rules spares the user a round trip for a typo without
 * accepting something the API would refuse.
 */

const UNIT_NS: Record<string, bigint> = {
  h: 3_600_000_000_000n,
  m: 60_000_000_000n,
  s: 1_000_000_000n,
  ms: 1_000_000n,
  us: 1_000n,
  'µs': 1_000n,
  'μs': 1_000n,
  ns: 1n
};

// Go's int64 nanosecond range (about 292 years).
const MAX_NS = 9_223_372_036_854_775_807n;

// One number-with-unit token, anchored at lastIndex (sticky). "ms" is listed
// before "m" and the "us" spellings before "s" so the longest unit wins.
const TOKEN = /(\d*)(?:\.(\d*))?(h|ms|m|s|us|µs|μs|ns)/y;

/** Nanoseconds for a Go duration string, or null when Go would reject it. */
export const parseGoDuration = (text: string): bigint | null => {
  let s = text.trim();
  if (!s) return null;
  let negative = false;
  if (s[0] === '+' || s[0] === '-') {
    negative = s[0] === '-';
    s = s.slice(1);
  }
  if (s === '0') return 0n;
  if (!s) return null;
  let pos = 0;
  let total = 0n;
  while (pos < s.length) {
    TOKEN.lastIndex = pos;
    const m = TOKEN.exec(s);
    if (!m) return null;
    const [, whole, fraction, unit] = m;
    if (!whole && !fraction) return null;
    const unitNs = UNIT_NS[unit];
    let value = BigInt(whole || '0') * unitNs;
    if (value > MAX_NS) return null;
    if (fraction) {
      // Go accumulates the fraction in floating point and truncates.
      const f = Number(fraction) / 10 ** fraction.length;
      value += BigInt(Math.floor(f * Number(unitNs)));
    }
    total += value;
    if (total > MAX_NS) return null;
    pos = TOKEN.lastIndex;
  }
  return negative ? -total : total;
};

/** The CRD refuses a key rotation period below one hour. */
export const MIN_KEY_ROTATION_NS = UNIT_NS.h;

/**
 * Empty is valid (no automatic rotation, the field is left out); anything
 * else must parse and be at least one hour.
 */
export const keyRotationPeriodValid = (text: string): boolean => {
  if (!text.trim()) return true;
  const ns = parseGoDuration(text);
  return ns !== null && ns >= MIN_KEY_ROTATION_NS;
};
