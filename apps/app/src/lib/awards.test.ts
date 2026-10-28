import {
  compareForAward,
  formationOf,
  playerOfTheWeek,
  teamOfTheWeek,
  type AwardCandidate,
  type AwardStatLine,
} from './awards';

const ZERO: AwardStatLine = {
  points: 0,
  bonus: 0,
  bps: 0,
  goals: 0,
  assists: 0,
  defensiveContribution: 0,
  cleanSheets: 0,
  minutes: 90,
};

let seq = 0;
function c(position: string, stats: Partial<AwardStatLine>, id?: string): AwardCandidate {
  seq += 1;
  return { id: id ?? `p${String(seq).padStart(3, '0')}`, position, stats: { ...ZERO, ...stats } };
}

/** `n` players of a position with descending points from `top`. */
const line = (position: string, n: number, top: number) =>
  Array.from({ length: n }, (_, i) => c(position, { points: top - i }, `${position}-${i}`));

describe('playerOfTheWeek', () => {
  it('picks the most points', () => {
    const best = c('MID', { points: 18 });
    expect(playerOfTheWeek([c('DEF', { points: 12 }), best, c('FWD', { points: 17 })])).toBe(best);
  });

  it('returns null for an empty pool', () => {
    expect(playerOfTheWeek([])).toBeNull();
  });

  // Each step: two players level on every earlier rule, split by this one.
  const steps: [string, Partial<AwardStatLine>, Partial<AwardStatLine>][] = [
    ['goals', { goals: 2 }, { goals: 1, bonus: 3, bps: 40 }],
    ['assists', { goals: 1, assists: 1 }, { goals: 1, bonus: 3, bps: 40 }],
    ['bonus', { assists: 1, bonus: 3 }, { assists: 1, bonus: 2, bps: 45 }],
    ['BPS', { bonus: 3, bps: 41 }, { bonus: 3, bps: 38 }],
    ['defensive contribution', { defensiveContribution: 12 }, { defensiveContribution: 10 }],
    ['clean sheet', { defensiveContribution: 10, cleanSheets: 1 }, { defensiveContribution: 10 }],
  ];
  it.each(steps)('breaks a points tie on %s', (_, winner, loser) => {
    // The loser has the lower id, so only the rule under test can put the winner first.
    const a = c('DEF', { points: 14, ...loser }, 'a');
    const b = c('DEF', { points: 14, ...winner }, 'b');
    expect(playerOfTheWeek([a, b])).toBe(b);
    expect(playerOfTheWeek([b, a])).toBe(b);
  });

  it('a later rule never outranks an earlier one', () => {
    const moreGoals = c('MID', { points: 14, goals: 1, bps: 20 }, 'z');
    const moreEverythingElse = c(
      'MID',
      { points: 14, assists: 3, bonus: 3, bps: 60, defensiveContribution: 12, cleanSheets: 1 },
      'a',
    );
    expect(playerOfTheWeek([moreEverythingElse, moreGoals])).toBe(moreGoals);
  });

  it('puts a goal ahead of an assist on a points tie (Cherki over Cunha)', () => {
    const cunha = c('MID', { points: 10, assists: 1, bonus: 3, bps: 38 }, 'a');
    const cherki = c('MID', { points: 10, goals: 1, bonus: 1, bps: 27 }, 'b');
    expect(playerOfTheWeek([cunha, cherki])).toBe(cherki);
  });

  it('gives a full tie to the player with fewer minutes', () => {
    const level = { points: 12, bonus: 2, bps: 30, goals: 1, assists: 1, cleanSheets: 1 };
    const ninety = c('MID', { ...level, minutes: 90 }, 'a');
    const sixtyOne = c('MID', { ...level, minutes: 61 }, 'b');
    expect(playerOfTheWeek([ninety, sixtyOne])).toBe(sixtyOne);
    expect(compareForAward(sixtyOne, ninety)).toBeLessThan(0);
  });

  it('falls back to the id when every rule ties, whatever the input order', () => {
    const a = c('FWD', { points: 9 }, 'plr_a');
    const b = c('FWD', { points: 9 }, 'plr_b');
    expect(playerOfTheWeek([b, a])).toBe(a);
    expect(playerOfTheWeek([a, b])).toBe(a);
  });

  it('counts players of unknown positions', () => {
    const odd = c('WING', { points: 30 });
    expect(playerOfTheWeek([c('MID', { points: 20 }), odd])).toBe(odd);
  });
});

