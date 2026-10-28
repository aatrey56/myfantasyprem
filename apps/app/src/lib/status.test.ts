import type { Schemas } from '@myfantasyprem/api-client';

import {
  attentionHref,
  clampOffset,
  defaultAwardsGameweek,
  fixtureCell,
  gameweekSummary,
  injuryBadge,
  leagueTable,
  myFixtures,
  ordinal,
  potwCardWidth,
  potwFirstShown,
  rankChangeLabel,
  sortAttention,
  topAvailableFor,
  totwLines,
  upcomingDeadlines,
} from './status';

const L = 'lg_01k7m2xq8rfv3c9a4t6wbn5hdy';
const team = (n: number, name = `Team ${n}`): Schemas['TeamRef'] => ({
  id: `tm_0000000000000000000000000${n}`,
  name,
  managerName: `Manager ${n}`,
});
const ME = team(1, 'Sunday League Heroes');
const THEM = team(2, 'Park The Bus FC');
const OTHER = team(3, "Gaffer's Army");
const FOURTH = team(4, 'Rotation Roulette');

const row = (t: Schemas['TeamRef'], rank: number, form: Schemas['FormResult'][]) =>
  ({
    rank,
    team: t,
    played: form.length,
    wins: 0,
    draws: 0,
    losses: 0,
    pointsFor: 0,
    pointsAgainst: 0,
    leaguePoints: 0,
    form,
  }) as Schemas['StandingRow'];

function home(
  state: Schemas['ScoreState'],
  mine: number,
  theirs: number,
  meAway = false,
): Schemas['LeagueHome'] {
  const my = { kind: 'team' as const, team: ME, points: mine };
  const opp = { kind: 'team' as const, team: THEM, points: theirs };
  return {
    viewer: { team: ME },
    currentGameweek: { number: 7 },
    standings: [row(OTHER, 1, ['W', 'W']), row(THEM, 2, ['W', 'L']), row(ME, 3, ['L', 'D'])],
    matchups: [
      {
        id: 'mu_1',
        gameweek: 7,
        state,
        home: meAway ? opp : my,
        away: meAway ? my : opp,
      },
      {
        id: 'mu_2',
        gameweek: 7,
        state,
        home: { kind: 'team', team: OTHER, points: 50 },
        away: { kind: 'average', points: 40, basis: 'floor_mean_all_teams' },
      },
    ],
  } as unknown as Schemas['LeagueHome'];
}

describe('ordinal', () => {
  it.each([
    [1, '1st'],
    [2, '2nd'],
    [3, '3rd'],
    [4, '4th'],
    [6, '6th'],
    [11, '11th'],
    [12, '12th'],
    [13, '13th'],
    [21, '21st'],
    [22, '22nd'],
    [111, '111th'],
  ])('%i → %s', (n, s) => expect(ordinal(n)).toBe(s));
});

describe('gameweekSummary', () => {
  it('shows a result only once the gameweek is final', () => {
    expect(gameweekSummary(home('live', 32, 42), -1)).toMatchObject({
      gameweek: 7,
      points: 32,
      opponentPoints: 42,
      opponentName: 'Park The Bus FC',
      result: null,
      rank: 3,
      rankChange: -1,
    });
    expect(gameweekSummary(home('final', 32, 42), 0).result).toBe('L');
    expect(gameweekSummary(home('final', 50, 42), 0).result).toBe('W');
    expect(gameweekSummary(home('final', 42, 42), 0).result).toBe('D');
  });

  it('puts my side first when I am the away team', () => {
    const s = gameweekSummary(home('final', 61, 40, true), 2);
    expect(s).toMatchObject({ points: 61, opponentPoints: 40, result: 'W' });
  });

  it('copes with a team that has no matchup', () => {
    const h = { ...home('live', 1, 2), matchups: [] } as Schemas['LeagueHome'];
    expect(gameweekSummary(h, 0)).toMatchObject({ points: null, result: null, rank: 3 });
  });
});

describe('rankChangeLabel', () => {
  it('marks moves up, down and none', () => {
    expect(rankChangeLabel(2)).toEqual({ text: '▲ 2', tone: 'up', a11y: 'up 2' });
    expect(rankChangeLabel(-1)).toEqual({ text: '▼ 1', tone: 'down', a11y: 'down 1' });
    expect(rankChangeLabel(0).tone).toBe('same');
  });
});

