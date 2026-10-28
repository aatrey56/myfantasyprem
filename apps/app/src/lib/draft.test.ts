import type { Schemas } from '@myfantasyprem/api-client';

import {
  buildBoard,
  clockRemaining,
  hasRoomFor,
  pickLabel,
  picksUntilTurn,
  positionCounts,
  queueAdd,
  queueMove,
  queueRemove,
  queueWithoutDrafted,
  snakeColumn,
  snakeOverall,
  type QueueItem,
  missedClockNotice,
  startBlocker,
  startExplanation,
} from './draft';

const team = (n: number): Schemas['TeamRef'] => ({
  id: `tm_${String(n).padStart(26, '0')}`,
  name: `Team ${n}`,
  managerName: `M${n}`,
});
const player = (n: number, position: Schemas['Position'] = 'MID'): Schemas['PlayerRef'] => ({
  id: `plr_${String(n).padStart(26, '0')}`,
  displayName: `P${n}`,
  club: { code: 'NOR', name: 'Northgate' },
  position,
  availability: 'available',
});
const item = (n: number): QueueItem => ({ player: player(n), draftRank: n });
const ids = (q: QueueItem[]) => q.map((x) => x.draftRank);

const order = [1, 2, 3, 4].map((n, i) => ({ slot: i + 1, team: team(n), autopick: false }));
// The 4-team snake by hand (RESEARCH.md §7): columns for overall picks 1-8,
// then it repeats. Fixtures use this, never the snake functions under test.
const SNAKE4 = [0, 1, 2, 3, 3, 2, 1, 0];
const columnOf = (overall: number) => SNAKE4[(overall - 1) % 8]!;
function pick(overall: number, position: Schemas['Position'] = 'MID'): Schemas['DraftPick'] {
  const n = order.length;
  const round = Math.ceil(overall / n);
  return {
    id: `pk_${String(overall).padStart(26, '0')}`,
    overall,
    round,
    pickInRound: overall - (round - 1) * n,
    team: order[columnOf(overall)]!.team,
    player: player(overall, position),
    method: 'manual',
    missedClock: false,
    pickedAt: '2026-10-12T10:00:00Z',
  };
}

describe('snake order', () => {
  it('runs left to right in odd rounds and back in even rounds', () => {
    expect([0, 1, 2, 3].map((c) => snakeOverall(1, c, 4))).toEqual([1, 2, 3, 4]);
    expect([0, 1, 2, 3].map((c) => snakeOverall(2, c, 4))).toEqual([8, 7, 6, 5]);
    expect([0, 1, 2, 3].map((c) => snakeOverall(3, c, 4))).toEqual([9, 10, 11, 12]);
  });

  it('maps an overall pick back to its column', () => {
    for (let overall = 1; overall <= 60; overall++) {
      expect(snakeColumn(overall, 4)).toBe(columnOf(overall));
    }
    // 3 teams: 1 2 3 | 3 2 1 | 1 ...
    expect([1, 2, 3, 4, 5, 6, 7].map((o) => snakeColumn(o, 3))).toEqual([0, 1, 2, 2, 1, 0, 0]);
  });

  it('labels picks as round.pick', () => {
    expect(pickLabel(6, 3)).toBe('6.03');
    expect(pickLabel(12, 10)).toBe('12.10');
  });
});

describe('board', () => {
  it('places picks in their cells and leaves the rest empty', () => {
    const board = buildBoard({ order, rounds: 15, picks: [pick(1), pick(5), pick(8)] });
    expect(board).toHaveLength(15);
    expect(board[0]!.map((c) => c.pick?.overall ?? null)).toEqual([1, null, null, null]);
    // Round 2 is reversed: pick 5 is in the last column, pick 8 in the first.
    expect(board[1]!.map((c) => c.pick?.overall ?? null)).toEqual([8, null, null, 5]);
    expect(board[1]![0]!.pickInRound).toBe(4);
  });
});

describe('turns and the clock', () => {
  const currentPick = (overall: number): Schemas['DraftClock'] => ({
    overall,
    round: Math.ceil(overall / 4),
    pickInRound: ((overall - 1) % 4) + 1,
    team: order[columnOf(overall)]!.team,
    startedAt: '2026-10-12T10:00:00Z',
    deadlineAt: '2026-10-12T10:01:30Z',
  });

  it('counts picks until a team is up, across the snake turn', () => {
    const draft = { order, rounds: 15, picks: [], currentPick: currentPick(3) };
    expect(picksUntilTurn(draft, team(3).id)).toBe(0);
    expect(picksUntilTurn(draft, team(4).id)).toBe(1);
    // Team 1 picks 1 then 8 (the snake back).
    expect(picksUntilTurn(draft, team(1).id)).toBe(5);
    expect(picksUntilTurn({ ...draft, currentPick: null }, team(1).id)).toBeNull();
    expect(picksUntilTurn(draft, 'tm_unknown')).toBeNull();
  });

  it('judges the clock on the server clock and never goes negative', () => {
    const deadline = '2026-10-12T10:01:30Z';
    const now = Date.parse('2026-10-12T10:01:00Z');
    expect(clockRemaining(deadline, 0, now)).toBe(30_000);
    // The device clock runs 10 s behind the server.
    expect(clockRemaining(deadline, 10_000, now)).toBe(20_000);
    expect(clockRemaining(deadline, 0, now + 60_000)).toBe(0);
    expect(clockRemaining(null, 0, now)).toBeNull();
  });
});

