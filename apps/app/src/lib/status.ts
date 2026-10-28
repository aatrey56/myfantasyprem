/**
 * View-model for the Status tab: pure functions from the API shapes to what
 * each section shows. The screen only lays these out.
 */
import type { Schemas } from '@myfantasyprem/api-client';

import { findMyMatchup, orient, resultFor, sideName, sidePoints } from './matchup';

type LeagueHome = Schemas['LeagueHome'];

/** 1st, 2nd, 3rd, 4th … 11th, 12th, 13th, 21st. */
export function ordinal(n: number): string {
  const mod100 = n % 100;
  if (mod100 >= 11 && mod100 <= 13) return `${n}th`;
  switch (n % 10) {
    case 1:
      return `${n}st`;
    case 2:
      return `${n}nd`;
    case 3:
      return `${n}rd`;
    default:
      return `${n}th`;
  }
}

// ---------------------------------------------------------------- summary

export interface GameweekSummary {
  gameweek: number;
  state: Schemas['ScoreState'];
  points: number | null;
  opponentPoints: number | null;
  opponentName: string | null;
  /** W / D / L once final; null while the gameweek is still being scored. */
  result: Schemas['FormResult'] | null;
  rank: number | null;
  /** Places moved with the last completed gameweek; positive is up. */
  rankChange: number;
}

/** My points, result and league position for the current gameweek. */
export function gameweekSummary(home: LeagueHome, rankChange: number): GameweekSummary {
  const myTeamId = home.viewer.team?.id;
  const mine = findMyMatchup(home.matchups, myTeamId);
  const rank = home.standings.find((r) => r.team.id === myTeamId)?.rank ?? null;
  if (!mine) {
    return {
      gameweek: home.currentGameweek.number,
      state: 'upcoming',
      points: null,
      opponentPoints: null,
      opponentName: null,
      result: null,
      rank,
      rankChange,
    };
  }
  const { left, right } = orient(mine, myTeamId);
  const points = sidePoints(left);
  const opponentPoints = sidePoints(right);
  return {
    gameweek: mine.gameweek,
    state: mine.state,
    points,
    opponentPoints,
    opponentName: sideName(right),
    result: resultFor(points, opponentPoints, mine.state),
    rank,
    rankChange,
  };
}

/** "▲ 2", "▼ 1" or "–", with how to say it. */
export function rankChangeLabel(change: number): {
  text: string;
  tone: 'up' | 'down' | 'same';
  a11y: string;
} {
  if (change > 0) return { text: `▲ ${change}`, tone: 'up', a11y: `up ${change}` };
  if (change < 0) return { text: `▼ ${-change}`, tone: 'down', a11y: `down ${-change}` };
  return { text: '–', tone: 'same', a11y: 'no change' };
}

// ---------------------------------------------------------------- attention

/** Where an attention item leads; null for kinds this app does not know (open enum). */
export function attentionHref(
  item: Pick<Schemas['StatusAttentionItem'], 'kind'>,
  leagueId: string,
): string | null {
  const base = `/leagues/${leagueId}`;
  switch (item.kind) {
    case 'trade_offers':
      return `${base}/players/trades?from=status`;
    case 'starters_without_fixture':
    case 'injured_starters':
      return `${base}/team`;
    case 'pending_waivers':
      return `${base}/players/waivers?from=status`;
    default:
      return null;
  }
}

const SEVERITY_ORDER: Record<string, number> = { critical: 0, warning: 1, info: 2 };

/** Most urgent first; unknown severities last, in server order. */
export function sortAttention<T extends Pick<Schemas['StatusAttentionItem'], 'severity'>>(
  items: readonly T[],
): T[] {
  return items
    .map((item, i) => ({ item, i }))
    .sort(
      (a, b) =>
        (SEVERITY_ORDER[a.item.severity] ?? 3) - (SEVERITY_ORDER[b.item.severity] ?? 3) ||
        a.i - b.i,
    )
    .map((x) => x.item);
}

// ---------------------------------------------------------------- deadlines

