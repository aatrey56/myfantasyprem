import type { Schemas } from '@myfantasyprem/api-client';

import type { PillTone } from '@/components/Chip';
import { streakOf } from '@/lib/standings';
import { formatKickoff } from '@/lib/time';

type Slot = Schemas['LineupSlot'];
type DetailSide = Schemas['MatchupDetailSide'];
type SummarySide = Schemas['MatchupSide'];

/** Positions in the order a matchup lists them; anything unknown goes last. */
export const POSITION_ORDER = ['GKP', 'DEF', 'MID', 'FWD'] as const;

export interface StarterRow {
  /** The row's position; `Other` for values this app does not know (open enum). */
  position: string;
  left: Slot | null;
  right: Slot | null;
}

export interface BenchRow<S extends { slot: number } = Slot> {
  /** 1-4: the auto-sub order. */
  order: number;
  left: S | null;
  right: S | null;
}

const positionRank = (p: string) => {
  const i = (POSITION_ORDER as readonly string[]).indexOf(p);
  return i < 0 ? POSITION_ORDER.length : i;
};

/**
 * Pairs two XIs into rows, GK→FWD. Within a position players pair in slot
 * order; when one side has more of a position the other cell is empty, so a
 * 4-4-2 against a 3-5-2 gives 4 DEF rows (one with an empty right cell) and
 * 5 MID rows. A side without a lineup (the league average) leaves every
 * right cell empty.
 */
export function pairStarters(
  left: readonly Slot[] | null,
  right: readonly Slot[] | null,
): StarterRow[] {
  const group = (slots: readonly Slot[] | null) => {
    const m = new Map<string, Slot[]>();
    for (const s of [...(slots ?? [])].sort((a, b) => a.slot - b.slot)) {
      const key =
        positionRank(s.player.position) < POSITION_ORDER.length ? s.player.position : 'Other';
      m.set(key, [...(m.get(key) ?? []), s]);
    }
    return m;
  };
  const l = group(left);
  const r = group(right);
  const rows: StarterRow[] = [];
  for (const position of [...POSITION_ORDER, 'Other']) {
    const a = l.get(position) ?? [];
    const b = r.get(position) ?? [];
    for (let i = 0; i < Math.max(a.length, b.length); i++) {
      rows.push({ position, left: a[i] ?? null, right: b[i] ?? null });
    }
  }
  return rows;
}

/** The benches side by side in auto-sub order (slot 12 first). */
export function pairBench<S extends { slot: number }>(
  left: readonly S[] | null,
  right: readonly S[] | null,
): BenchRow<S>[] {
  const l = [...(left ?? [])].sort((a, b) => a.slot - b.slot);
  const r = [...(right ?? [])].sort((a, b) => a.slot - b.slot);
  return Array.from({ length: Math.max(l.length, r.length) }, (_, i) => ({
    order: i + 1,
    left: l[i] ?? null,
    right: r[i] ?? null,
  }));
}

/** The real team on a side; null for the league average or an unknown kind. */
export function teamOf(side: AnySide): Schemas['TeamRef'] | null {
  return side.kind === 'team' ? side.team : null;
}

/** Any side shape: matchup detail, matchup summary or a scheduled opponent. */
export type AnySide = DetailSide | SummarySide | Schemas['Opponent'];

const teamIdOf = (side: AnySide) => teamOf(side)?.id ?? null;

/**
 * Puts my team on the left. For a matchup I am not in, the home side stays
 * on the left. A league-average side always goes right.
 */
export function orient<T extends AnySide>(
  m: { home: T; away: T },
  myTeamId: string | undefined,
): { left: T; right: T } {
  if (myTeamId && teamIdOf(m.away) === myTeamId) return { left: m.away, right: m.home };
  if (m.home.kind === 'average' && m.away.kind !== 'average')
    return { left: m.away, right: m.home };
  return { left: m.home, right: m.away };
}

