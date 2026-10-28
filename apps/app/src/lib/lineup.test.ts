import type { Schemas } from '@myfantasyprem/api-client';

import {
  checkSwap,
  DEFAULT_RULES,
  formationOf,
  lineupProblems,
  linesOf,
  swapPlayers,
  tapPlayer,
  toLineupInput,
  type LineupState,
} from './lineup';

const entry = (n: number, position: Schemas['Position'], slot = n): Schemas['LineupEntry'] => ({
  slot,
  player: {
    id: `plr_${String(n).padStart(26, '0')}`,
    displayName: `${position} ${n}`,
    club: { code: 'NOR', name: 'Northgate' },
    position,
    availability: 'available',
  },
  fixtures: [],
});
const id = (n: number) => `plr_${String(n).padStart(26, '0')}`;

/** 4-4-2 with a GK, DEF, MID, FWD bench. */
function lineup(): LineupState {
  const xi: Schemas['Position'][] = [
    'GKP',
    'DEF',
    'DEF',
    'DEF',
    'DEF',
    'MID',
    'MID',
    'MID',
    'MID',
    'FWD',
    'FWD',
  ];
  const bench: Schemas['Position'][] = ['GKP', 'DEF', 'MID', 'FWD'];
  return {
    starters: xi.map((p, i) => entry(i + 1, p)),
    bench: bench.map((p, i) => entry(i + 12, p)),
  };
}

describe('formation', () => {
  it('reads defenders-midfielders-forwards', () => {
    expect(formationOf(lineup().starters)).toBe('4-4-2');
  });

  it('groups the XI by line, goalkeeper first, keeping unknown positions', () => {
    const state = lineup();
    state.starters[10] = entry(11, 'COACH' as Schemas['Position']);
    const lines = linesOf(state.starters);
    expect(lines.map((l) => [l.position, l.entries.length])).toEqual([
      ['GKP', 1],
      ['DEF', 4],
      ['MID', 4],
      ['FWD', 1],
      ['Other', 1],
    ]);
  });

  it('accepts a legal lineup', () => {
    expect(lineupProblems(lineup())).toEqual([]);
  });
});

describe('swaps', () => {
  it('swaps a starter with a bench player and renumbers slots', () => {
    const next = swapPlayers(lineup(), id(10), id(15)); // FWD out, bench FWD in
    expect(next.starters[9]!.player.id).toBe(id(15));
    expect(next.starters[9]!.slot).toBe(10);
    expect(next.bench[3]!.player.id).toBe(id(10));
    expect(next.bench[3]!.slot).toBe(15);
    expect(formationOf(next.starters)).toBe('4-4-2');
  });

  it('allows a change of formation within the limits', () => {
    // FWD for the bench MID: 4-5-1.
    expect(checkSwap(lineup(), id(10), id(14))).toEqual({ kind: 'swap' });
    expect(formationOf(swapPlayers(lineup(), id(10), id(14)).starters)).toBe('4-5-1');
  });

  it('refuses a keeper for an outfield player, naming the broken rule', () => {
    const verdict = checkSwap(lineup(), id(1), id(13));
    expect(verdict).toEqual({ kind: 'invalid', reason: 'Start at least 1 goalkeeper.' });
  });

  it('keeps the backup keeper first on the bench', () => {
    const verdict = checkSwap(lineup(), id(12), id(13));
    expect(verdict.kind).toBe('invalid');
    expect(lineupProblems(swapPlayers(lineup(), id(12), id(13)))).toContain(
      'The first bench slot must be your backup goalkeeper.',
    );
    // Reordering outfield bench players is fine.
    expect(checkSwap(lineup(), id(13), id(15))).toEqual({ kind: 'swap' });
  });

  it('treats two starters as no swap', () => {
    expect(checkSwap(lineup(), id(2), id(6))).toEqual({ kind: 'none' });
    expect(checkSwap(lineup(), id(2), id(2))).toEqual({ kind: 'none' });
    expect(swapPlayers(lineup(), id(2), 'plr_missing')).toEqual(lineup());
  });

  it('builds the request body', () => {
    const body = toLineupInput(lineup(), 8);
    expect(body.gameweek).toBe(8);
    expect(body.starters).toHaveLength(11);
    expect(body.bench).toEqual([id(12), id(13), id(14), id(15)]);
  });
});

/** A lineup in a formation like '3-5-2', with a GK, DEF, MID, FWD bench. */
function inFormation(f: string): LineupState {
  const [d, m, fw] = f.split('-').map(Number) as [number, number, number];
  const xi: Schemas['Position'][] = [
    'GKP',
    ...Array<Schemas['Position']>(d).fill('DEF'),
    ...Array<Schemas['Position']>(m).fill('MID'),
    ...Array<Schemas['Position']>(fw).fill('FWD'),
  ];
  const bench: Schemas['Position'][] = ['GKP', 'DEF', 'MID', 'FWD'];
  return {
    starters: xi.map((p, i) => entry(i + 1, p)),
    bench: bench.map((p, i) => entry(i + 12, p)),
  };
}
/** The id of the n-th (0-based) starter at a position. */
const starter = (s: LineupState, p: string, n = 0) =>
  s.starters.filter((e) => e.player.position === p)[n]!.player.id;
