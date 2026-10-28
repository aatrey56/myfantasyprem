import type { Schemas } from '@myfantasyprem/api-client';

import { cardLeagueFor, matchEvents, minutesText, mineCount, ownerBadges } from './fixture-detail';

const NONE: Schemas['FixturePlayerStats'] = {
  goalsScored: 0,
  assists: 0,
  ownGoals: 0,
  yellowCards: 0,
  redCards: 0,
  saves: 0,
  penaltiesSaved: 0,
  penaltiesMissed: 0,
  bonus: 0,
};

const owner = (league: string, team: string, mine = false): Schemas['FixtureOwner'] => ({
  leagueId: league,
  leagueName: `League ${league}`,
  team: { id: `tm_${team}`, name: team, managerName: 'X' },
  mine,
});

describe('matchEvents', () => {
  it('is empty for a quiet match', () => {
    expect(matchEvents(NONE)).toEqual([]);
  });

  it('reads like a box score: goals, assists, cards, saves, bonus', () => {
    const e = matchEvents({ ...NONE, goalsScored: 2, assists: 1, yellowCards: 1, bonus: 3 });
    expect(e.map((x) => x.label)).toEqual(['2G', 'A', 'YC', 'B3']);
    expect(e.map((x) => x.a11y)).toEqual(['2 goals', '1 assist', 'yellow card', '3 bonus']);
    expect(e.map((x) => x.tone)).toEqual(['good', 'good', 'bad', 'good']);
  });

  it('covers keepers and mishaps', () => {
    const e = matchEvents({
      ...NONE,
      saves: 4,
      penaltiesSaved: 1,
      ownGoals: 1,
      redCards: 1,
      penaltiesMissed: 2,
    });
    expect(e.map((x) => x.label)).toEqual(['OG', 'PS', '2PM', 'RC', '4 SV']);
    expect(e.map((x) => x.a11y)).toEqual([
      '1 own goal',
      '1 penalty saved',
      '2 penalties missed',
      'red card',
      '4 saves',
    ]);
  });
});

describe('ownerBadges', () => {
  it('puts my team first and keeps the league order otherwise', () => {
    const badges = ownerBadges([
      owner('lg_a', "Gaffer's Army"),
      owner('lg_b', 'Sunday League Heroes', true),
      owner('lg_c', 'pub team'),
    ]);
    expect(badges.map((b) => [b.initials, b.mine])).toEqual([
      ['SL', true],
      ['GA', false],
      ['PT', false],
    ]);
    expect(badges[0]!.a11y).toBe('Sunday League Heroes in League lg_b (your team)');
    expect(badges[1]!.a11y).toBe("Gaffer's Army in League lg_a");
  });

  it('is empty for an unowned player', () => {
    expect(ownerBadges([])).toEqual([]);
  });
});

describe('cardLeagueFor', () => {
  it('opens the card where the player is mine, else where he is owned, else my first league', () => {
    expect(cardLeagueFor([owner('lg_a', 'A'), owner('lg_b', 'B', true)], 'lg_x')).toBe('lg_b');
    expect(cardLeagueFor([owner('lg_a', 'A')], 'lg_x')).toBe('lg_a');
    expect(cardLeagueFor([], 'lg_x')).toBe('lg_x');
    expect(cardLeagueFor([], null)).toBeNull();
  });
});

describe('mineCount and minutesText', () => {
  it('counts my players across both sides', () => {
    const p = (owners: Schemas['FixtureOwner'][]) =>
      ({ owners }) as unknown as Schemas['FixturePlayer'];
    const detail = {
      home: { players: [p([owner('a', 'A', true)]), p([])] },
      away: { players: [p([owner('a', 'B'), owner('b', 'C', true)])] },
    } as unknown as Schemas['FixtureDetail'];
    expect(mineCount(detail)).toBe(2);
  });

  it('shows minutes played, a dash before he comes on', () => {
    expect(minutesText(90)).toBe("90'");
    expect(minutesText(0)).toBe('–');
  });
});