/** The matchup my team plays in, if any. */
export function findMyMatchup<M extends { home: AnySide; away: AnySide }>(
  matchups: readonly M[],
  myTeamId: string | undefined,
): M | undefined {
  if (!myTeamId) return undefined;
  return matchups.find((m) => teamIdOf(m.home) === myTeamId || teamIdOf(m.away) === myTeamId);
}

/**
 * A side's current points, whichever shape it has; null for a scheduled
 * opponent (no score yet) or an unknown kind.
 */
export function sidePoints(side: AnySide): number | null {
  if ('totalPoints' in side) return side.totalPoints;
  if ('points' in side) return side.points;
  return null;
}

export function sideName(side: AnySide): string {
  if (side.kind === 'average') return 'League average';
  if (side.kind === 'team') return side.team.name;
  return 'Opponent';
}

/** W / D / L for the left side, once every match of the gameweek is played. */
export function resultFor(
  mine: number | null,
  theirs: number | null,
  state: Schemas['ScoreState'],
): Schemas['FormResult'] | null {
  if (!gameweekPlayed(state) || mine === null || theirs === null) return null;
  return mine > theirs ? 'W' : mine < theirs ? 'L' : 'D';
}

/** Label and tone for a score state. Unknown states render neutrally. */
export function scoreStateLabel(state: string): { label: string; tone: PillTone; dot?: boolean } {
  switch (state) {
    case 'live':
      return { label: 'Live', tone: 'live', dot: true };
    // Results count once every match is over: provisional bonus is final
    // in practice (owner decision 2026-10-25), so no "bonus pending" state.
    case 'provisional':
    case 'official':
    case 'final':
      return { label: 'Final', tone: 'neutral' };
    case 'upcoming':
      return { label: 'Upcoming', tone: 'neutral' };
    default:
      return { label: 'Scores', tone: 'neutral' };
  }
}

/**
 * One fixture's state from a player's side: kickoff time before it starts,
 * the player's minutes while live (`63'`), FT once over, or Postponed.
 */
export function fixtureState(f: Schemas['PlayerFixture']): {
  text: string;
  live: boolean;
  done: boolean;
} {
  switch (f.status) {
    case 'live':
      return { text: `${f.minutes}'`, live: true, done: false };
    case 'finished':
    case 'finished_provisional':
      return { text: 'FT', live: false, done: true };
    case 'postponed':
      return { text: 'Postponed', live: false, done: true };
    case 'scheduled':
      return { text: formatKickoff(Date.parse(f.kickoffAt)), live: false, done: false };
    default:
      return { text: '–', live: false, done: false };
  }
}

/** Auto-sub direction and whether it is only projected so far. */
export function autoSubOf(state: Schemas['AutoSubState'] | undefined): {
  kind: 'in' | 'out' | null;
  projected: boolean;
} {
  switch (state) {
    case 'projected_out':
      return { kind: 'out', projected: true };
    case 'confirmed_out':
      return { kind: 'out', projected: false };
    case 'projected_in':
      return { kind: 'in', projected: true };
    case 'confirmed_in':
      return { kind: 'in', projected: false };
    default:
      return { kind: null, projected: false };
  }
}

/**
 * True once every match of the gameweek has been played, provisional bonus
 * included. Unknown states count as still in play.
 */
export function gameweekPlayed(state: string): boolean {
  return state === 'provisional' || state === 'official' || state === 'final';
}

/**
 * The auto-sub line: "Auto-sub projected: A → B" while any of the
 * gameweek's games are still to finish, "Auto-sub: A → B" once all are
 * played. Several read "Auto-subs …: A → B, C → D". Null with none.
 */
export function autoSubLine(
  pairs: readonly { out: string; in: string }[],
  played: boolean,
): string | null {
  if (pairs.length === 0) return null;
  const head = pairs.length > 1 ? 'Auto-subs' : 'Auto-sub';
  return `${head}${played ? '' : ' projected'}: ${pairs.map((p) => `${p.out} → ${p.in}`).join(', ')}`;
}

