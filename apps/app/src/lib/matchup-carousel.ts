import type { Schemas } from '@myfantasyprem/api-client';

import { findMyMatchup, teamOf, type AnySide } from './matchup';

/**
 * The Match tab's header carousel (Sleeper's): every matchup in the
 * gameweek, mine first, the rest in the order the server lists them.
 */
export function carouselOrder<M extends { id: string; home: AnySide; away: AnySide }>(
  matchups: readonly M[],
  myTeamId: string | undefined,
): M[] {
  const mine = findMyMatchup(matchups, myTeamId);
  return mine ? [mine, ...matchups.filter((m) => m.id !== mine.id)] : [...matchups];
}

/** The page showing `matchupId`; 0 (the first, mine) when it is not in the order. */
export function pageOf(order: readonly { id: string }[], matchupId: string | undefined): number {
  const i = matchupId === undefined ? -1 : order.findIndex((m) => m.id === matchupId);
  return i < 0 ? 0 : i;
}

/** The matchup on `page`, clamped to the order; undefined when it is empty. */
export function matchupAt<M>(order: readonly M[], page: number): M | undefined {
  if (order.length === 0) return undefined;
  return order[Math.min(Math.max(Math.round(page), 0), order.length - 1)];
}

/** The pages either side of `page` (to prefetch), within the order. */
export function neighbourPages(count: number, page: number): number[] {
  return [page - 1, page + 1].filter((p) => p >= 0 && p < count);
}

/** Season numbers under a header side: start/sit accuracy, and average points for and against a gameweek. */
export interface SeasonLine {
  /** Points conceded over gameweeks played, one decimal; null before any. */
  against: number | null;
  /** 0–1, null before any completed gameweek. */
  accuracy: number | null;
  /** Points for over gameweeks played, one decimal; null before any. */
  average: number | null;
}

/**
 * Season lines by team id: accuracy from the start/sit endpoint, average
 * points from the standings (they count completed gameweeks only).
 */
export function seasonLines(
  standings:
    | readonly Pick<Schemas['StandingRow'], 'team' | 'played' | 'pointsFor' | 'pointsAgainst'>[]
    | undefined,
  startSit: readonly Pick<Schemas['TeamStartSit'], 'team' | 'seasonAccuracy'>[] | undefined,
): ReadonlyMap<string, SeasonLine> {
  const out = new Map<string, SeasonLine>();
  for (const r of standings ?? [])
    out.set(r.team.id, {
      against: r.played > 0 ? Math.round((r.pointsAgainst / r.played) * 10) / 10 : null,
      accuracy: null,
      average: r.played > 0 ? Math.round((r.pointsFor / r.played) * 10) / 10 : null,
    });
  for (const t of startSit ?? []) {
    const line = out.get(t.team.id) ?? { against: null, accuracy: null, average: null };
    out.set(t.team.id, { ...line, accuracy: t.seasonAccuracy });
  }
  return out;
}

/** `87%` / `–`. */
export function accuracyLabel(accuracy: number | null | undefined): string {
  return accuracy === null || accuracy === undefined ? '–' : `${Math.round(accuracy * 100)}%`;
}

/** `54.3` / `–`. */
export function averageLabel(average: number | null | undefined): string {
  return average === null || average === undefined ? '–' : average.toFixed(1);
}

/** The side's season line, if it is a team with one. */
export function seasonLineOf(
  side: AnySide,
  lines: ReadonlyMap<string, SeasonLine>,
): SeasonLine | undefined {
  const t = teamOf(side);
  return t ? lines.get(t.id) : undefined;
}