const benchOf = (s: LineupState, p: string) =>
  s.bench.find((e) => e.player.position === p)!.player.id;

describe('swap selection (tapPlayer)', () => {
  it('selects on the first tap and deselects on a second tap of the same player', () => {
    const s = lineup();
    expect(tapPlayer(s, null, id(4))).toEqual({ kind: 'select', selected: id(4) });
    expect(tapPlayer(s, id(4), id(4))).toEqual({ kind: 'deselect' });
    // Bench players can be selected first too.
    expect(tapPlayer(s, null, id(14))).toEqual({ kind: 'select', selected: id(14) });
  });

  it('swaps a starter with a bench player of the same position', () => {
    const r = tapPlayer(lineup(), id(4), id(13)); // DEF ↔ bench DEF
    expect(r.kind).toBe('swap');
    if (r.kind !== 'swap') return;
    expect(r.next.starters[3]!.player.id).toBe(id(13));
    expect(r.next.bench[1]!.player.id).toBe(id(4));
    expect(formationOf(r.next.starters)).toBe('4-4-2');
  });

  it('swaps either way round and changes formation within the limits', () => {
    const r = tapPlayer(lineup(), id(14), id(10)); // bench MID first, then a FWD
    expect(r.kind).toBe('swap');
    if (r.kind === 'swap') expect(formationOf(r.next.starters)).toBe('4-5-1');
  });

  it('swaps keeper for keeper', () => {
    const r = tapPlayer(lineup(), id(1), id(12));
    expect(r.kind).toBe('swap');
    if (r.kind !== 'swap') return;
    expect(r.next.starters[0]!.player.id).toBe(id(12));
    expect(r.next.bench[0]!.player.id).toBe(id(1));
  });

  it('rejects a keeper for an outfield player, either way round', () => {
    expect(tapPlayer(lineup(), id(1), id(15))).toEqual({
      kind: 'reject',
      reason: 'Start at least 1 goalkeeper.',
    });
    expect(tapPlayer(lineup(), id(6), id(12))).toEqual({
      kind: 'reject',
      reason: 'Start at most 1 goalkeeper.',
    });
  });

  it.each([
    ['3-5-2', 'DEF', 'MID', /at least 3 defenders/],
    ['5-3-2', 'MID', 'DEF', /at most 5 defenders/],
    ['5-2-3', 'MID', 'FWD', /at least 2 midfielders/],
    ['4-5-1', 'DEF', 'MID', /at most 5 midfielders/],
    ['4-3-3', 'MID', 'FWD', /at most 3 forwards/],
    ['4-5-1', 'FWD', 'DEF', /at least 1 forward/],
  ])(
    'rejects a swap that breaks the formation limits in %s (%s off, bench %s on)',
    (f, off, on, why) => {
      const s = inFormation(f);
      const r = tapPlayer(s, starter(s, off), benchOf(s, on));
      expect(r.kind).toBe('reject');
      if (r.kind === 'reject') expect(r.reason).toMatch(why);
    },
  );

  it('respects the league rules it is given', () => {
    const s = inFormation('3-5-2');
    const strict = { ...DEFAULT_RULES, minStarters: { ...DEFAULT_RULES.minStarters, MID: 5 } };
    expect(tapPlayer(s, starter(s, 'DEF'), benchOf(s, 'DEF')).kind).toBe('swap');
    expect(tapPlayer(s, starter(s, 'MID'), benchOf(s, 'FWD'), strict)).toEqual({
      kind: 'reject',
      reason: 'Start at least 5 midfielders.',
    });
  });

  it('moves the selection to another starter instead of swapping two starters', () => {
    expect(tapPlayer(lineup(), id(2), id(6))).toEqual({ kind: 'select', selected: id(6) });
  });

  it('runs a whole sequence: select, change mind, swap', () => {
    let s = lineup();
    let selected: string | null = null;
    const tap = (t: string) => {
      const r = tapPlayer(s, selected, t);
      if (r.kind === 'select') selected = r.selected;
      else if (r.kind === 'swap') {
        s = r.next;
        selected = null;
      } else selected = null;
      return r.kind;
    };
    expect(tap(id(10))).toBe('select');
    expect(tap(id(10))).toBe('deselect');
    expect(tap(id(11))).toBe('select');
    expect(tap(id(12))).toBe('reject'); // a forward for the backup keeper
    expect(selected).toBeNull();
    expect(tap(id(11))).toBe('select');
    expect(tap(id(15))).toBe('swap');
    expect(s.starters.map((e) => e.player.id)).toContain(id(15));
    expect(s.bench[3]!.player.id).toBe(id(11));
  });
});
