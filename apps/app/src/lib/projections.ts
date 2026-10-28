/**
 * Fixture difficulty, Expected points, win odds and start/sit accuracy
 * (docs/adr/0014-projections.md). The server computes them; this file holds
 * the display rules the screens use and the app's copy of the formulas the
 * mocks use, mirroring `server/internal/projection`. Nothing here is a
 * forecast model: every number is arithmetic on real past points.
 */
import type { Schemas } from '@myfantasyprem/api-client';

import { ordinal } from './leagues';

type Position = Schemas['Position'];

/**
 * Whether Expected points and win chances show (owner, 2026-10-17: hidden
 * for now). Off unless the build sets EXPO_PUBLIC_SHOW_PROJECTIONS=true;
 * the server endpoint stays. Fixture difficulty and start/sit accuracy are
 * not projections and always show. Read at call time so tests can set it.
 */
export function showProjections(): boolean {
  return process.env.EXPO_PUBLIC_SHOW_PROJECTIONS === 'true';
}

/** Completed gameweeks fixture difficulty and win odds look back over. */
export const RECENT_WEEKS = 6;

/** Plays per win-odds simulation. */
export const SAMPLES = 10_000;

/** The label every win chance carries. */
export const ODDS_BASIS = 'Based on recent scores';

/** What Expected is, for its label. */
export const EXPECTED_BASIS = '2-GW form × fixture';

/** The last RECENT_WEEKS completed gameweeks before `before`, oldest first. */
export function recentGameweeks(before: number, completed: (gw: number) => boolean): number[] {
  const out: number[] = [];
  for (let gw = before - 1; gw >= 1 && out.length < RECENT_WEEKS; gw--) {
    if (completed(gw)) out.unshift(gw);
  }
  return out;
}

// ------------------------------------------------------------ difficulty

export interface DifficultyMatch {
  fixtureId: string;
  home: string;
  away: string;
}

/** One player's points in one match: `club` played, the other club conceded them. */
export interface DifficultyLine {
  fixtureId: string;
  club: string;
  position: Position;
  points: number;
}

export interface ClubRanking {
  club: string;
  /** 1 = concedes least to the position (hardest) … 20 = most (easiest). */
  rank: number;
  conceded: number;
  matches: number;
}

const POSITIONS: readonly Position[] = ['GKP', 'DEF', 'MID', 'FWD'];

/**
 * Ranks clubs per position by points conceded per match, fewest first;
 * equal per-match figures share the better rank (1, 2, 2, 4).
 */
export function rankDifficulty(
  matches: readonly DifficultyMatch[],
  lines: readonly DifficultyLine[],
): Map<Position, Map<string, ClubRanking>> {
  const byFixture = new Map<string, DifficultyMatch>();
  const played = new Map<string, number>();
  for (const m of matches) {
    if (byFixture.has(m.fixtureId)) continue;
    byFixture.set(m.fixtureId, m);
    played.set(m.home, (played.get(m.home) ?? 0) + 1);
    played.set(m.away, (played.get(m.away) ?? 0) + 1);
  }
  const conceded = new Map<string, number>();
  for (const l of lines) {
    const m = byFixture.get(l.fixtureId);
    if (!m) continue;
    const against = l.club === m.home ? m.away : l.club === m.away ? m.home : null;
    if (against === null) continue;
    const key = `${l.position}:${against}`;
    conceded.set(key, (conceded.get(key) ?? 0) + l.points);
  }
  const out = new Map<Position, Map<string, ClubRanking>>();
  for (const pos of POSITIONS) {
    const rows = [...played].map(([club, n]) => ({
      club,
      rank: 0,
      conceded: conceded.get(`${pos}:${club}`) ?? 0,
      matches: n,
    }));
    const perMatch = (a: ClubRanking, b: ClubRanking) =>
      Math.sign(a.conceded * b.matches - b.conceded * a.matches);
    rows.sort((a, b) => perMatch(a, b) || (a.club < b.club ? -1 : a.club > b.club ? 1 : 0));
    const ranked = new Map<string, ClubRanking>();
    rows.forEach((r, i) => {
      const prev = rows[i - 1];
      r.rank = prev && perMatch(prev, r) === 0 ? ranked.get(prev.club)!.rank : i + 1;
      ranked.set(r.club, r);
    });
    out.set(pos, ranked);
  }
  return out;
}

