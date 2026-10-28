/**
 * Rule tests for the client helpers, added from a StrykerJS mutation run
 * (docs/testing.md). Every expected value is worked out by hand from the
 * rules (RESEARCH.md §7, ADR 0008, the API contract), never by calling the
 * code under test.
 */
import type { Schemas } from '@myfantasyprem/api-client';

import { clockRemaining, hasRoomFor, picksUntilTurn, positionCounts, queueMove } from './draft';
import { checkSwap, lineupProblems, type LineupState } from './lineup';
import { autoSubOf, findMyMatchup, fixtureState, orient, resultFor } from './matchup';
import { sortStandings, streakOf } from './standings';
import { checkTrade, tradeBucket } from './trade';
import { moveClaim } from './waivers';

// ---------------------------------------------------------------- trade

const pl = (id: string, position: string) => ({ id, position: position as Schemas['Position'] });
const side = (n: number, position = 'DEF') =>
  Array.from({ length: n }, (_, i) => pl(`${position}${i}-${n}`, position));

describe('trade shape (ADR 0008 rule 13: 1-5 players a side)', () => {
  it('allows exactly 5 for 5 and refuses 6 for 6', () => {
    expect(checkTrade(side(5), side(5)).ok).toBe(true);
    const six = checkTrade(side(6), side(6));
    expect(six.ok).toBe(false);
    expect(six.problems).toEqual(['A trade moves at most 5 players each way.']);
  });

  it('allows 1 for 1, and a 280-character message but not 281', () => {
    expect(checkTrade(side(1), side(1), 'x'.repeat(280)).ok).toBe(true);
    expect(checkTrade(side(1), side(1), 'x'.repeat(281)).problems).toEqual([
      'Keep the message to 280 characters.',
    ]);
  });

  it('flags only the empty side', () => {
    expect(checkTrade([], side(1)).problems).toEqual([
      'Pick at least one of your players to give.',
    ]);
    expect(checkTrade(side(1), []).problems).toEqual([
      'Pick at least one of their players to get.',
    ]);
  });

  it('needs the same count before comparing positions', () => {
    expect(checkTrade(side(2), side(1)).problems).toEqual([
      'Both sides must move the same number of players: you give 2, you get 1.',
    ]);
  });
});

describe('trade inbox buckets', () => {
  const t = (status: string, from: string, to: string) =>
    ({
      status,
      offeringTeam: { id: from },
      receivingTeam: { id: to },
    }) as unknown as Schemas['Trade'];
  it('files open trades by who must act', () => {
    expect(tradeBucket(t('offered', 'a', 'me'), 'me')).toBe('incoming');
    expect(tradeBucket(t('offered', 'me', 'b'), 'me')).toBe('outgoing');
    expect(tradeBucket(t('offered', 'a', 'b'), 'me')).toBe('review');
    expect(tradeBucket(t('accepted', 'me', 'b'), 'me')).toBe('review');
    expect(tradeBucket(t('in_review', 'a', 'me'), 'me')).toBe('review');
    expect(tradeBucket(t('vetoed', 'a', 'me'), 'me')).toBe('history');
    expect(tradeBucket(t('offered', 'a', 'me'), undefined)).toBe('review');
  });
});

// ---------------------------------------------------------------- lineup

let n = 0;
const entry = (position: Schemas['Position']): Schemas['LineupEntry'] => {
  n += 1;
  return {
    slot: n,
    player: {
      id: `p${n}`,
      displayName: `${position} ${n}`,
      club: { code: 'NOR', name: 'Northgate' },
      position,
      availability: 'available',
    },
    fixtures: [],
  };
};
/** An XI of the given shape and the remaining squad (2/5/5/3) on the bench, keeper first. */
function xi(def: number, mid: number, fwd: number): LineupState {
  const starters = [entry('GKP')];
  const bench = [entry('GKP')];
  for (const [pos, k, total] of [
    ['DEF', def, 5],
    ['MID', mid, 5],
    ['FWD', fwd, 3],
  ] as const) {
    for (let i = 0; i < total; i++) (i < k ? starters : bench).push(entry(pos));
  }
  return { starters, bench };
}

