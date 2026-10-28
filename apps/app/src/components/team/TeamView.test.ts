import type { Schemas } from '@myfantasyprem/api-client';

import { statRank, teamAutoSubLine, teamStatLine } from './TeamView';

type Entry = Schemas['LineupEntry'];
const entry = (name: string, autoSub: Schemas['AutoSubState'] = 'none') =>
  ({ player: { displayName: name }, autoSub }) as unknown as Entry;

const lineup = (scoreState: string, starters: Entry[], bench: Entry[]) =>
  ({ scoreState, starters, bench }) as unknown as Schemas['Lineup'];

describe('teamAutoSubLine', () => {
  it('names one sub as a pair, projected until every game is played', () => {
    const l = lineup(
      'live',
      [entry('A', 'projected_out'), entry('X')],
      [entry('B', 'projected_in')],
    );
    expect(teamAutoSubLine(l)).toBe('Auto-sub projected: A → B');
    expect(teamAutoSubLine({ ...l, scoreState: 'provisional' })).toBe('Auto-sub: A → B');
  });

  it('lists several subs without guessing the pairs', () => {
    const l = lineup(
      'final',
      [entry('A', 'confirmed_out'), entry('C', 'confirmed_out')],
      [entry('B', 'confirmed_in'), entry('D', 'confirmed_in')],
    );
    expect(teamAutoSubLine(l)).toBe('Auto-subs: A, C → B, D');
  });

  it('is empty with no subs', () => {
    expect(teamAutoSubLine(lineup('live', [entry('A')], [entry('B')]))).toBeNull();
  });
});

describe('team card stat line', () => {
  const row = (id: string, pointsFor: number, pointsAgainst: number) =>
    ({ team: { id }, pointsFor, pointsAgainst }) as unknown as Schemas['StandingRow'];
  const standings = [
    row('a', 300, 250),
    row('b', 280, 290),
    row('me', 290, 260),
    row('c', 290, 200),
  ];
  const record = { wins: 3, draws: 0, losses: 3, rank: 5, pointsFor: 290 } as Schemas['Record'];

  it('ranks points for and against, most first, ties sharing a rank', () => {
    expect(statRank(standings, 'me', 'pointsFor')).toBe(2);
    expect(statRank(standings, 'c', 'pointsFor')).toBe(2);
    expect(statRank(standings, 'me', 'pointsAgainst')).toBe(2);
    expect(statRank(standings, 'nobody', 'pointsFor')).toBeNull();
  });

  it('reads like Sleeper: record, rank, PF rank, PA rank', () => {
    expect(teamStatLine(record, standings, 'me')).toBe('3-0-3 · 5th · PF 2nd · PA 2nd');
    // Without the standings, the points-for total as before.
    expect(teamStatLine(record, undefined, 'me')).toBe('3-0-3 · 5th · PF 290');
  });
});
