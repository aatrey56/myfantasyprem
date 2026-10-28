import {
  accuracy,
  accuracyText,
  difficultyLookup,
  difficultyText,
  difficultyTier,
  expectedPoints,
  expectedText,
  finalOdds,
  fixtureFactor,
  oddsFor,
  oddsSeed,
  optimalXI,
  percentText,
  rankDifficulty,
  recentGameweeks,
  seasonAccuracy,
  teamExpected,
  winOdds,
  type Scored,
  type SimTeam,
} from './projections';

// Expected values are worked out by hand from docs/adr/0014-projections.md
// (the same tables as server/internal/projection/projection_test.go).

describe('recentGameweeks', () => {
  const done =
    (...gws: number[]) =>
    (gw: number) =>
      gws.includes(gw);
  it('takes the last six completed gameweeks before the given one', () => {
    expect(recentGameweeks(10, done(1, 2, 3, 4, 5, 6, 7, 8, 9))).toEqual([4, 5, 6, 7, 8, 9]);
    // GW 9 not finished: 3..8.
    expect(recentGameweeks(10, done(1, 2, 3, 4, 5, 6, 7, 8))).toEqual([3, 4, 5, 6, 7, 8]);
    expect(recentGameweeks(3, done(1, 2))).toEqual([1, 2]);
    expect(recentGameweeks(1, done())).toEqual([]);
  });
});

describe('rankDifficulty', () => {
  // f1 A v B, f2 C v D, f3 A v C. MID conceded: A 2+1 = 3 in 2 (1.5 a
  // match), B 6 in 1, C 8+5 = 13 in 2 (6.5), D 2 in 1 -> A 1st, D 2nd,
  // B 3rd, C 4th. FWD: A 6 in 2 = 3, B 0, C 0, D 3 in 1 = 3 -> B, C 1st;
  // A, D 3rd.
  const matches = [
    { fixtureId: 'f1', home: 'A', away: 'B' },
    { fixtureId: 'f2', home: 'C', away: 'D' },
    { fixtureId: 'f3', home: 'A', away: 'C' },
  ];
  const lines = [
    { fixtureId: 'f1', club: 'A', position: 'MID', points: 6 },
    { fixtureId: 'f1', club: 'B', position: 'MID', points: 2 },
    { fixtureId: 'f2', club: 'C', position: 'MID', points: 2 },
    { fixtureId: 'f2', club: 'D', position: 'MID', points: 4 },
    { fixtureId: 'f2', club: 'D', position: 'MID', points: 4 },
    { fixtureId: 'f3', club: 'A', position: 'MID', points: 5 },
    { fixtureId: 'f3', club: 'C', position: 'MID', points: 1 },
    { fixtureId: 'f1', club: 'B', position: 'FWD', points: 4 },
    { fixtureId: 'f1', club: 'A', position: 'FWD', points: 0 },
    { fixtureId: 'f3', club: 'C', position: 'FWD', points: 2 },
    { fixtureId: 'f2', club: 'C', position: 'FWD', points: 3 },
    { fixtureId: 'f9', club: 'A', position: 'MID', points: 50 },
    { fixtureId: 'f1', club: 'C', position: 'MID', points: 50 },
  ] as const;
  const d = rankDifficulty(matches, lines);
  it('ranks per match, hardest first', () => {
    const mid = d.get('MID')!;
    expect(['A', 'D', 'B', 'C'].map((c) => mid.get(c)!.rank)).toEqual([1, 2, 3, 4]);
    expect(mid.get('C')).toEqual({ club: 'C', rank: 4, conceded: 13, matches: 2 });
    expect(mid.get('D')).toEqual({ club: 'D', rank: 2, conceded: 2, matches: 1 });
  });
  it('shares the better rank on a tie', () => {
    const fwd = d.get('FWD')!;
    expect(['B', 'C', 'A', 'D'].map((c) => fwd.get(c)!.rank)).toEqual([1, 1, 3, 3]);
    expect([...d.get('GKP')!.values()].map((r) => r.rank)).toEqual([1, 1, 1, 1]);
  });
});

