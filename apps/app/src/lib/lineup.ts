import type { Schemas } from '@myfantasyprem/api-client';

type Entry = Schemas['LineupEntry'];
type Position = 'GKP' | 'DEF' | 'MID' | 'FWD';

export interface LineupState {
  starters: Entry[];
  bench: Entry[];
}

/** v1 XI rules (ADR 0008); the league's `settings.lineup` carries the same numbers. */
export const DEFAULT_RULES: Schemas['LineupRules'] = {
  starters: 11,
  minStarters: { GKP: 1, DEF: 3, MID: 2, FWD: 1 },
  maxStarters: { GKP: 1, DEF: 5, MID: 5, FWD: 3 },
  benchGoalkeeperFirst: true,
};

const POSITIONS: readonly Position[] = ['GKP', 'DEF', 'MID', 'FWD'];
const WORD: Record<Position, string> = {
  GKP: 'goalkeeper',
  DEF: 'defenders',
  MID: 'midfielders',
  FWD: 'forwards',
};

export function countByPosition(entries: readonly Entry[]): Record<Position, number> {
  const c: Record<Position, number> = { GKP: 0, DEF: 0, MID: 0, FWD: 0 };
  for (const e of entries) {
    const p = e.player.position as Position;
    if (p in c) c[p]++;
  }
  return c;
}

/** `4-4-2`: defenders, midfielders, forwards in the XI. */
export function formationOf(starters: readonly Entry[]): string {
  const c = countByPosition(starters);
  return `${c.DEF}-${c.MID}-${c.FWD}`;
}

/** The XI by line, goalkeeper first. Unknown positions go last, so nothing disappears. */
export function linesOf(starters: readonly Entry[]): { position: string; entries: Entry[] }[] {
  const lines = POSITIONS.map((position) => ({
    position: position as string,
    entries: starters.filter((e) => e.player.position === position),
  }));
  const other = starters.filter((e) => !POSITIONS.includes(e.player.position as Position));
  if (other.length) lines.push({ position: 'Other', entries: other });
  return lines;
}

/**
 * Client-side check of the XI rules, as plain sentences. Only a hint for
 * the UI: the server's 422 is the authority, and its messages are what
 * the team screen shows after a refused save.
 */
export function lineupProblems(
  state: LineupState,
  rules: Schemas['LineupRules'] = DEFAULT_RULES,
): string[] {
  const out: string[] = [];
  const c = countByPosition(state.starters);
  if (state.starters.length !== rules.starters)
    out.push(`Pick exactly ${rules.starters} starters.`);
  for (const p of POSITIONS) {
    if (c[p] < rules.minStarters[p]) out.push(`Start at least ${rules.minStarters[p]} ${WORD[p]}.`);
    if (c[p] > rules.maxStarters[p]) out.push(`Start at most ${rules.maxStarters[p]} ${WORD[p]}.`);
  }
  if (rules.benchGoalkeeperFirst && state.bench[0] && state.bench[0].player.position !== 'GKP') {
    out.push('The first bench slot must be your backup goalkeeper.');
  }
  return out;
}

type Where = { zone: 'starters' | 'bench'; index: number };

function locate(state: LineupState, playerId: string): Where | null {
  const s = state.starters.findIndex((e) => e.player.id === playerId);
  if (s >= 0) return { zone: 'starters', index: s };
  const b = state.bench.findIndex((e) => e.player.id === playerId);
  if (b >= 0) return { zone: 'bench', index: b };
  return null;
}

/** Exchanges two players' places (XI ↔ bench, or two bench places). Slots are renumbered. */
export function swapPlayers(state: LineupState, a: string, b: string): LineupState {
  const wa = locate(state, a);
  const wb = locate(state, b);
  if (!wa || !wb || a === b) return state;
  const next: LineupState = { starters: [...state.starters], bench: [...state.bench] };
  const ea = next[wa.zone][wa.index]!;
  const eb = next[wb.zone][wb.index]!;
  next[wa.zone][wa.index] = eb;
  next[wb.zone][wb.index] = ea;
  return renumber(next);
}

function renumber(state: LineupState): LineupState {
  return {
    starters: state.starters.map((e, i) => ({ ...e, slot: i + 1 })),
    bench: state.bench.map((e, i) => ({ ...e, slot: i + 12 })),
  };
}

export type SwapCheck = { kind: 'swap' } | { kind: 'invalid'; reason: string } | { kind: 'none' };

/**
 * What tapping `target` after selecting `selected` would do: a legal
 * swap, an illegal one (with the first broken rule), or nothing (two
 * starters: order inside the XI does not matter).
 */
export function checkSwap(
  state: LineupState,
  selected: string,
  target: string,
  rules: Schemas['LineupRules'] = DEFAULT_RULES,
): SwapCheck {
  if (selected === target) return { kind: 'none' };
  const a = locate(state, selected);
  const b = locate(state, target);
  if (!a || !b) return { kind: 'none' };
  if (a.zone === 'starters' && b.zone === 'starters') return { kind: 'none' };
  const problems = lineupProblems(swapPlayers(state, selected, target), rules);
  return problems.length ? { kind: 'invalid', reason: problems[0]! } : { kind: 'swap' };
}

/** What a tap does to the swap selection. */
export type TapResult =
  | { kind: 'select'; selected: string }
  | { kind: 'deselect' }
  | { kind: 'swap'; next: LineupState }
  | { kind: 'reject'; reason: string };

/**
 * The swap selection as a state machine: the first tap selects a player,
 * tapping them again deselects, and a second player either swaps with them
 * (a legal swap), is refused with the first broken rule, or (another
 * starter: order inside the XI does not matter) becomes the selection.
 */
export function tapPlayer(
  state: LineupState,
  selected: string | null,
  tapped: string,
  rules: Schemas['LineupRules'] = DEFAULT_RULES,
): TapResult {
  if (selected === null) return { kind: 'select', selected: tapped };
  if (selected === tapped) return { kind: 'deselect' };
  const verdict = checkSwap(state, selected, tapped, rules);
  if (verdict.kind === 'swap') return { kind: 'swap', next: swapPlayers(state, selected, tapped) };
  if (verdict.kind === 'invalid') return { kind: 'reject', reason: verdict.reason };
  return { kind: 'select', selected: tapped };
}

export function toLineupInput(state: LineupState, gameweek: number) {
  return {
    gameweek,
    starters: state.starters.map((e) => e.player.id),
    bench: state.bench.map((e) => e.player.id),
  };
}

export function fromLineup(lineup: Schemas['Lineup']): LineupState {
  return { starters: lineup.starters, bench: lineup.bench };
}