/** The auto-sub line for one side of a matchup in the given score state. */
export function autoSubSummary(side: Schemas['TeamLineup'], state: string): string | null {
  const name = (id: string) =>
    [...side.starters, ...side.bench].find((s) => s.player.id === id)?.player.displayName ?? '?';
  return autoSubLine(
    side.autoSubs.map((a) => ({ out: name(a.playerOutId), in: name(a.playerInId) })),
    gameweekPlayed(state),
  );
}

const STAT_LABELS: Record<string, string> = {
  minutes: 'Minutes played',
  goals_scored: 'Goals',
  assists: 'Assists',
  clean_sheets: 'Clean sheet',
  goals_conceded: 'Goals conceded',
  own_goals: 'Own goals',
  penalties_saved: 'Penalties saved',
  penalties_missed: 'Penalties missed',
  yellow_cards: 'Yellow card',
  red_cards: 'Red card',
  saves: 'Saves',
  defensive_contribution: 'Defensive contributions',
  bonus: 'Bonus',
};

/** Human label for an FPL `explain` stat; unknown stats get a tidied identifier. */
export function statLabel(stat: string): string {
  return STAT_LABELS[stat] ?? stat.replace(/_/g, ' ').replace(/^\w/, (c) => c.toUpperCase());
}

/**
 * Season totals per stat from a player's game log (value and points summed).
 * `fixtures` counts the matches with a non-zero value: for `minutes`, the
 * appearances.
 */
export function seasonStatTotals(
  gameweeks: readonly Schemas['PlayerGameweek'][],
): { stat: string; value: number; points: number; fixtures: number }[] {
  const m = new Map<string, { value: number; points: number; fixtures: number }>();
  for (const gw of gameweeks) {
    for (const f of gw.fixtures) {
      for (const line of f.breakdown) {
        const t = m.get(line.stat) ?? { value: 0, points: 0, fixtures: 0 };
        m.set(line.stat, {
          value: t.value + line.value,
          points: t.points + line.points,
          fixtures: t.fixtures + (line.value !== 0 ? 1 : 0),
        });
      }
    }
  }
  const order = Object.keys(STAT_LABELS);
  const rank = (s: string) => (order.includes(s) ? order.indexOf(s) : order.length);
  return [...m.entries()]
    .map(([stat, t]) => ({ stat, ...t }))
    .sort((a, b) => rank(a.stat) - rank(b.stat) || a.stat.localeCompare(b.stat));
}

type GameweekHome = Pick<Schemas['LeagueHome'], 'currentGameweek' | 'settings'>;

/**
 * The gameweek a league view opens on: the season's current one (from
 * `GET /v1/gameweeks/current` when known, else the league home's), but
 * never outside the league's gameweeks. A league drafted mid-season starts
 * at `settings.startGameweek` (ADR 0010), so it never shows a gameweek it
 * did not play.
 */
export function leagueGameweek(home: GameweekHome, seasonCurrent?: number): number {
  const gw = Math.max(seasonCurrent ?? home.currentGameweek.number, home.settings.startGameweek);
  return Math.min(gw, home.settings.endGameweek);
}

/**
 * Whether `gw` is still ahead for matchups: after the league's current
 * gameweek, or the current one before its deadline (a league drafted
 * mid-season whose first gameweek has not started). Matchup lineups open at
 * the deadline, so these show the pairing from the schedule instead.
 */
export function matchupUpcoming(home: GameweekHome, gw: number, now = Date.now()): boolean {
  const current = home.currentGameweek;
  return gw > current.number || (gw === current.number && Date.parse(current.deadlineAt) > now);
}

