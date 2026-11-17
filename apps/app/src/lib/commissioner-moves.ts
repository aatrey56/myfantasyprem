import type { Schemas } from '@myfantasyprem/api-client';

type Summary = Schemas['CommissionerMovesSummary'];

/** The settings section the Status banner opens (`?section=` on the settings route). */
export const MOVES_SECTION = 'commissioner-moves';

/** Where the Status banner leads: League settings, scrolled to the commissioner's moves. */
export function movesHref(leagueId: string): string {
  return `/leagues/${leagueId}/settings?section=${MOVES_SECTION}`;
}

/**
 * The Status banner's words, or null when every move has been seen:
 * "Commissioner moves · 2 new" over the newest one's summary.
 */
export function movesBanner(
  summary: Pick<Summary, 'unseenCount' | 'latest'> | undefined,
): { title: string; detail: string | null } | null {
  if (!summary || summary.unseenCount <= 0) return null;
  return {
    title: `Commissioner moves · ${summary.unseenCount} new`,
    detail: summary.latest?.summary ?? null,
  };
}

/** Whether the settings screen should scroll to the moves: the route's `section` param. */
export function wantsMovesSection(section: string | string[] | undefined): boolean {
  return (Array.isArray(section) ? section[0] : section) === MOVES_SECTION;
}

/** "By Sam P." under a move; a deleted account's moves read "By a former manager". */
export function moveByline(move: Pick<Schemas['CommissionerMove'], 'actor'>): string {
  return move.actor.displayName ? `By ${move.actor.displayName}` : 'By a former manager';
}
