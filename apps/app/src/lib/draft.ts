import type { Schemas } from '@myfantasyprem/api-client';

type Draft = Schemas['Draft'];
type DraftPick = Schemas['DraftPick'];
type Position = 'GKP' | 'DEF' | 'MID' | 'FWD';

/** The v1 squad (ADR 0008): 2 GKP, 5 DEF, 5 MID, 3 FWD. */
export const SQUAD: Record<Position, number> = { GKP: 2, DEF: 5, MID: 5, FWD: 3 };
export const POSITIONS: readonly Position[] = ['GKP', 'DEF', 'MID', 'FWD'];

/**
 * Overall pick number of a board cell in a snake draft: round `round`
 * (1-based), team column `col` (0-based, round-1 order), `teams` teams.
 * Odd rounds run left to right, even rounds right to left.
 */
export function snakeOverall(round: number, col: number, teams: number): number {
  const inRound = round % 2 === 1 ? col + 1 : teams - col;
  return (round - 1) * teams + inRound;
}

/** The round-1 column (0-based) that picks at `overall`. */
export function snakeColumn(overall: number, teams: number): number {
  const round = Math.ceil(overall / teams);
  const idx = (overall - 1) % teams;
  return round % 2 === 1 ? idx : teams - 1 - idx;
}

/** `6.03`: round and pick in round, as draft boards write it. */
export function pickLabel(round: number, pickInRound: number): string {
  return `${round}.${String(pickInRound).padStart(2, '0')}`;
}

export interface BoardCell {
  round: number;
  col: number;
  overall: number;
  pickInRound: number;
  pick: DraftPick | null;
}

/** The board as rows (rounds) of cells (teams in round-1 order). */
export function buildBoard(draft: Pick<Draft, 'order' | 'picks' | 'rounds'>): BoardCell[][] {
  const n = draft.order.length;
  const byOverall = new Map(draft.picks.map((p) => [p.overall, p]));
  return Array.from({ length: draft.rounds }, (_, r) =>
    Array.from({ length: n }, (_, col) => {
      const round = r + 1;
      const overall = snakeOverall(round, col, n);
      return {
        round,
        col,
        overall,
        pickInRound: overall - (round - 1) * n,
        pick: byOverall.get(overall) ?? null,
      };
    }),
  );
}

/** Players a team has drafted, per position. Unknown positions are ignored. */
export function positionCounts(
  picks: readonly DraftPick[],
  teamId: string,
): Record<Position, number> {
  const counts: Record<Position, number> = { GKP: 0, DEF: 0, MID: 0, FWD: 0 };
  for (const p of picks) {
    if (p.team.id !== teamId) continue;
    const pos = p.player.position as Position;
    if (pos in counts) counts[pos]++;
  }
  return counts;
}

/** True while the squad still has a place at this position. */
export function hasRoomFor(counts: Record<Position, number>, position: string): boolean {
  const pos = position as Position;
  return pos in SQUAD ? counts[pos] < SQUAD[pos] : false;
}

/**
 * Picks to go before the team's next turn: 0 when it is on the clock,
 * null when it has no picks left (or the draft is not running).
 */
export function picksUntilTurn(
  draft: Pick<Draft, 'order' | 'picks' | 'rounds' | 'currentPick'>,
  teamId: string,
): number | null {
  const n = draft.order.length;
  if (!draft.currentPick || n === 0) return null;
  const col = draft.order.findIndex((s) => s.team.id === teamId);
  if (col < 0) return null;
  const total = n * draft.rounds;
  for (let overall = draft.currentPick.overall; overall <= total; overall++) {
    if (snakeColumn(overall, n) === col) return overall - draft.currentPick.overall;
  }
  return null;
}

/**
 * Milliseconds left on the pick clock, judged on the server's clock:
 * `offset` is serverTime minus the device time the response arrived.
 */
