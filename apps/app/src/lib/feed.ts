import type { Schemas } from '@myfantasyprem/api-client';

type Txn = Schemas['Transaction'];

export interface FeedItem {
  league: { id: string; name: string };
  txn: Txn;
}

/**
 * The app-wide feed: every league's latest moves in one list, newest
 * first, each move once (a league's pages can repeat one), at most `limit`.
 */
export function mergeLeagueFeeds(
  feeds: readonly { league: { id: string; name: string }; txns: readonly Txn[] }[],
  limit = 50,
): FeedItem[] {
  const seen = new Set<string>();
  const out: FeedItem[] = [];
  for (const { league, txns } of feeds) {
    for (const txn of txns) {
      const key = `${league.id}/${txn.id}`;
      if (seen.has(key)) continue;
      seen.add(key);
      out.push({ league, txn });
    }
  }
  out.sort(
    (a, b) =>
      Date.parse(b.txn.occurredAt) - Date.parse(a.txn.occurredAt) ||
      a.league.name.localeCompare(b.league.name),
  );
  return out.slice(0, limit);
}
