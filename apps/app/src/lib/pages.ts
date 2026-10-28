/**
 * Flattens infinite-query pages, keeping the first copy of each item.
 * Cursors are offsets, so an item added or removed between two page loads
 * shifts the list and the next page can repeat (or skip) one.
 */
export function flattenUnique<T>(
  pages: readonly { data: readonly T[] }[] | undefined,
  idOf: (item: T) => string,
): T[] {
  const seen = new Set<string>();
  const out: T[] = [];
  for (const page of pages ?? []) {
    for (const item of page.data) {
      const id = idOf(item);
      if (seen.has(id)) continue;
      seen.add(id);
      out.push(item);
    }
  }
  return out;
}