describe('attention', () => {
  it('links each known kind and leaves unknown kinds unlinked', () => {
    expect(attentionHref({ kind: 'trade_offers' }, L)).toBe(
      `/leagues/${L}/players/trades?from=status`,
    );
    expect(attentionHref({ kind: 'starters_without_fixture' }, L)).toBe(`/leagues/${L}/team`);
    expect(attentionHref({ kind: 'injured_starters' }, L)).toBe(`/leagues/${L}/team`);
    expect(attentionHref({ kind: 'pending_waivers' }, L)).toBe(
      `/leagues/${L}/players/waivers?from=status`,
    );
    expect(attentionHref({ kind: 'something_new' as never }, L)).toBeNull();
  });

  it('sorts critical, warning, info, then unknown severities, keeping server order on ties', () => {
    const items = [
      { id: 'a', severity: 'info' },
      { id: 'b', severity: 'loud' },
      { id: 'c', severity: 'warning' },
      { id: 'd', severity: 'critical' },
      { id: 'e', severity: 'warning' },
    ] as unknown as Schemas['StatusAttentionItem'][];
    expect(sortAttention(items).map((i) => (i as unknown as { id: string }).id)).toEqual([
      'd',
      'c',
      'e',
      'a',
      'b',
    ]);
  });
});

describe('upcomingDeadlines', () => {
  const t0 = Date.parse('2026-10-14T12:00:00Z');
  const gw = (n: number, deadline: string): Schemas['Gameweek'] => {
    const d = Date.parse(deadline);
    return {
      number: n,
      name: `Gameweek ${n}`,
      deadlineAt: new Date(d).toISOString(),
      waiverDeadlineAt: new Date(d - 24 * 3_600_000).toISOString(),
      tradeDeadlineAt: new Date(d - 48 * 3_600_000).toISOString(),
      finished: false,
    };
  };
  const waivers = {
    nextRunAt: null,
    priority: [
      { rank: 1, team: THEM },
      { rank: 6, team: ME },
    ],
  };

  it('skips passed deadlines and labels my waiver pick', () => {
    const d = upcomingDeadlines(
      { current: gw(7, '2026-10-13T10:00:00Z'), next: gw(8, '2026-10-17T05:00:00Z') },
      t0,
      waivers,
      ME.id,
    );
    expect(d.map((x) => [x.key, x.label])).toEqual([
      ['waivers', 'Waivers · you have 6th pick'],
      ['lock', 'GW 8 lineups lock'],
    ]);
    expect(d[0]!.at).toBe(Date.parse('2026-10-16T05:00:00Z'));
  });

  it('uses the current gameweek while its deadlines are ahead', () => {
    const d = upcomingDeadlines(
      { current: gw(7, '2026-10-16T05:00:00Z'), next: gw(8, '2026-10-23T05:00:00Z') },
      t0,
    );
    expect(d.map((x) => x.label)).toEqual(['Waivers process', 'GW 7 lineups lock']);
  });

  it('has nothing after the last gameweek locks', () => {
    expect(upcomingDeadlines({ current: gw(38, '2026-10-13T10:00:00Z'), next: null }, t0)).toEqual(
      [],
    );
  });
});

describe('top available', () => {
  const p = (id: string, position: string) => ({ id, player: { position } });
  const list = [p('a', 'MID'), p('b', 'DEF'), p('c', 'MID'), p('d', 'FWD'), p('e', 'GKP')];

  it('filters by position and keeps the order', () => {
    expect(topAvailableFor(list, 'all', 3).map((x) => x.id)).toEqual(['a', 'b', 'c']);
    expect(topAvailableFor(list, 'MID').map((x) => x.id)).toEqual(['a', 'c']);
    expect(topAvailableFor(list, 'GKP').map((x) => x.id)).toEqual(['e']);
  });

  it('formats fixture cells for singles, doubles and blanks', () => {
    const f = (code: string, isHome: boolean) =>
      ({ opponent: { code, name: code }, isHome }) as Schemas['UpcomingFixture'];
    expect(fixtureCell({ fixtures: [f('eve', false)] })).toBe('EVE (A)');
    expect(fixtureCell({ fixtures: [f('EVE', false), f('TOT', true)] })).toBe('EVE (A), TOT (H)');
    expect(fixtureCell({ fixtures: [] })).toBe('–');
  });
});

