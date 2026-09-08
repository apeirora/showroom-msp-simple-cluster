// Gateway JSONString scalars may contain a JSON-encoded Quantity string,
// while ordinary Kubernetes/fixture values already contain the plain quantity.
// Decode only that outer string wrapper for display. Do not change stored data,
// validate quantities here, or alter the create mutation's JSONString encoding.
export const displayQuantity = (value?: string | number | null): string => {
  if (value == null) return '—';
  if (typeof value === 'string') {
    try {
      const decoded: unknown = JSON.parse(value);
      if (typeof decoded === 'string') return decoded;
    } catch {
      // Plain quantities such as 500m and 4Gi are not JSON documents.
    }
  }
  return String(value);
};