export interface Deadline {
  key: 'waivers' | 'lock';
  label: string;
  at: number;
}

/**
 * The next waiver run and lineup lock still ahead of `now`, soonest first.
 * The waiver label says where the caller is in the order.
 */
export function upcomingDeadlines(
  gameweeks: Pick<Schemas['GameweekStatus'], 'current' | 'next'>,
  now: number,
  waivers?: Pick<Schemas['WaiverState'], 'priority' | 'nextRunAt'> | null,
  myTeamId?: string,
): Deadline[] {
  const pick = waivers?.priority.find((p) => p.team.id === myTeamId)?.rank;
  const waiverLabel = pick ? `Waivers · you have ${ordinal(pick)} pick` : 'Waivers process';
  const out: Deadline[] = [];
  for (const gw of [gameweeks.current, gameweeks.next]) {
    if (!gw) continue;
    const waiverAt = Date.parse(gw.waiverDeadlineAt);
    const lockAt = Date.parse(gw.deadlineAt);
    if (waiverAt > now && !out.some((d) => d.key === 'waivers')) {
      out.push({ key: 'waivers', label: waiverLabel, at: waiverAt });
    }
    if (lockAt > now && !out.some((d) => d.key === 'lock')) {
      out.push({ key: 'lock', label: `GW ${gw.number} lineups lock`, at: lockAt });
    }
  }
  return out.sort((a, b) => a.at - b.at);
}

// ---------------------------------------------------------------- top available

export type PositionFilter = 'all' | 'GKP' | 'DEF' | 'MID' | 'FWD';

/** The best `n` for a position filter. The list comes best first. */
export function topAvailableFor<T extends { player: { position: string } }>(
  list: readonly T[],
  position: PositionFilter,
  n = 10,
): T[] {
  return (position === 'all' ? list : list.filter((p) => p.player.position === position)).slice(
    0,
    n,
  );
}

/** One gameweek's fixtures for a table cell: `EVE (A)`, `EVE (A), TOT (H)`, or `–` in a blank. */
export function fixtureCell(week: Pick<Schemas['StatusFixtureWeek'], 'fixtures'>): string {
  if (week.fixtures.length === 0) return '–';
  return week.fixtures
    .map((f) => `${f.opponent.code.toUpperCase()} (${f.isHome ? 'H' : 'A'})`)
    .join(', ');
}

// ---------------------------------------------------------------- injuries

export interface InjuryBadge {
  /** `out` draws the red stop sign, `doubt` the yellow hazard. */
  kind: 'out' | 'doubt';
  label: string;
  /** Spoken status, e.g. "Out, injured" or "Doubtful, 50% chance of playing". */
  a11y: string;
}

/** The badge for a flagged player. Unknown availability values read as out (not playing). */
export function injuryBadge(
  f: Pick<Schemas['StatusFlaggedPlayer'], 'chanceOfPlaying'> & {
    player: Pick<Schemas['PlayerRef'], 'availability'>;
  },
): InjuryBadge {
  const a = f.player.availability;
  if (a === 'doubtful') {
    const chance = f.chanceOfPlaying;
    return chance === null
      ? { kind: 'doubt', label: 'Doubt', a11y: 'Doubtful' }
      : { kind: 'doubt', label: `${chance}%`, a11y: `Doubtful, ${chance}% chance of playing` };
  }
  const why =
    a === 'injured'
      ? 'injured'
      : a === 'suspended'
        ? 'suspended'
        : a === 'unavailable'
          ? 'unavailable'
          : 'status unknown';
  return { kind: 'out', label: 'Out', a11y: `Out, ${why}` };
}

// ---------------------------------------------------------------- league table

export interface TableRow {
  rank: number;
  team: Schemas['TeamRef'];
  /** The last completed result, if any. */
  last: Schemas['FormResult'] | null;
  /** Points in the current gameweek's matchup, if it has started. */
  gameweekPoints: number | null;
  mine: boolean;
}

