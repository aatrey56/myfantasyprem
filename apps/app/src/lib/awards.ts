/**
 * Player of the Week and Team of the Week rules. Pure functions: the MSW
 * mocks compute the awards endpoints with them, and the Go server mirrors
 * them (server/internal/scoring/awards.go). Both test suites run
 * server/internal/scoring/testdata/awards_cases.json: change a rule there
 * and on both sides.
 *
 * The pool is every Premier League player in the game, owned or not.
 */

/** A player's gameweek numbers, as in the contract's `AwardStats`. */
export interface AwardStatLine {
  points: number;
  bonus: number;
  bps: number;
  goals: number;
  assists: number;
  defensiveContribution: number;
  cleanSheets: number;
  minutes: number;
}

export interface AwardCandidate {
  /** Stable id: the last resort when every rule ties, so results never flicker. */
  id: string;
  /** Open enum (GKP, DEF, MID, FWD). Unknown positions can win POTW but not a TOTW place. */
  position: string;
  stats: AwardStatLine;
}

/** Higher is better for each of these, in this order. */
const HIGHER_FIRST: readonly (keyof AwardStatLine)[] = [
  'points',
  'goals',
  'assists',
  'bonus',
  'bps',
  'defensiveContribution',
  'cleanSheets',
];

/**
 * Award order: negative when `a` ranks ahead of `b`. Points, then goals,
 * assists, bonus, BPS, defensive contribution and clean sheets (more is
 * better), then fewer minutes. Ties after that fall back to the id.
 */
export function compareForAward(a: AwardCandidate, b: AwardCandidate): number {
  for (const key of HIGHER_FIRST) {
    const d = b.stats[key] - a.stats[key];
    if (d !== 0) return d;
  }
  const minutes = a.stats.minutes - b.stats.minutes;
  if (minutes !== 0) return minutes;
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

/** The gameweek's best player, or null for an empty pool. */
export function playerOfTheWeek<T extends AwardCandidate>(pool: readonly T[]): T | null {
  let best: T | null = null;
  for (const c of pool) if (!best || compareForAward(c, best) < 0) best = c;
  return best;
}

export type TotwPosition = 'GKP' | 'DEF' | 'MID' | 'FWD';

/** Team of the Week shape: exactly one keeper; outfield lines between min and max. */
export const TOTW_LIMITS: Record<TotwPosition, { min: number; max: number }> = {
  GKP: { min: 1, max: 1 },
  DEF: { min: 3, max: 5 },
  MID: { min: 2, max: 5 },
  FWD: { min: 1, max: 3 },
};

export const TOTW_SIZE = 11;
export const TOTW_ORDER: readonly TotwPosition[] = ['GKP', 'DEF', 'MID', 'FWD'];

const isTotwPosition = (p: string): p is TotwPosition => p in TOTW_LIMITS;

/**
 * Team of the Week: the best keeper, three defenders, two midfielders and
 * one forward, then the best four remaining outfield players that keep
 * every line within its cap (DEF 5, MID 5, FWD 3). Returned keeper first,
 * then defenders, midfielders and forwards, best first within each.
 *
 * A pool too thin to fill a line yields fewer than 11 rather than breaking
 * the caps.
 */
export function teamOfTheWeek<T extends AwardCandidate>(pool: readonly T[]): T[] {
  const ranked = pool.filter((c) => isTotwPosition(c.position)).sort(compareForAward);
  const picked = new Set<T>();
  const count: Record<TotwPosition, number> = { GKP: 0, DEF: 0, MID: 0, FWD: 0 };
  const take = (c: T) => {
    picked.add(c);
    count[c.position as TotwPosition]++;
  };
  // The minimum of each line first.
  for (const pos of TOTW_ORDER) {
    for (const c of ranked) {
      if (count[pos] >= TOTW_LIMITS[pos].min) break;
      if (c.position === pos) take(c);
    }
  }
  // Then the best of the rest, within the caps.
  for (const c of ranked) {
    if (picked.size >= TOTW_SIZE) break;
    const pos = c.position as TotwPosition;
    if (picked.has(c) || pos === 'GKP' || count[pos] >= TOTW_LIMITS[pos].max) continue;
    take(c);
  }
  return TOTW_ORDER.flatMap((pos) => ranked.filter((c) => picked.has(c) && c.position === pos));
}

/** "4-4-2" style shape of a Team of the Week (for tests and labels). */
export function formationOf(team: readonly AwardCandidate[]): string {
  const n = (pos: TotwPosition) => team.filter((c) => c.position === pos).length;
  return `${n('DEF')}-${n('MID')}-${n('FWD')}`;
}