/** 0.8 against the hardest (1st) to 1.2 against the easiest (20th); 1 unranked. */
export function fixtureFactor(rank: number | null | undefined): number {
  if (!rank || rank < 1) return 1;
  return 0.8 + (0.4 * (Math.min(rank, 20) - 1)) / 19;
}

/** One decimal, halves up (form's rounding); the epsilon absorbs binary error. */
const round1 = (x: number) => Math.floor(x * 10 + 0.5 + 1e-9) / 10;

/** Form × the sum of the fixture factors of the gameweek's matches. */
export function expectedPoints(form: number, ranks: readonly (number | null)[]): number {
  return round1(form * ranks.reduce<number>((s, r) => s + fixtureFactor(r), 0));
}

/**
 * The chip's colour band for a rank: 1 hardest (1st-4th) … 5 easiest
 * (17th-20th), Sleeper's red-to-green.
 */
export function difficultyTier(rank: number): 1 | 2 | 3 | 4 | 5 {
  return Math.min(5, Math.max(1, Math.ceil(rank / 4))) as 1 | 2 | 3 | 4 | 5;
}

/** "(18th)" next to a fixture. */
export const difficultyText = (rank: number) => `(${ordinal(rank)})`;

/** Spoken form of a chip: the rank and what it means. */
export function difficultyA11y(rank: number): string {
  const tier = difficultyTier(rank);
  const word =
    tier === 1
      ? 'very hard'
      : tier === 2
        ? 'hard'
        : tier === 3
          ? 'average'
          : tier === 4
            ? 'easy'
            : 'very easy';
  return `opponent ranked ${ordinal(rank)} of 20 by points conceded, ${word}`;
}

/** A lookup `position:CLUB → rank` from GET /fixture-difficulty. */
export function difficultyLookup(d: Schemas['FixtureDifficulty'] | undefined) {
  const map = new Map<string, number>();
  for (const p of d?.positions ?? []) {
    for (const c of p.clubs) map.set(`${p.position}:${c.club.code}`, c.rank);
  }
  return (position: Position, club: string): number | null =>
    map.get(`${position}:${club}`) ?? null;
}

// ------------------------------------------------------------ expected

/** Expected as shown: one decimal, a dash when there is none. */
export function expectedText(e: number | null | undefined): string {
  return e === null || e === undefined ? '–' : e.toFixed(1);
}

// ------------------------------------------------------------ win odds

export interface SimStarter {
  /** Points already scored this gameweek. */
  actual: number;
  /** Matches not yet kicked off: one draw from history each. */
  remaining: number;
  /** His points in each recent completed gameweek (0 when he did not play). */
  history: readonly number[];
}

export interface SimTeam {
  id: string;
  starters: readonly SimStarter[];
}

export interface Odds {
  home: number;
  draw: number;
  away: number;
}

/** FNV-1a, 32-bit. */
function fnv(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  return h >>> 0;
}

/** The seed of a matchup's plays: same matchup, gameweek and bucket, same odds. */
export const oddsSeed = (matchupId: string, gw: number, bucket: number) =>
  fnv(`${matchupId}|${gw}|${bucket}`);