describe('injuryBadge', () => {
  const f = (availability: string, chanceOfPlaying: number | null) => ({
    player: { availability: availability as Schemas['PlayerAvailability'] },
    chanceOfPlaying,
  });

  it('is a red Out for injured, suspended, unavailable and unknown values', () => {
    expect(injuryBadge(f('injured', 0))).toEqual({
      kind: 'out',
      label: 'Out',
      a11y: 'Out, injured',
    });
    expect(injuryBadge(f('suspended', 0)).a11y).toBe('Out, suspended');
    expect(injuryBadge(f('unavailable', null)).kind).toBe('out');
    expect(injuryBadge(f('on_loan_elsewhere', null))).toMatchObject({
      kind: 'out',
      a11y: 'Out, status unknown',
    });
  });

  it('is a yellow percentage for doubtful players', () => {
    expect(injuryBadge(f('doubtful', 50))).toEqual({
      kind: 'doubt',
      label: '50%',
      a11y: 'Doubtful, 50% chance of playing',
    });
    expect(injuryBadge(f('doubtful', null)).label).toBe('Doubt');
  });
});

describe('leagueTable', () => {
  it('adds the last result, live gameweek points and marks my row', () => {
    const rows = leagueTable(home('live', 32, 42));
    expect(rows.map((r) => [r.rank, r.team.name, r.last, r.gameweekPoints, r.mine])).toEqual([
      [1, "Gaffer's Army", 'W', 50, false],
      [2, 'Park The Bus FC', 'L', 42, false],
      [3, 'Sunday League Heroes', 'D', 32, true],
    ]);
  });

  it('has no gameweek points before kickoff', () => {
    expect(leagueTable(home('upcoming', 0, 0)).every((r) => r.gameweekPoints === null)).toBe(true);
  });
});

describe('myFixtures', () => {
  const schedule: Schemas['ScheduleWeek'][] = [
    {
      gameweek: 8,
      matchups: [
        {
          id: 'mu_a',
          gameweek: 8,
          home: { kind: 'team', team: THEM },
          away: { kind: 'team', team: FOURTH },
        },
        {
          id: 'mu_b',
          gameweek: 8,
          home: { kind: 'team', team: OTHER },
          away: { kind: 'team', team: ME },
        },
      ],
    },
  ];

  it('gives this gameweek with the score and next gameweek from the schedule', () => {
    const f = myFixtures(home('live', 32, 42), schedule);
    expect(f.current).toEqual({
      gameweek: 7,
      left: 'Sunday League Heroes',
      right: 'Park The Bus FC',
      score: '32 – 42',
    });
    expect(f.next).toEqual({
      gameweek: 8,
      left: 'Sunday League Heroes',
      right: "Gaffer's Army",
      score: null,
    });
  });

  it('has no next fixture until the schedule loads', () => {
    expect(myFixtures(home('live', 1, 2), undefined).next).toBeNull();
  });
});

describe('awards helpers', () => {
  it('opens Team of the Week on the latest settled gameweek', () => {
    expect(
      defaultAwardsGameweek([
        { gameweek: 5, state: 'final' },
        { gameweek: 6, state: 'official' },
        { gameweek: 7, state: 'live' },
      ]),
    ).toBe(6);
    expect(defaultAwardsGameweek([{ gameweek: 1, state: 'live' }])).toBe(1);
    expect(defaultAwardsGameweek([])).toBeNull();
  });

  it('clamps the carousel so the row stays full', () => {
    expect(clampOffset(99, 7, 5)).toBe(2);
    expect(clampOffset(-1, 7, 5)).toBe(0);
    expect(clampOffset(1, 3, 5)).toBe(0);
  });

  it('sizes five cards and four gaps to fill the row', () => {
    expect(potwCardWidth(342, 4)).toBeCloseTo(65.2);
    expect(potwCardWidth(760, 10)).toBe(144);
    expect(potwCardWidth(0, 4)).toBe(0);
  });

  it('reads the leftmost card shown from the scroll offset, within range', () => {
    expect(potwFirstShown(0, 70, 7)).toBe(0);
    expect(potwFirstShown(140, 70, 7)).toBe(2);
    expect(potwFirstShown(100, 70, 7)).toBe(1);
    expect(potwFirstShown(9999, 70, 7)).toBe(2);
    expect(potwFirstShown(0, 70, 3)).toBe(0);
    expect(potwFirstShown(50, 0, 7)).toBe(0);
  });

  it('lines up keeper to forwards for the pitch', () => {
    const t = (position: string) => ({ player: { position } });
    const lines = totwLines([t('FWD'), t('DEF'), t('MID'), t('GKP'), t('DEF')]);
    expect(lines.map((l) => l.map((x) => x.player.position))).toEqual([
      ['GKP'],
      ['DEF', 'DEF'],
      ['MID'],
      ['FWD'],
    ]);
  });
});