describe('fixtureFactor and expectedPoints', () => {
  it('runs from 0.8 (1st) to 1.2 (20th), 1 unranked', () => {
    expect(fixtureFactor(1)).toBeCloseTo(0.8, 12);
    expect(fixtureFactor(20)).toBeCloseTo(1.2, 12);
    expect(fixtureFactor(null)).toBe(1);
    // 0.8 + 0.4 x 9 / 19 = 0.98947...
    expect(fixtureFactor(10)).toBeCloseTo(0.989473684, 8);
  });
  it.each([
    [6.0, [20], 7.2], // 6.0 x 1.2
    [6.0, [1], 4.8], // 6.0 x 0.8
    [6.0, [20, 1], 12.0], // a double: 6.0 x 2.0
    [6.0, [], 0], // a blank
    [4.5, [10], 4.5], // 4.4526 -> 4.5
    [5.0, [11], 5.1], // 5.0526 -> 5.1
    [5.9, [1], 4.7], // 4.72 -> 4.7
    [0.7, [1], 0.6], // 0.56 -> 0.6
    [-1.0, [20], -1.2],
  ])('form %p v ranks %p -> %p', (form, ranks, want) => {
    expect(expectedPoints(form, ranks)).toBeCloseTo(want, 9);
  });
  it('shows one decimal or a dash', () => {
    expect(expectedText(7.2)).toBe('7.2');
    expect(expectedText(12)).toBe('12.0');
    expect(expectedText(null)).toBe('–');
  });
});

describe('difficulty chips', () => {
  it('bands ranks in fours, hardest first', () => {
    expect([1, 4, 5, 8, 9, 12, 13, 16, 17, 20].map(difficultyTier)).toEqual([
      1, 1, 2, 2, 3, 3, 4, 4, 5, 5,
    ]);
  });
  it('prints the ordinal', () => {
    expect(difficultyText(18)).toBe('(18th)');
    expect(difficultyText(1)).toBe('(1st)');
    expect(difficultyText(2)).toBe('(2nd)');
    expect(difficultyText(13)).toBe('(13th)');
  });
  it('looks a rank up by position and club', () => {
    const rank = difficultyLookup({
      formGameweek: 7,
      recentGameweeks: [1, 2, 3, 4, 5, 6],
      positions: [
        {
          position: 'FWD',
          clubs: [
            { club: { code: 'NOR', name: 'Northgate' }, rank: 18, pointsConceded: 30, matches: 6 },
          ],
        },
      ],
    });
    expect(rank('FWD', 'NOR')).toBe(18);
    expect(rank('MID', 'NOR')).toBeNull();
    expect(difficultyLookup(undefined)('FWD', 'NOR')).toBeNull();
  });
});

describe('winOdds', () => {
  const one = (id: string, actual: number, remaining: number, history: number[]): SimTeam => ({
    id,
    starters: [{ actual, remaining, history }],
  });
  const seed = oddsSeed('mu_x', 5, 0);
  it('is certain when the worst week beats the best', () => {
    expect(winOdds([one('h', 0, 1, [5, 6]), one('a', 0, 1, [1, 2])], 'h', 'a', seed)).toEqual({
      home: 1,
      draw: 0,
      away: 0,
    });
  });
  it('splits coin flips 1/4, 1/2, 1/4', () => {
    // (2,2) D, (2,4) A, (4,2) H, (4,4) D
    const o = winOdds([one('h', 0, 1, [2, 4]), one('a', 0, 1, [2, 4])], 'h', 'a', seed);
    expect(o.home).toBeCloseTo(0.25, 1);
    expect(o.draw).toBeCloseTo(0.5, 1);
    expect(o.away).toBeCloseTo(0.25, 1);
  });
  it('keeps live points and resamples only what is left', () => {
    // 7 already v a draw of 5 (lose) or 9 (win).
    const o = winOdds([one('h', 7, 0, [1, 1]), one('a', 0, 1, [5, 9])], 'h', 'a', seed);
    expect(o.home).toBeCloseTo(0.5, 1);
    expect(o.draw).toBe(0);
  });
  it('plays the average as floor(mean of every team)', () => {
    // 10, 4, 7 -> floor(21 / 3) = 7.
    const teams = [one('t1', 10, 0, []), one('t2', 4, 0, []), one('t3', 7, 0, [])];
    expect(winOdds(teams, 't1', null, seed)).toEqual({ home: 1, draw: 0, away: 0 });
    expect(winOdds(teams, 't3', null, seed)).toEqual({ home: 0, draw: 1, away: 0 });
    expect(winOdds(teams, null, 't2', seed)).toEqual({ home: 1, draw: 0, away: 0 });
    // -3, 0 -> floor(-1.5) = -2: 0 beats it.
    expect(winOdds([one('a', -3, 0, []), one('b', 0, 0, [])], 'b', null, seed).home).toBe(1);
  });
  it('is symmetric, deterministic and sums to 1', () => {
    const teams = [one('h', 2, 2, [0, 3, 8, 12]), one('a', 5, 1, [1, 2, 6, 9, 14])];
    const o = winOdds(teams, 'h', 'a', seed, 500);
    const s = winOdds(teams, 'a', 'h', seed, 500);
    expect(s).toEqual({ home: o.away, draw: o.draw, away: o.home });
    expect(winOdds(teams, 'h', 'a', seed, 500)).toEqual(o);
    expect(o.home + o.draw + o.away).toBeCloseTo(1, 12);
    expect(oddsSeed('mu_x', 5, 0)).not.toBe(oddsSeed('mu_x', 5, 1));
  });
  it('turns a finished gameweek into the result', () => {
    expect(finalOdds(50, 40)).toEqual({ home: 1, draw: 0, away: 0 });
    expect(finalOdds(45, 45)).toEqual({ home: 0, draw: 1, away: 0 });
  });
  it('orients and prints chances', () => {
    expect(oddsFor({ home: 0.6, draw: 0.1, away: 0.3 }, false)).toEqual({
      left: 0.3,
      right: 0.6,
      draw: 0.1,
    });
    expect(percentText(0.583)).toBe('58%');
    expect(percentText(0.004)).toBe('<1%');
    expect(percentText(0.996)).toBe('>99%');
    expect(percentText(1)).toBe('100%');
    expect(percentText(0)).toBe('0%');
  });
});