function mulberry32(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/**
 * Plays the gameweek n times (ADR 0014): each starter scores his actual
 * points plus one random recent gameweek per match still to play. `null`
 * is the league-average side: floor(mean of every team). Each team has its
 * own random stream, so swapping the sides swaps the odds exactly.
 */
export function winOdds(
  teams: readonly SimTeam[],
  homeId: string | null,
  awayId: string | null,
  seed: number,
  n = SAMPLES,
): Odds {
  if (n <= 0) return { home: 0, draw: 0, away: 0 };
  const all = homeId === null || awayId === null;
  const play = teams
    .filter((t) => all || t.id === homeId || t.id === awayId)
    .map((t) => ({ t, rng: mulberry32(seed ^ fnv(t.id)) }));
  const totals = new Map<string, number>();
  let home = 0;
  let draw = 0;
  let away = 0;
  for (let i = 0; i < n; i++) {
    let sum = 0;
    for (const { t, rng } of play) {
      let total = 0;
      for (const s of t.starters) {
        total += s.actual;
        if (s.history.length === 0) continue;
        for (let k = 0; k < s.remaining; k++) {
          total += s.history[Math.floor(rng() * s.history.length)]!;
        }
      }
      totals.set(t.id, total);
      sum += total;
    }
    const avg = play.length ? Math.floor(sum / play.length) : 0;
    const side = (id: string | null) => (id === null ? avg : (totals.get(id) ?? 0));
    const h = side(homeId);
    const a = side(awayId);
    if (h > a) home++;
    else if (h < a) away++;
    else draw++;
  }
  return { home: home / n, draw: draw / n, away: away / n };
}

/** A finished gameweek's odds: the result. */
export function finalOdds(home: number, away: number): Odds {
  return home > away
    ? { home: 1, draw: 0, away: 0 }
    : home < away
      ? { home: 0, draw: 0, away: 1 }
      : { home: 0, draw: 1, away: 0 };
}

/** A chance as a whole percent; never shows 0% or 100% for something still possible. */
export function percentText(p: number): string {
  if (p <= 0) return '0%';
  if (p >= 1) return '100%';
  const pct = Math.round(p * 100);
  if (pct === 0) return '<1%';
  if (pct === 100) return '>99%';
  return `${pct}%`;
}

/** The odds seen from the left and right of a card (my team on the left). */
export function oddsFor(odds: Odds, leftIsHome: boolean) {
  return leftIsHome
    ? { left: odds.home, right: odds.away, draw: odds.draw }
    : { left: odds.away, right: odds.home, draw: odds.draw };
}

// ------------------------------------------------------------ start/sit

export interface Scored {
  id: string;
  position: Position;
  points: number;
}

const MIN: Record<Position, number> = { GKP: 1, DEF: 3, MID: 2, FWD: 1 };
const MAX: Record<Position, number> = { GKP: 1, DEF: 5, MID: 5, FWD: 3 };

/** The best legal XI by actual points (1 GKP, 3-5 DEF, 2-5 MID, 1-3 FWD), or null. */
export function optimalXI(squad: readonly Scored[]): { ids: string[]; points: number } | null {
  const by = new Map<Position, Scored[]>(POSITIONS.map((p) => [p, []]));
  for (const p of squad) by.get(p.position)?.push(p);
  for (const list of by.values()) {
    list.sort((a, b) => b.points - a.points || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
  }
  let best: { counts: number[]; points: number } | null = null;
  const tryCounts = (i: number, left: number, counts: number[]) => {
    if (i === POSITIONS.length) {
      if (left !== 0) return;
      const points = POSITIONS.reduce(
        (s, pos, j) =>
          s +
          by
            .get(pos)!
            .slice(0, counts[j])
            .reduce((t, p) => t + p.points, 0),
        0,
      );
      if (!best || points > best.points) best = { counts: [...counts], points };
      return;
    }
    const pos = POSITIONS[i]!;
    const top = Math.min(MAX[pos], by.get(pos)!.length, left);
    for (let c = MIN[pos]; c <= top; c++) tryCounts(i + 1, left - c, [...counts, c]);
  };
  tryCounts(0, 11, []);
  if (!best) return null;
  const { counts, points } = best as { counts: number[]; points: number };
  const ids = POSITIONS.flatMap((pos, j) =>
    by
      .get(pos)!
      .slice(0, counts[j])
      .map((p) => p.id),
  );
  return { ids, points };
}

/** Points as a share of the optimal XI's; null when the optimal is 0 or less. */
export function accuracy(points: number, optimal: number): number | null {
  return optimal > 0 ? points / optimal : null;
}

/** The mean of the gameweeks' accuracies; null with none. */
export function seasonAccuracy(weeks: readonly number[]): number | null {
  return weeks.length ? weeks.reduce((s, a) => s + a, 0) / weeks.length : null;
}

/** An accuracy as shown: a whole percent, a dash when there is none. */
export function accuracyText(a: number | null | undefined): string {
  return a === null || a === undefined ? '–' : `${Math.round(a * 100)}%`;
}

/** A fixture still to kick off shows its difficulty chip; a started one its score state. */
export const showsDifficulty = (f: { status?: Schemas['FixtureStatus'] }) =>
  f.status === undefined || f.status === 'scheduled';

/**
 * A team's Expected: the sum of its starters' (no auto-subs), added in
 * tenths so it matches the rows. Null when no starter has one.
 */
export function teamExpected(
  starters: readonly string[],
  expected: ReadonlyMap<string, number | null>,
): number | null {
  let tenths = 0;
  let any = false;
  for (const id of starters) {
    const e = expected.get(id);
    if (e === null || e === undefined) continue;
    tenths += Math.round(e * 10);
    any = true;
  }
  return any ? tenths / 10 : null;
}