describe('formation rule (RESEARCH.md §7: 1 GKP, 3-5 DEF, 2-5 MID, 1-3 FWD)', () => {
  it.each([
    [3, 5, 2],
    [5, 2, 3],
    [5, 4, 1],
    [4, 3, 3],
  ])('accepts %i-%i-%i', (d, m, f) => {
    expect(lineupProblems(xi(d, m, f))).toEqual([]);
  });

  it('names each broken limit', () => {
    // 1 + 5 + 5 + 0 = 11 starters, but no forward.
    expect(lineupProblems(xi(5, 5, 0))).toEqual(['Start at least 1 forwards.']);
    // 1 + 5 + 4 + 0 = 10 starters, no forward.
    expect(lineupProblems(xi(5, 4, 0))).toEqual([
      'Pick exactly 11 starters.',
      'Start at least 1 forwards.',
    ]);
    // 1 + 2 + 5 + 3 = 11, two defenders.
    expect(lineupProblems(xi(2, 5, 3))).toEqual(['Start at least 3 defenders.']);
  });

  it('needs the backup keeper first on the bench, and only checks a bench that exists', () => {
    const s = xi(4, 4, 2);
    s.bench = [...s.bench.slice(1), s.bench[0]!];
    expect(lineupProblems(s)).toEqual(['The first bench slot must be your backup goalkeeper.']);
    expect(lineupProblems({ starters: xi(4, 4, 2).starters, bench: [] })).toEqual([]);
  });

  it('swaps: legal like for like, illegal below a minimum, nothing between starters', () => {
    const s = xi(3, 5, 2);
    const benchDef = s.bench.find((e) => e.player.position === 'DEF')!.player.id;
    const benchFwd = s.bench.find((e) => e.player.position === 'FWD')!.player.id;
    const startDef = s.starters.find((e) => e.player.position === 'DEF')!.player.id;
    const startMid = s.starters.find((e) => e.player.position === 'MID')!.player.id;
    expect(checkSwap(s, startDef, benchDef)).toEqual({ kind: 'swap' });
    // 3-5-2 minus a defender is 2 DEF: below the minimum of 3.
    expect(checkSwap(s, startDef, benchFwd)).toEqual({
      kind: 'invalid',
      reason: 'Start at least 3 defenders.',
    });
    expect(checkSwap(s, startDef, startMid)).toEqual({ kind: 'none' });
    expect(checkSwap(s, startDef, 'nobody')).toEqual({ kind: 'none' });
  });
});

// ---------------------------------------------------------------- draft

describe('draft helpers', () => {
  const pk = (teamId: string, position: string) =>
    ({ team: { id: teamId }, player: { position } }) as unknown as Schemas['DraftPick'];

  it('counts only the team’s own picks and caps each position at the squad size', () => {
    const picks = [pk('a', 'DEF'), pk('b', 'DEF'), pk('a', 'DEF'), pk('a', 'COACH')];
    const c = positionCounts(picks, 'a');
    expect(c).toEqual({ GKP: 0, DEF: 2, MID: 0, FWD: 0 });
    expect(hasRoomFor({ GKP: 1, DEF: 5, MID: 4, FWD: 3 }, 'GKP')).toBe(true); // 1 of 2
    expect(hasRoomFor({ GKP: 1, DEF: 5, MID: 4, FWD: 3 }, 'DEF')).toBe(false); // 5 of 5
    expect(hasRoomFor({ GKP: 1, DEF: 5, MID: 4, FWD: 3 }, 'FWD')).toBe(false); // 3 of 3
  });

  it('has no next turn after the last pick, or with no teams', () => {
    const team = (id: string) => ({
      slot: 1,
      team: { id, name: id, managerName: id },
      autopick: false,
    });
    const order = [team('a'), team('b')];
    const clock = (overall: number) => ({ overall }) as unknown as Schemas['DraftClock'];
    // 2 teams × 2 rounds = picks a b | b a. At pick 4 (a), b has none left.
    expect(picksUntilTurn({ order, rounds: 2, picks: [], currentPick: clock(4) }, 'b')).toBeNull();
    expect(picksUntilTurn({ order, rounds: 2, picks: [], currentPick: clock(4) }, 'a')).toBe(0);
    expect(picksUntilTurn({ order, rounds: 2, picks: [], currentPick: clock(2) }, 'a')).toBe(2);
    expect(
      picksUntilTurn({ order: [], rounds: 2, picks: [], currentPick: clock(1) }, 'a'),
    ).toBeNull();
  });

  it('treats an unreadable deadline as no clock', () => {
    expect(clockRemaining('not a date', 0, 0)).toBeNull();
  });

  it('moves a queued player exactly one place', () => {
    const q = ['a', 'b', 'c', 'd'].map((id, i) => ({
      player: { id } as Schemas['PlayerRef'],
      draftRank: i + 1,
    }));
    expect(queueMove(q, 'c', -2).map((x) => x.player.id)).toEqual(['c', 'a', 'b', 'd']);
    expect(queueMove(q, 'a', 3).map((x) => x.player.id)).toEqual(['b', 'c', 'd', 'a']);
  });
});

// ---------------------------------------------------------------- waivers

describe('claim order', () => {
  it('moves one place and stops at both ends', () => {
    expect(moveClaim(['a', 'b', 'c'], 'c', 1)).toEqual(['a', 'b', 'c']);
    expect(moveClaim(['a', 'b', 'c'], 'b', 1)).toEqual(['a', 'c', 'b']);
    expect(moveClaim(['a', 'b', 'c'], 'a', -1)).toEqual(['a', 'b', 'c']);
    expect(moveClaim(['a', 'b', 'c'], 'x', 1)).toEqual(['a', 'b', 'c']);
  });
});

// ---------------------------------------------------------------- standings

