import type { Schemas } from '@myfantasyprem/api-client';

type Entry = Schemas['TradeBlockEntry'];

/** Positions in the order the block shows them. */
export const BLOCK_POSITIONS: readonly Schemas['Position'][] = ['GKP', 'DEF', 'MID', 'FWD'];

/** A note may be this long (characters), as the server checks. */
export const MAX_NOTE = 140;

/** "Wants GKP, MID", or "Open to anything" when the team named no position. */
export function wantedText(wanted: readonly string[]): string {
  return wanted.length ? `Wants ${wanted.join(', ')}` : 'Open to anything';
}

export interface BlockGroup {
  team: Schemas['TeamRef'];
  entries: Entry[];
}

/**
 * The block table: one group per team, my team first, then by team name;
 * inside a team the players stay in block order (oldest first).
 */
export function groupByTeam(entries: readonly Entry[], myTeamId?: string): BlockGroup[] {
  const groups = new Map<string, BlockGroup>();
  for (const e of entries) {
    const g = groups.get(e.team.id) ?? { team: e.team, entries: [] };
    g.entries.push(e);
    groups.set(e.team.id, g);
  }
  return [...groups.values()].sort(
    (a, b) =>
      Number(b.team.id === myTeamId) - Number(a.team.id === myTeamId) ||
      a.team.name.localeCompare(b.team.name),
  );
}

/** Other teams' entries, newest first: the carousel, which leads with what is new. */
export function carouselEntries(entries: readonly Entry[], myTeamId?: string): Entry[] {
  return entries
    .filter((e) => e.team.id !== myTeamId)
    .sort((a, b) => b.addedAt.localeCompare(a.addedAt));
}

/** The trade builder, opened on the team with the player already asked for. */
export function tradeHref(leagueId: string, e: Pick<Entry, 'team' | 'player'>): string {
  return `/leagues/${leagueId}/players/trades/new?teamId=${e.team.id}&playerId=${e.player.id}`;
}
