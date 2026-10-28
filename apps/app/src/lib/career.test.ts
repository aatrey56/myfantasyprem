import { CAREER_COLUMNS, careerCell, careerRows, careerTotals, type PastSeason } from './career';

const col = (key: string) => CAREER_COLUMNS.find((c) => c.key === key)!;

const season = (label: string, s: Partial<PastSeason>): PastSeason => ({
  season: label,
  points: null,
  minutes: null,
  goalsScored: null,
  assists: null,
  cleanSheets: null,
  bonus: null,
  starts: null,
  expectedGoals: null,
  expectedAssists: null,
  expectedGoalInvolvements: null,
  ictIndex: null,
  defensiveContribution: null,
  ...s,
});

// Henderson's real 2015/16, 2016/17 and 2022/23 (FPL history_past), with
// what FPL did not track yet as null.
const s1516 = season('2015/16', {
  points: 57,
  minutes: 1290,
  goalsScored: 2,
  assists: 3,
  cleanSheets: 5,
  bonus: 3,
});
const s1617 = season('2016/17', {
  points: 73,
  minutes: 2116,
  goalsScored: 1,
  assists: 4,
  cleanSheets: 7,
  bonus: 9,
  ictIndex: 115.5,
});
const s2223 = season('2022/23', {
  points: 69,
  minutes: 2060,
  goalsScored: 0,
  assists: 3,
  cleanSheets: 8,
  bonus: 0,
  starts: 23,
  expectedGoals: 1.35,
  expectedAssists: 3.59,
  expectedGoalInvolvements: 4.93,
  ictIndex: 103.0,
});

describe('careerRows', () => {
  it('puts the newest season first', () => {
    expect(careerRows([s1516, s2223, s1617]).map((s) => s.season)).toEqual([
      '2022/23',
      '2016/17',
      '2015/16',
    ]);
  });
});

describe('careerTotals', () => {
  const t = careerTotals([s1516, s1617, s2223]);
  it('sums counts over every season', () => {
    expect(t.points).toBe(57 + 73 + 69); // 199
    expect(t.minutes).toBe(1290 + 2116 + 2060); // 5466
    expect(t.assists).toBe(3 + 4 + 3); // 10
    expect(t.goalsScored).toBe(3); // 2 + 1 + 0: a tracked 0 counts
    expect(t.bonus).toBe(12); // 3 + 9 + 0
  });
  it('sums a stat only over the seasons that tracked it', () => {
    expect(t.ictIndex).toBe(218.5); // 115.5 + 103.0; 2015/16 untracked
    expect(t.starts).toBe(23); // 2022/23 only
    expect(t.expectedGoals).toBe(1.35);
  });
  it('is null when no season tracked the stat', () => {
    expect(t.defensiveContribution).toBeNull();
    expect(careerTotals([]).points).toBeNull();
  });
  it('rounds float noise to the shown places', () => {
    const a = season('2023/24', { expectedGoals: 0.1 });
    const b = season('2024/25', { expectedGoals: 0.2 });
    expect(careerTotals([a, b]).expectedGoals).toBe(0.3); // not 0.30000000000000004
  });
});

describe('careerCell', () => {
  it('shows untracked as a dash, never 0', () => {
    expect(careerCell(null, col('starts'))).toBe('—');
    expect(careerCell(undefined, col('ictIndex'))).toBe('—');
  });
  it('shows a tracked 0 as 0', () => {
    expect(careerCell(0, col('goalsScored'))).toBe('0');
    expect(careerCell(0, col('expectedGoals'))).toBe('0.00');
  });
  it('formats decimals at the column places', () => {
    expect(careerCell(1.35, col('expectedGoals'))).toBe('1.35');
    expect(careerCell(103, col('ictIndex'))).toBe('103.0');
  });
  it('labels assists as FPL assists', () => {
    expect(col('assists').title).toBe('FPL assists');
  });
});