describe('teamOfTheWeek', () => {
  const ids = (team: AwardCandidate[]) => team.map((p) => p.id);

  it('takes the minimum of each line, then the best four outfielders', () => {
    const pool = [
      ...line('GKP', 3, 9),
      ...line('DEF', 6, 10),
      ...line('MID', 6, 14),
      ...line('FWD', 4, 13),
    ];
    const team = teamOfTheWeek(pool);
    expect(team).toHaveLength(11);
    // DEF 10,9,8 + MID 14,13 + FWD 13 are the minimums; the next best
    // outfielders are FWD 12, MID 12, MID 11, FWD 11.
    expect(ids(team)).toEqual([
      'GKP-0',
      'DEF-0',
      'DEF-1',
      'DEF-2',
      'MID-0',
      'MID-1',
      'MID-2',
      'MID-3',
      'FWD-0',
      'FWD-1',
      'FWD-2',
    ]);
    expect(formationOf(team)).toBe('3-4-3');
  });

  it('caps defenders at five when six outscore everyone else', () => {
    const pool = [
      ...line('GKP', 2, 6),
      ...line('DEF', 6, 20), // 20..15
      ...line('MID', 4, 5),
      ...line('FWD', 3, 4),
    ];
    const team = teamOfTheWeek(pool);
    expect(team).toHaveLength(11);
    expect(team.filter((p) => p.position === 'DEF').map((p) => p.id)).toEqual([
      'DEF-0',
      'DEF-1',
      'DEF-2',
      'DEF-3',
      'DEF-4',
    ]);
    expect(ids(team)).not.toContain('DEF-5');
    expect(formationOf(team)).toBe('5-3-2');
  });

  it('caps midfielders at five and forwards at three', () => {
    const pool = [
      ...line('GKP', 1, 2),
      ...line('DEF', 3, 1),
      ...line('MID', 8, 30),
      ...line('FWD', 6, 25),
    ];
    const team = teamOfTheWeek(pool);
    expect(formationOf(team)).toBe('3-5-2');
    // Minimums take MID 30, 29 and FWD 25. The next four best are MID 28,
    // 27, 26 (the fifth midfielder) and, with MID now capped, FWD 24 rather
    // than MID 25.
    expect(team.filter((p) => p.position === 'MID')).toHaveLength(5);
    expect(team.filter((p) => p.position === 'FWD').map((p) => p.id)).toEqual(['FWD-0', 'FWD-1']);
  });

  it('never fields more than three forwards', () => {
    const pool = [
      ...line('GKP', 1, 2),
      ...line('DEF', 3, 1),
      ...line('MID', 2, 1),
      ...line('FWD', 6, 30),
    ];
    const team = teamOfTheWeek(pool);
    expect(team.filter((p) => p.position === 'FWD')).toHaveLength(3);
    expect(team).toHaveLength(9); // the pool has no more legal players
  });

  it('always plays exactly one keeper, even when keepers top the scoring', () => {
    const pool = [
      ...line('GKP', 4, 25),
      ...line('DEF', 5, 6),
      ...line('MID', 5, 5),
      ...line('FWD', 3, 4),
    ];
    const team = teamOfTheWeek(pool);
    expect(team.filter((p) => p.position === 'GKP').map((p) => p.id)).toEqual(['GKP-0']);
    expect(team).toHaveLength(11);
  });

  it('keeps a minimum line even when its players score least', () => {
    const pool = [
      ...line('GKP', 1, 5),
      ...line('DEF', 8, 20),
      ...line('MID', 8, 19),
      ...line('FWD', 2, 0),
    ];
    const team = teamOfTheWeek(pool);
    expect(team.filter((p) => p.position === 'FWD').map((p) => p.id)).toEqual(['FWD-0']);
  });

  it('uses the award tie-breaks for places', () => {
    const fewerMinutes = c('MID', { points: 8, minutes: 70 }, 'mid-b');
    const moreMinutes = c('MID', { points: 8, minutes: 90 }, 'mid-a');
    const pool = [
      ...line('GKP', 1, 5),
      ...line('DEF', 5, 3),
      c('MID', { points: 10 }, 'mid-top'),
      moreMinutes,
      fewerMinutes,
      ...line('FWD', 3, 2),
    ];
    // The second minimum midfield place goes to the 8 with fewer minutes;
    // the other 8 still wins one of the four open places.
    const team = teamOfTheWeek(pool);
    const mids = team.filter((p) => p.position === 'MID').map((p) => p.id);
    expect(mids).toEqual(['mid-top', 'mid-b', 'mid-a']);
  });

  it('ignores players of unknown positions', () => {
    const pool = [
      c('WING', { points: 50 }, 'wing'),
      ...line('GKP', 1, 5),
      ...line('DEF', 5, 5),
      ...line('MID', 5, 5),
      ...line('FWD', 3, 5),
    ];
    const team = teamOfTheWeek(pool);
    expect(ids(team)).not.toContain('wing');
    expect(team).toHaveLength(11);
  });

  it('is order independent', () => {
    const pool = [
      ...line('GKP', 2, 7),
      ...line('DEF', 7, 11),
      ...line('MID', 7, 12),
      ...line('FWD', 4, 12),
    ];
    const a = ids(teamOfTheWeek(pool));
    const b = ids(teamOfTheWeek([...pool].reverse()));
    expect(b).toEqual(a);
  });
});
