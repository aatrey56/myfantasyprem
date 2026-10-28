import type { Schemas } from '@myfantasyprem/api-client';

import { lineupLocked, msUntilLock, plannedLineup } from './lineup-hooks';
import { RuleError } from './optimistic';

// The live case: gameweek 7 locked Sat 17 Oct 2026 10:00 UTC and its matches
// are on; gameweek 8 locks Fri 23 Oct 2026 17:30 UTC.
const GW7_DEADLINE = '2026-10-17T10:00:00Z';
const GW8_DEADLINE = '2026-10-23T17:30:00Z';
const SAT_NOON = Date.parse('2026-10-17T12:00:00Z');

const entry = (id: string, position: Schemas['Position'], slot: number) =>
  ({ slot, player: { id, position } }) as unknown as Schemas['LineupEntry'];

const STARTERS = [
  entry('gk', 'GKP', 1),
  ...['d1', 'd2', 'd3', 'd4'].map((id, i) => entry(id, 'DEF', 2 + i)),
  ...['m1', 'm2', 'm3', 'm4'].map((id, i) => entry(id, 'MID', 6 + i)),
  entry('f1', 'FWD', 10),
  entry('f2', 'FWD', 11),
];
const BENCH = [
  entry('bgk', 'GKP', 12),
  entry('bd', 'DEF', 13),
  entry('bm', 'MID', 14),
  entry('bf', 'FWD', 15),
];

const envelope = (gameweek: number, locksAt: string, locked: boolean, serverTime: string) => ({
  data: {
    gameweek,
    locksAt,
    locked,
    starters: STARTERS,
    bench: BENCH,
  } as unknown as Schemas['Lineup'],
  meta: { dataAsOf: serverTime, serverTime, stale: false },
});

/** Defender d1 off, bench defender bd on. */
const SUB = (gameweek: number) => ({
  gameweek,
  starters: STARTERS.map((e) => (e.player.id === 'd1' ? 'bd' : e.player.id)),
  bench: BENCH.map((e) => (e.player.id === 'bd' ? 'd1' : e.player.id)),
});

describe('editing gameweek 8 while gameweek 7 is locked', () => {
  // Fetched Saturday noon: the server's default lineup is gameweek 8.
  const gw8 = envelope(8, GW8_DEADLINE, false, '2026-10-17T12:00:00Z');

  it('lets the defender sub through: GW8 locks in 6 days 5 h 30 min', () => {
    expect(msUntilLock(gw8, SAT_NOON, SAT_NOON)).toBe(((6 * 24 + 5) * 60 + 30) * 60_000);
    expect(lineupLocked(gw8, SAT_NOON, SAT_NOON)).toBe(false);
    const plan = plannedLineup(gw8, SAT_NOON, SUB(8), undefined, SAT_NOON);
    expect(plan).not.toBeInstanceOf(RuleError);
    expect((plan as Schemas['Lineup']).bench.map((e) => e.player.id)).toContain('d1');
  });

  it('still allows it a minute before the GW8 deadline, and refuses it at the deadline', () => {
    const fri1729 = Date.parse('2026-10-23T17:29:00Z');
    const fri1730 = Date.parse(GW8_DEADLINE);
    expect(plannedLineup(gw8, SAT_NOON, SUB(8), undefined, fri1729)).not.toBeInstanceOf(RuleError);
    expect(plannedLineup(gw8, SAT_NOON, SUB(8), undefined, fri1730)).toBeInstanceOf(RuleError);
  });

  it('reads a GW7 lineup fetched before its deadline as locked, so the screen refetches', () => {
    // Fetched Sat 09:00, an hour before GW7 locked; the server said open.
    const sat0900 = Date.parse('2026-10-17T09:00:00Z');
    const gw7 = envelope(7, GW7_DEADLINE, false, '2026-10-17T09:00:00Z');
    expect(lineupLocked(gw7, sat0900, Date.parse('2026-10-17T09:59:59Z'))).toBe(false);
    expect(lineupLocked(gw7, sat0900, SAT_NOON)).toBe(true);
  });

  it('measures the deadline on the server clock, not a device running 2 h slow', () => {
    // The device reads 10:00 at server noon: GW8 is no closer for it.
    const device = SAT_NOON - 2 * 3_600_000;
    expect(msUntilLock(gw8, device, device)).toBe(msUntilLock(gw8, SAT_NOON, SAT_NOON));
  });
});