export function clockRemaining(
  deadlineAt: string | null,
  offsetMs: number,
  now: number,
): number | null {
  if (!deadlineAt) return null;
  const deadline = Date.parse(deadlineAt);
  if (!Number.isFinite(deadline)) return null;
  return Math.max(0, deadline - (now + offsetMs));
}

// ---------------------------------------------------------------- queue

export type QueueItem = Schemas['DraftQueueItem'];

/** Adds to the end; a player already queued stays where they are. */
export function queueAdd(queue: readonly QueueItem[], item: QueueItem): QueueItem[] {
  return queue.some((q) => q.player.id === item.player.id) ? [...queue] : [...queue, item];
}

export function queueRemove(queue: readonly QueueItem[], playerId: string): QueueItem[] {
  return queue.filter((q) => q.player.id !== playerId);
}

/** Moves a player by `delta` places (negative is up), clamped to the ends. */
export function queueMove(
  queue: readonly QueueItem[],
  playerId: string,
  delta: number,
): QueueItem[] {
  const from = queue.findIndex((q) => q.player.id === playerId);
  if (from < 0) return [...queue];
  const to = Math.min(queue.length - 1, Math.max(0, from + delta));
  const next = [...queue];
  const [item] = next.splice(from, 1);
  next.splice(to, 0, item!);
  return next;
}

/** Drops players who have been drafted (the server does too; this keeps the UI honest between polls). */
export function queueWithoutDrafted(
  queue: readonly QueueItem[],
  picks: readonly DraftPick[],
): QueueItem[] {
  const taken = new Set(picks.map((p) => p.player.id));
  return queue.filter((q) => !taken.has(q.player.id));
}

/** The fewest teams with a manager a draft can start with (ADR 0008 rule 15). */
export const MIN_TEAMS = 2;

type StartLeague = Pick<Schemas['LeagueSummary'], 'teamCount' | 'memberCount'>;

/**
 * Why the draft cannot start yet, or null when it can: at least
 * {@link MIN_TEAMS} teams need a manager; empty places become open seats.
 * The server refuses the same with 409 `not-enough-managers`.
 */
export function startBlocker(league: StartLeague): string | null {
  if (league.memberCount >= MIN_TEAMS) return null;
  return `At least ${MIN_TEAMS} teams need a manager before the draft can start.`;
}

/** What starting does, or why it cannot: the line under the Start button. */
export function startExplanation(league: StartLeague): string {
  const blocked = startBlocker(league);
  if (blocked) return blocked;
  const open = league.teamCount - league.memberCount;
  if (open <= 0) return 'Every team has a manager, so you can start now.';
  const have = `${league.memberCount} of ${league.teamCount} teams have a manager.`;
  return open === 1
    ? `${have} The open seat is autopicked; anyone who joins later takes it over.`
    : `${have} The ${open} open seats are autopicked; anyone who joins later takes one over.`;
}

export interface MissedClockNotice {
  /** The pick it is about (dismissing hides it until the next one). */
  pickId: string;
  mine: boolean;
  text: string;
}

/**
 * The draft room's notice for the latest pick autopicked because a clock
 * ran out (`missedClock`), while it is within the last round of picks;
 * null otherwise. A missed clock is never silent (ADR 0010).
 */
export function missedClockNotice(
  draft: Pick<Draft, 'picks' | 'order'>,
  myTeamId: string | null,
  autopickOn: boolean,
): MissedClockNotice | null {
  const window = Math.max(draft.order.length, 1);
  const recent = draft.picks.slice(-window);
  const p = recent.filter((pk) => pk.missedClock).at(-1);
  if (!p) return null;
  const mine = p.team.id === myTeamId;
  const how = p.method === 'queue' ? 'from the queue' : 'best available by rank';
  const text = mine
    ? `Your clock ran out: autopicked ${p.player.displayName} for you (${how}).${
        autopickOn
          ? ' Autopick stays on for your next picks.'
          : ' Your next pick gets a full clock.'
      }`
    : `${p.team.name}'s clock ran out: autopicked ${p.player.displayName} (${how}).`;
  return { pickId: p.id, mine, text };
}