/** The standings with each team's last result and current gameweek points. */
export function leagueTable(home: LeagueHome): TableRow[] {
  const points = new Map<string, number>();
  for (const m of home.matchups) {
    for (const side of [m.home, m.away]) {
      if (side.kind === 'team' && m.state !== 'upcoming') points.set(side.team.id, side.points);
    }
  }
  return home.standings.map((r) => ({
    rank: r.rank,
    team: r.team,
    last: r.form.at(-1) ?? null,
    gameweekPoints: points.get(r.team.id) ?? null,
    mine: r.team.id === home.viewer.team?.id,
  }));
}

// ---------------------------------------------------------------- fixtures

export interface FixtureLine {
  gameweek: number;
  left: string;
  right: string;
  /** "45 – 38" once the gameweek has started; null before. */
  score: string | null;
}

/**
 * My head-to-head this gameweek (with the score) and next gameweek (from
 * the schedule). Either is null when my team has no matchup.
 */
export function myFixtures(
  home: LeagueHome,
  schedule: readonly Schemas['ScheduleWeek'][] | undefined,
): { current: FixtureLine | null; next: FixtureLine | null } {
  const myTeamId = home.viewer.team?.id;
  const gw = home.currentGameweek.number;
  const mine = findMyMatchup(home.matchups, myTeamId);
  let current: FixtureLine | null = null;
  if (mine) {
    const { left, right } = orient(mine, myTeamId);
    const a = sidePoints(left);
    const b = sidePoints(right);
    current = {
      gameweek: mine.gameweek,
      left: sideName(left),
      right: sideName(right),
      score: mine.state !== 'upcoming' && a !== null && b !== null ? `${a} – ${b}` : null,
    };
  }
  const nextWeek = schedule?.find((w) => w.gameweek === gw + 1);
  const pairing = nextWeek ? findMyMatchup(nextWeek.matchups, myTeamId) : undefined;
  let next: FixtureLine | null = null;
  if (pairing) {
    const { left, right } = orient(pairing, myTeamId);
    next = { gameweek: gw + 1, left: sideName(left), right: sideName(right), score: null };
  }
  return { current, next };
}

// ---------------------------------------------------------------- awards

/** Gameweeks count once every match is played; earlier states can still move. */
const settled = (state: string) =>
  state === 'provisional' || state === 'official' || state === 'final';

/** The Team of the Week to open on: the latest settled gameweek, else the latest. */
export function defaultAwardsGameweek(
  weeks: readonly { gameweek: number; state: string }[],
): number | null {
  const done = weeks.filter((w) => settled(w.state));
  const pool = done.length > 0 ? done : weeks;
  return pool.length === 0 ? null : Math.max(...pool.map((w) => w.gameweek));
}

/** Clamps a carousel offset so `visible` cards stay filled. */
export function clampOffset(offset: number, total: number, visible: number): number {
  return Math.max(0, Math.min(offset, total - visible));
}

/** Player of the Week shows this many gameweeks at once, the latest rightmost. */
export const POTW_SHOWN = 5;

/** Below this row width the cards go compact (smaller shirt, name and gaps). */
export const POTW_COMPACT_BELOW = 560;

/** Card width so that five cards and four `gap`s fill a row `width` wide. */
export function potwCardWidth(width: number, gap: number): number {
  return Math.max(0, (width - gap * (POTW_SHOWN - 1)) / POTW_SHOWN);
}

/** Index of the leftmost card shown at scroll offset `x`, cards `step` apart. */
export function potwFirstShown(x: number, step: number, total: number): number {
  return step > 0 ? clampOffset(Math.round(x / step), total, POTW_SHOWN) : 0;
}

/** Team of the Week lines, keeper first (the bottom of the pitch) to the forwards. */
export function totwLines<T extends { player: { position: string } }>(team: readonly T[]): T[][] {
  return (['GKP', 'DEF', 'MID', 'FWD'] as const)
    .map((pos) => team.filter((t) => t.player.position === pos))
    .filter((line) => line.length > 0);
}