describe('squad needs', () => {
  it('counts a team’s picks by position and knows when a position is full', () => {
    const picks = [pick(1, 'GKP'), pick(8, 'GKP'), pick(9, 'DEF'), pick(2, 'FWD')];
    const counts = positionCounts(picks, team(1).id);
    expect(counts).toEqual({ GKP: 2, DEF: 1, MID: 0, FWD: 0 });
    expect(hasRoomFor(counts, 'GKP')).toBe(false);
    expect(hasRoomFor(counts, 'DEF')).toBe(true);
    expect(hasRoomFor(counts, 'COACH')).toBe(false);
  });
});

describe('queue', () => {
  const q = [item(1), item(2), item(3)];

  it('adds to the end and never twice', () => {
    expect(ids(queueAdd(q, item(9)))).toEqual([1, 2, 3, 9]);
    expect(ids(queueAdd(q, item(2)))).toEqual([1, 2, 3]);
  });

  it('removes', () => {
    expect(ids(queueRemove(q, player(2).id))).toEqual([1, 3]);
    expect(ids(queueRemove(q, 'plr_missing'))).toEqual([1, 2, 3]);
  });

  it('moves up and down, clamped at the ends', () => {
    expect(ids(queueMove(q, player(3).id, -1))).toEqual([1, 3, 2]);
    expect(ids(queueMove(q, player(1).id, 1))).toEqual([2, 1, 3]);
    expect(ids(queueMove(q, player(1).id, -1))).toEqual([1, 2, 3]);
    expect(ids(queueMove(q, player(3).id, 5))).toEqual([1, 2, 3]);
    expect(ids(queueMove(q, 'plr_missing', 1))).toEqual([1, 2, 3]);
  });

  it('drops drafted players', () => {
    expect(ids(queueWithoutDrafted(q, [pick(2)]))).toEqual([1, 3]);
  });

  it('does not mutate the input', () => {
    queueMove(q, player(3).id, -2);
    queueAdd(q, item(4));
    expect(ids(q)).toEqual([1, 2, 3]);
  });
});

describe('startBlocker', () => {
  it('needs at least 2 teams with a manager, and nothing more', () => {
    expect(startBlocker({ teamCount: 8, memberCount: 1 })).toBe(
      'At least 2 teams need a manager before the draft can start.',
    );
    expect(startBlocker({ teamCount: 8, memberCount: 2 })).toBeNull();
    expect(startBlocker({ teamCount: 8, memberCount: 8 })).toBeNull();
    expect(startBlocker({ teamCount: 2, memberCount: 2 })).toBeNull();
  });
});

describe('startExplanation', () => {
  it('explains the open seats an early start leaves', () => {
    expect(startExplanation({ teamCount: 12, memberCount: 3 })).toBe(
      '3 of 12 teams have a manager. The 9 open seats are autopicked; anyone who joins later takes one over.',
    );
    expect(startExplanation({ teamCount: 8, memberCount: 7 })).toBe(
      '7 of 8 teams have a manager. The open seat is autopicked; anyone who joins later takes it over.',
    );
  });

  it('says so when every place is filled, and why when too few are', () => {
    expect(startExplanation({ teamCount: 8, memberCount: 8 })).toBe(
      'Every team has a manager, so you can start now.',
    );
    expect(startExplanation({ teamCount: 8, memberCount: 1 })).toBe(
      'At least 2 teams need a manager before the draft can start.',
    );
  });
});

describe('missedClockNotice', () => {
  const missed = (overall: number, method: Schemas['DraftPickMethod'] = 'rank') => ({
    ...pick(overall),
    method,
    missedClock: true,
  });
  const picks = (...list: Schemas['DraftPick'][]) => ({ order, picks: list });

  it('names the team and the player of the latest missed clock this round', () => {
    const n = missedClockNotice(picks(pick(1), missed(2), pick(3)), null, false)!;
    expect(n.mine).toBe(false);
    expect(n.text).toBe(
      `${pick(2).team.name}'s clock ran out: autopicked ${pick(2).player.displayName} (best available by rank).`,
    );
    expect(n.pickId).toBe(pick(2).id);
  });

  it('speaks to me when it was my clock, and says what happens next', () => {
    const me = pick(2).team.id;
    const on = missedClockNotice(picks(pick(1), missed(2, 'queue')), me, true)!;
    expect(on.mine).toBe(true);
    expect(on.text).toMatch(/^Your clock ran out: autopicked .+ for you \(from the queue\)\./);
    expect(on.text).toMatch(/Autopick stays on/);
    expect(missedClockNotice(picks(missed(2)), me, false)!.text).toMatch(/full clock/);
  });

  it('is gone once a full round has passed, or when no clock was missed', () => {
    expect(
      missedClockNotice(picks(missed(1), pick(2), pick(3), pick(4), pick(5)), null, false),
    ).toBeNull();
    expect(missedClockNotice(picks(pick(1), pick(2)), null, false)).toBeNull();
  });
});