describe('standings columns', () => {
  const row = (
    name: string,
    w: number,
    d: number,
    pf: number,
    pa: number,
    form: string[],
    rank: number,
  ) =>
    ({
      rank,
      team: { id: name, name, managerName: name },
      wins: w,
      draws: d,
      losses: 0,
      leaguePoints: 3 * w + d,
      pointsFor: pf,
      pointsAgainst: pa,
      form,
    }) as unknown as Schemas['StandingRow'];
  // a: 9 pts, b: 7 pts, c: 4 pts. Streaks: a W1, b L2, c D3.
  const a = row('Ash', 3, 0, 300, 250, ['L', 'W'], 1);
  const b = row('Birch', 2, 1, 320, 200, ['W', 'L', 'L'], 2);
  const c = row('Cedar', 1, 1, 280, 210, ['D', 'D', 'D'], 3);
  const names = (rows: Schemas['StandingRow'][]) => rows.map((r) => r.team.name);

  it('sorts each column best first, and reversed with asc', () => {
    expect(names(sortStandings([c, b, a], 'rank'))).toEqual(['Ash', 'Birch', 'Cedar']);
    expect(names(sortStandings([a, b, c], 'pointsFor'))).toEqual(['Birch', 'Ash', 'Cedar']);
    expect(names(sortStandings([a, b, c], 'pointsAgainst'))).toEqual(['Birch', 'Cedar', 'Ash']);
    // Streak score: W1 = +1, D3 = 0, L2 = -2.
    expect(names(sortStandings([b, c, a], 'streak'))).toEqual(['Ash', 'Cedar', 'Birch']);
    expect(names(sortStandings([a, b, c], 'team', 'asc'))).toEqual(['Cedar', 'Birch', 'Ash']);
    expect(names(sortStandings([a, b, c], 'record', 'asc'))).toEqual(['Cedar', 'Birch', 'Ash']);
  });

  it('counts a streak back from the newest result only', () => {
    expect(streakOf(['W', 'W', 'L', 'W', 'W', 'W'])).toEqual({ result: 'W', count: 3 });
    expect(streakOf(['D'])).toEqual({ result: 'D', count: 1 });
  });
});

// ---------------------------------------------------------------- matchups

describe('matchup sides and results', () => {
  const t = (id: string) =>
    ({ kind: 'team', team: { id, name: id } }) as unknown as Schemas['Opponent'];
  const avg = { kind: 'average' } as unknown as Schemas['Opponent'];

  it('puts my team left, the average right, otherwise home left', () => {
    expect(orient({ home: t('x'), away: t('me') }, 'me')).toEqual({ left: t('me'), right: t('x') });
    expect(orient({ home: t('me'), away: t('x') }, 'me')).toEqual({ left: t('me'), right: t('x') });
    expect(orient({ home: avg, away: t('x') }, undefined)).toEqual({ left: t('x'), right: avg });
    expect(orient({ home: t('h'), away: t('a') }, 'me')).toEqual({ left: t('h'), right: t('a') });
  });

  it('finds my matchup home or away, and none without a team', () => {
    const ms = [
      { id: 1, home: t('a'), away: t('b') },
      { id: 2, home: t('c'), away: t('me') },
    ];
    expect(findMyMatchup(ms, 'me')?.id).toBe(2);
    expect(findMyMatchup(ms, 'a')?.id).toBe(1);
    expect(findMyMatchup(ms, undefined)).toBeUndefined();
    expect(findMyMatchup(ms, 'zz')).toBeUndefined();
  });

  it('gives W/D/L by one point once every match is played (RESEARCH.md §7 H2H)', () => {
    expect(resultFor(51, 50, 'final')).toBe('W');
    expect(resultFor(50, 51, 'final')).toBe('L');
    expect(resultFor(50, 50, 'final')).toBe('D');
    expect(resultFor(51, 50, 'provisional')).toBe('W');
    expect(resultFor(51, 50, 'live')).toBeNull();
    expect(resultFor(null, 50, 'final')).toBeNull();
    expect(resultFor(50, null, 'final')).toBeNull();
  });

  it('counts a postponed fixture as done (it is replayed in another gameweek)', () => {
    const f = (status: string) =>
      ({ status, minutes: 63, kickoffAt: '2026-10-17T14:00:00Z' }) as Schemas['PlayerFixture'];
    expect(fixtureState(f('postponed'))).toEqual({ text: 'Postponed', live: false, done: true });
    expect(fixtureState(f('finished_provisional'))).toEqual({
      text: 'FT',
      live: false,
      done: true,
    });
    expect(fixtureState(f('live'))).toEqual({ text: "63'", live: true, done: false });
    expect(fixtureState(f('scheduled')).done).toBe(false);
    expect(fixtureState(f('abandoned'))).toEqual({ text: '–', live: false, done: false });
  });

  it('reads every auto-sub state', () => {
    expect(autoSubOf('projected_in')).toEqual({ kind: 'in', projected: true });
    expect(autoSubOf('confirmed_in')).toEqual({ kind: 'in', projected: false });
    expect(autoSubOf('projected_out')).toEqual({ kind: 'out', projected: true });
    expect(autoSubOf('confirmed_out')).toEqual({ kind: 'out', projected: false });
    expect(autoSubOf(undefined)).toEqual({ kind: null, projected: false });
  });
});
