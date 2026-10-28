import type { Schemas } from '@myfantasyprem/api-client';

import { compareStandings, recordOf, sortStandings, streakOf, streakText } from './standings';

function row(
  name: string,
  w: number,
  d: number,
  l: number,
  pf: number,
  pa: number,
  form: Schemas['FormResult'][] = [],
  rank = 0,
): Schemas['StandingRow'] {
  return {
    rank,
    team: { id: `tm_${name}`, name, managerName: name },
    played: w + d + l,
    wins: w,
    draws: d,
    losses: l,
    pointsFor: pf,
    pointsAgainst: pa,
    leaguePoints: w * 3 + d,
    form,
  };
}

describe('standings order', () => {
  it('ranks by league points, then points for, then fewer against, then name', () => {
    const rows = [
      row('Delta', 3, 0, 1, 200, 150),
      row('Alpha', 3, 0, 1, 210, 160),
      row('Charlie', 3, 0, 1, 210, 140),
      row('Bravo', 3, 0, 1, 210, 140),
      row('Echo', 2, 3, 0, 300, 100),
    ];
    expect([...rows].sort(compareStandings).map((r) => r.team.name)).toEqual([
      // 9 points each except Echo (9 too: 2 wins + 3 draws), most points for first.
      'Echo',
      'Bravo',
      'Charlie',
      'Alpha',
      'Delta',
    ]);
  });

  it('sorts by a column and falls back to the league order on ties', () => {
    const rows = [
      row('A', 4, 0, 0, 250, 200, [], 1),
      row('B', 2, 0, 2, 250, 180, [], 2),
      row('C', 1, 0, 3, 260, 220, [], 3),
    ];
    expect(sortStandings(rows, 'pointsFor').map((r) => r.team.name)).toEqual(['C', 'A', 'B']);
    expect(sortStandings(rows, 'pointsFor', 'asc').map((r) => r.team.name)).toEqual([
      'B',
      'A',
      'C',
    ]);
    // Fewest against is "best".
    expect(sortStandings(rows, 'pointsAgainst').map((r) => r.team.name)).toEqual(['B', 'A', 'C']);
    expect(sortStandings(rows, 'rank').map((r) => r.rank)).toEqual([1, 2, 3]);
    expect(sortStandings(rows, 'team', 'asc').map((r) => r.team.name)).toEqual(['C', 'B', 'A']);
  });

  it('sorts by streak: longest winning run first, losing runs last', () => {
    const rows = [
      row('Lose2', 1, 0, 2, 1, 1, ['W', 'L', 'L']),
      row('Win3', 3, 0, 0, 1, 1, ['W', 'W', 'W']),
      row('Draw1', 1, 1, 0, 1, 1, ['W', 'D']),
      row('Win1', 2, 0, 1, 1, 1, ['W', 'L', 'W']),
    ];
    expect(sortStandings(rows, 'streak').map((r) => r.team.name)).toEqual([
      'Win3',
      'Win1',
      'Draw1',
      'Lose2',
    ]);
  });
});

describe('streaks', () => {
  it('counts the run from the newest result', () => {
    expect(streakOf(['L', 'W', 'W'])).toEqual({ result: 'W', count: 2 });
    expect(streakOf(['D'])).toEqual({ result: 'D', count: 1 });
    expect(streakOf([])).toBeNull();
    expect(streakText(['W', 'L', 'L', 'L'])).toBe('L3');
    expect(streakText([])).toBe('–');
    expect(recordOf(row('x', 4, 1, 2, 0, 0))).toBe('4-1-2');
  });
});