describe('optimalXI and accuracy', () => {
  const s = (id: string, position: Scored['position'], points: number): Scored => ({
    id,
    position,
    points,
  });
  it('finds the best legal XI by hand', () => {
    // GKP 6; DEF 8, 7, 1; MID 10, 9, 3, 2; FWD 12, 5, 4 -> 3-4-3:
    // 6 + 16 + 24 + 21 = 67.
    const squad = [
      s('g1', 'GKP', 6),
      s('g2', 'GKP', 2),
      s('d1', 'DEF', 8),
      s('d2', 'DEF', 7),
      s('d3', 'DEF', 1),
      s('d4', 'DEF', 0),
      s('d5', 'DEF', 0),
      s('m1', 'MID', 10),
      s('m2', 'MID', 9),
      s('m3', 'MID', 3),
      s('m4', 'MID', 2),
      s('m5', 'MID', 2),
      s('f1', 'FWD', 12),
      s('f2', 'FWD', 5),
      s('f3', 'FWD', 4),
    ];
    expect(optimalXI(squad)).toEqual({
      ids: ['g1', 'd1', 'd2', 'd3', 'm1', 'm2', 'm3', 'm4', 'f1', 'f2', 'f3'],
      points: 67,
    });
  });
  it('still starts a forward who scored -2', () => {
    // GKP 0 + 5 DEF x 1 + 4 MID x 1 + FWD -2 = 7.
    const squad = [
      s('g1', 'GKP', 0),
      s('g2', 'GKP', 0),
      ...['d1', 'd2', 'd3', 'd4', 'd5'].map((id) => s(id, 'DEF', 1)),
      ...['m1', 'm2', 'm3', 'm4', 'm5'].map((id) => s(id, 'MID', 1)),
      ...['f1', 'f2', 'f3'].map((id) => s(id, 'FWD', -2)),
    ];
    expect(optimalXI(squad)?.points).toBe(7);
  });
  it('has no XI without a goalkeeper', () => {
    expect(optimalXI([s('d1', 'DEF', 5)])).toBeNull();
  });
  it('divides by the optimal and averages the weeks', () => {
    expect(accuracy(55, 67)).toBeCloseTo(55 / 67, 12);
    expect(accuracy(0, 0)).toBeNull();
    expect(seasonAccuracy([1, 0.5, 0.75])).toBe(0.75);
    expect(seasonAccuracy([])).toBeNull();
    expect(accuracyText(0.8209)).toBe('82%');
    expect(accuracyText(null)).toBe('–');
  });
});

describe('teamExpected', () => {
  it('adds the starters in tenths and skips the unknown', () => {
    const e = new Map<string, number | null>([
      ['a', 4.1],
      ['b', 0.2],
      ['c', null],
      ['bench', 9.9],
    ]);
    // 4.1 + 0.2 = 4.3 exactly (not 4.299999...); c has none; bench not counted.
    expect(teamExpected(['a', 'b', 'c'], e)).toBe(4.3);
    expect(teamExpected(['c'], e)).toBeNull();
    expect(teamExpected([], e)).toBeNull();
  });
});
