import type { Schemas } from '@myfantasyprem/api-client';

type Txn = Schemas['Transaction'];

export type ActivityFilter = 'all' | 'waiver' | 'free_agent' | 'trade';

export const ACTIVITY_FILTERS: readonly { value: ActivityFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'waiver', label: 'Waivers' },
  { value: 'free_agent', label: 'Free agents' },
  { value: 'trade', label: 'Trades' },
];

/** The `kind` query parameter for a filter chip; undefined asks for everything. */
export function kindFor(filter: ActivityFilter): Schemas['TransactionKind'] | undefined {
  return filter === 'all' ? undefined : filter;
}

/** Client-side guard, so a page cached under another filter never leaks into this one. */
export function matchesFilter(t: Pick<Txn, 'kind'>, filter: ActivityFilter): boolean {
  return filter === 'all' || t.kind === filter;
}

/**
 * The feed as one list from cursor pages: newest first, each entry once.
 * Offset cursors can repeat an entry when new moves land between pages.
 */
export function flattenFeed(
  pages: readonly { data: readonly Txn[] }[] | undefined,
  filter: ActivityFilter,
): Txn[] {
  const seen = new Set<string>();
  const out: Txn[] = [];
  for (const page of pages ?? []) {
    for (const t of page.data) {
      if (seen.has(t.id) || !matchesFilter(t, filter)) continue;
      seen.add(t.id);
      out.push(t);
    }
  }
  return out;
}

export type FeedEntry =
  | { kind: 'transaction'; id: string; gameweek: number | null; at: string; txn: Txn }
  | { kind: 'draft'; id: string; gameweek: number | null; at: string };

export interface FeedGroup {
  /** Null for the draft: its completion and its missed-clock autopicks. */
  gameweek: number | null;
  entries: FeedEntry[];
}

/**
 * Groups the feed by gameweek, newest first, keeping feed order inside each
 * group. The draft closes the list as its own "Draft" group: its completion
 * (when shown) and then its missed-clock autopicks, newest first.
 */
export function groupFeed(
  txns: readonly Txn[],
  draftCompletedAt: string | null = null,
): FeedGroup[] {
  const groups: FeedGroup[] = [];
  const draft: FeedEntry[] = draftCompletedAt
    ? [{ kind: 'draft', id: 'draft', gameweek: null, at: draftCompletedAt }]
    : [];
  for (const t of txns) {
    const entry: FeedEntry = {
      kind: 'transaction',
      id: t.id,
      gameweek: t.gameweek,
      at: t.occurredAt,
      txn: t,
    };
    if (t.kind === 'draft_autopick') {
      draft.push({ ...entry, gameweek: null });
      continue;
    }
    const last = groups.at(-1);
    if (last && last.gameweek === t.gameweek) last.entries.push(entry);
    else groups.push({ gameweek: t.gameweek, entries: [entry] });
  }
  if (draft.length > 0) groups.push({ gameweek: null, entries: draft });
  return groups;
}

/** One line for screen readers and the trade card header. */
export function describeTransaction(t: Txn): string {
  switch (t.kind) {
    case 'waiver':
    case 'free_agent': {
      const how = t.kind === 'waiver' ? 'claimed' : 'added';
      if (t.result.outcome === 'denied') {
        return `${t.team.name}'s claim for ${t.playerIn.displayName} failed`;
      }
      return `${t.team.name} ${how} ${t.playerIn.displayName} and dropped ${t.playerOut.displayName}`;
    }
    case 'trade': {
      const verb = t.status === 'vetoed' ? 'trade was vetoed' : 'traded';
      const give = t.offered.map((p) => p.displayName).join(', ');
      const get = t.requested.map((p) => p.displayName).join(', ');
      return t.status === 'vetoed'
        ? `${t.offeringTeam.name} and ${t.receivingTeam.name}'s ${verb}: ${give} for ${get}`
        : `${t.offeringTeam.name} and ${t.receivingTeam.name} ${verb} ${give} for ${get}`;
    }
    case 'draft_autopick':
      return `${t.team.name}'s clock ran out: autopicked ${t.player.displayName}`;
    default:
      return 'League activity';
  }
}

/** Why a claim failed, for the activity row. */
export function deniedText(reason: string): string {
  switch (reason) {
    case 'player_in_unavailable':
      return 'Player already taken';
    case 'player_out_unavailable':
      return 'Dropped player had already left';
    default:
      return 'Claim failed';
  }
}
