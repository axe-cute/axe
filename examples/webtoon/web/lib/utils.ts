/**
 * Deduplicate an array by a key extractor; keeps the first occurrence.
 * Defensive helper for list renders where backend or race conditions may
 * briefly produce duplicate IDs (e.g. React Strict Mode double-fetch).
 */
export function dedupeBy<T, K>(arr: T[], keyFn: (item: T) => K): T[] {
  const seen = new Map<K, T>();
  for (const item of arr) {
    const k = keyFn(item);
    if (!seen.has(k)) seen.set(k, item);
  }
  return Array.from(seen.values());
}