/**
 * Each side's share of the combined live points, as whole percentages that
 * add up to 100 (the hero bars). Null when either side has no score yet or
 * both are on 0: there is nothing to split, and the API has no projections
 * to fall back on, so we never invent one. Negative totals count as 0.
 */
export function pointsShare(
  left: number | null,
  right: number | null,
): { left: number; right: number } | null {
  if (left === null || right === null) return null;
  const l = Math.max(0, left);
  const r = Math.max(0, right);
  if (l + r === 0) return null;
  const pct = Math.round((l / (l + r)) * 100);
  return { left: pct, right: 100 - pct };
}

/** Leading, trailing or level from one side's view; null without both scores. */
export function leadOf(
  mine: number | null,
  theirs: number | null,
): 'lead' | 'trail' | 'level' | null {
  if (mine === null || theirs === null) return null;
  return mine > theirs ? 'lead' : mine < theirs ? 'trail' : 'level';
}

/**
 * True when the player has a fixture this gameweek and none of them has
 * kicked off yet: his points show as a dash. A blank gameweek is not
 * "before kickoff" (he will not play), and unknown statuses count as started.
 */
export function beforeKickoff(slot: Pick<Slot, 'fixtures'>): boolean {
  return slot.fixtures.length > 0 && slot.fixtures.every((f) => f.status === 'scheduled');
}

/**
 * Starters still to play: any fixture of theirs not yet kicked off (a double
 * gameweek player with one game left counts). Blank, postponed and unknown
 * fixtures do not. `positions` lists them GK→FWD like `GK, 2 DEF, MID`.
 */
export function yetToPlay(starters: readonly Pick<Slot, 'fixtures' | 'player'>[]): {
  count: number;
  positions: string;
} {
  const left = starters.filter((s) => s.fixtures.some((f) => f.status === 'scheduled'));
  const counts = new Map<string, number>();
  for (const s of left) counts.set(s.player.position, (counts.get(s.player.position) ?? 0) + 1);
  const positions = [...counts.entries()]
    .sort(([a], [b]) => positionRank(a) - positionRank(b) || a.localeCompare(b))
    .map(([p, c]) => `${c > 1 ? `${c} ` : ''}${p === 'GKP' ? 'GK' : p}`)
    .join(', ');
  return { count: left.length, positions };
}

/** A team's line under its name in a matchup: record, current run and league rank. */
export interface TeamRecord {
  /** `3-2-1`: won-drawn-lost. */
  record: string;
  /** The current run, from the last five results the standings carry (so at most 5). */
  streak: { result: string; count: number } | null;
  rank: number;
}

/** ▲ for a winning run, ▼ for a losing one, = for draws; nothing for an unknown result. */
export function streakArrow(result: string): string {
  return result === 'W' ? '▲' : result === 'L' ? '▼' : result === 'D' ? '=' : '';
}

/** `▲2W`, `▼3L`, `=1D`; empty before any result. */
export function streakLabel(streak: TeamRecord['streak']): string {
  return streak ? `${streakArrow(streak.result)}${streak.count}${streak.result}` : '';
}

/** A standings row as a matchup record. */
export function teamRecord(
  r: Pick<Schemas['StandingRow'], 'wins' | 'draws' | 'losses' | 'rank' | 'form'>,
): TeamRecord {
  return { record: `${r.wins}-${r.draws}-${r.losses}`, streak: streakOf(r.form), rank: r.rank };
}

/** `3-2-1 ▼2L (#5)`: won-drawn-lost, the current run and league rank, for a matchup subline. */
export function recordLine(rec: TeamRecord): string {
  return [rec.record, streakLabel(rec.streak), `(#${rec.rank})`].filter(Boolean).join(' ');
}

/** Records by team id, from the standings. */
export function teamRecords(
  standings: readonly Schemas['StandingRow'][] | undefined,
): ReadonlyMap<string, TeamRecord> {
  return new Map((standings ?? []).map((r) => [r.team.id, teamRecord(r)]));
}
