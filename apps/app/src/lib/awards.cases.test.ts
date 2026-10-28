/**
 * The award cases shared with the Go server (server/internal/scoring,
 * awards_test.go runs the same file). A rule change here fails there until
 * both sides agree, so the mocks and the server cannot drift.
 */
import cases from '../../../../server/internal/scoring/testdata/awards_cases.json';
import {
  compareForAward,
  formationOf,
  playerOfTheWeek,
  teamOfTheWeek,
  type AwardCandidate,
  type AwardStatLine,
} from './awards';

type Entry =
  | { id: string; position: string; stats?: Partial<AwardStatLine> }
  | { line: string; n: number; top: number };

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

const candidate = (e: { id: string; position: string; stats?: Partial<AwardStatLine> }) => ({
  id: e.id,
  position: e.position,
  stats: { ...ZERO, ...e.stats },
});

function expand(entries: readonly Entry[]): AwardCandidate[] {
  return entries.flatMap((e) =>
    'line' in e
      ? Array.from({ length: e.n }, (_, i) => ({
          id: `${e.line}-${i}`,
          position: e.line,
          stats: { ...ZERO, points: e.top - i },
        }))
      : [candidate(e)],
  );
}

const orders = (pool: AwardCandidate[]) => [pool, [...pool].reverse()];

describe('shared award cases: playerOfTheWeek', () => {
  it.each(cases.playerOfTheWeek.map((c) => [c.name, c] as const))('%s', (_, c) => {
    for (const pool of orders(expand(c.pool as Entry[]))) {
      expect(playerOfTheWeek(pool)?.id ?? null).toBe(c.want);
    }
  });
});

describe('shared award cases: compareForAward', () => {
  it.each(cases.compare.map((c) => [c.name, c] as const))('%s', (_, c) => {
    expect(Math.sign(compareForAward(candidate(c.a), candidate(c.b)))).toBe(c.want);
    expect(Math.sign(compareForAward(candidate(c.b), candidate(c.a)))).toBe(-c.want || 0);
  });
});

describe('shared award cases: teamOfTheWeek', () => {
  it.each(cases.teamOfTheWeek.map((c) => [c.name, c] as const))('%s', (_, c) => {
    for (const pool of orders(expand(c.pool as Entry[]))) {
      const team = teamOfTheWeek(pool);
      expect(team.map((p) => p.id)).toEqual(c.want);
      expect(formationOf(team)).toBe(c.formation);
    }
  });
});
