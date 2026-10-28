import type { Schemas } from '@myfantasyprem/api-client';

import {
  autoSubOf,
  autoSubLine,
  autoSubSummary,
  beforeKickoff,
  leadOf,
  pointsShare,
  recordLine,
  streakLabel,
  teamRecord,
  teamRecords,
  yetToPlay,
  gameweekPlayed,
  findMyMatchup,
  leagueGameweek,
  matchupUpcoming,
  fixtureState,
  orient,
  pairBench,
  pairStarters,
  resultFor,
  scoreStateLabel,
  seasonStatTotals,
  sidePoints,
  statLabel,
} from './matchup';
import * as data from '@/mocks/data';

let n = 0;
function slot(position: string, s: number, extra: Partial<Schemas['LineupSlot']> = {}) {
  n += 1;
  return {
    slot: s,
    player: {
      id: `plr_${String(n).padStart(26, '0')}`,
      displayName: `${position} ${n}`,
      club: { code: 'NOR', name: 'Northgate' },
      position: position as Schemas['Position'],
      availability: 'available',
    },
    points: 2,
    minutes: 90,
    autoSub: 'none',
    fixtures: [],
    ...extra,
  } as Schemas['LineupSlot'];
}

/** An XI in the given formation (slots 1-11, GK first). */
function xi(formation: string): Schemas['LineupSlot'][] {
  const [d, m, f] = formation.split('-').map(Number) as [number, number, number];
  const positions = [
    'GKP',
    ...Array(d).fill('DEF'),
    ...Array(m).fill('MID'),
    ...Array(f).fill('FWD'),
  ];
  return positions.map((p, i) => slot(p, i + 1));
}

describe('pairStarters', () => {
  it('pairs GK→FWD and leaves a gap where one side has fewer of a position', () => {
    const rows = pairStarters(xi('4-4-2'), xi('3-5-2'));
    expect(rows.map((r) => r.position)).toEqual([
      'GKP',
      'DEF',
      'DEF',
      'DEF',
      'DEF',
      'MID',
      'MID',
      'MID',
      'MID',
      'MID',
      'FWD',
      'FWD',
    ]);
    // The fourth defender has no partner; the fifth midfielder has no left cell.
    expect(rows[4]!.left).not.toBeNull();
    expect(rows[4]!.right).toBeNull();
    expect(rows[9]!.left).toBeNull();
    expect(rows[9]!.right).not.toBeNull();
    // Every starter appears exactly once.
    expect(rows.filter((r) => r.left).length).toBe(11);
    expect(rows.filter((r) => r.right).length).toBe(11);
  });

  it('orders by slot within a position, whatever order the slots arrive in', () => {
    const left = xi('4-4-2').reverse();
    const rows = pairStarters(left, null);
    expect(rows.filter((r) => r.position === 'DEF').map((r) => r.left!.slot)).toEqual([2, 3, 4, 5]);
    expect(rows.every((r) => r.right === null)).toBe(true);
  });

  it('keeps players with an unknown position in an Other row at the end', () => {
    const left = [...xi('4-4-1'), slot('WING', 11)];
    const rows = pairStarters(left, xi('4-4-2'));
    expect(rows.at(-1)).toMatchObject({ position: 'Other', right: null });
    expect(rows.at(-1)!.left!.player.position).toBe('WING');
  });

  it('pairs a real mock matchup into 11 starters per side', () => {
    const summary = data
      .matchupSummaries(data.CURRENT_GW)
      .find((m) => m.home.kind === 'team' && m.away.kind === 'team')!;
    const d = data.matchupDetail(data.CURRENT_GW, summary.id)!;
    const home = d.home as Schemas['TeamLineup'];
    const away = d.away as Schemas['TeamLineup'];
    const rows = pairStarters(home.starters, away.starters);
    expect(rows.filter((r) => r.left).length).toBe(11);
    expect(rows.filter((r) => r.right).length).toBe(11);
    expect(rows[0]!.position).toBe('GKP');
  });
});

describe('pairBench', () => {
  it('numbers the bench 1-4 in slot order', () => {
    const bench = [slot('MID', 14), slot('GKP', 12), slot('FWD', 15), slot('DEF', 13)];
    const rows = pairBench(bench, null);
    expect(rows.map((r) => [r.order, r.left!.slot])).toEqual([
      [1, 12],
      [2, 13],
      [3, 14],
      [4, 15],
    ]);
  });
});

describe('orient', () => {
  const team = (id: string) => ({
    kind: 'team' as const,
    team: { id, name: id, managerName: 'x' },
    points: 1,
  });
  const average = { kind: 'average' as const, points: 40, basis: 'floor_mean_all_teams' as const };

  it('puts my team on the left', () => {
    expect(orient({ home: team('a'), away: team('me') }, 'me').left).toEqual(team('me'));
    expect(orient({ home: team('me'), away: team('b') }, 'me').left).toEqual(team('me'));
  });
  it('keeps home on the left when I am not playing, and the average always right', () => {
    expect(orient({ home: team('a'), away: team('b') }, 'me').left).toEqual(team('a'));
    const mixed: { home: Schemas['MatchupSide']; away: Schemas['MatchupSide'] } = {
      home: average,
      away: team('a'),
    };
    expect(orient(mixed, undefined).right).toEqual(average);
  });
  it('finds my matchup', () => {
    const ms: { id: string; home: Schemas['MatchupSide']; away: Schemas['MatchupSide'] }[] = [
      { id: '1', home: team('a'), away: team('b') },
      { id: '2', home: team('me'), away: average },
    ];
    expect(findMyMatchup(ms, 'me')?.id).toBe('2');
    expect(findMyMatchup(ms, undefined)).toBeUndefined();
    expect(sidePoints(average)).toBe(40);
  });
});

describe('labels', () => {
  it('gives W/D/L once every match is played, provisional bonus included', () => {
    expect(resultFor(50, 40, 'final')).toBe('W');
    expect(resultFor(40, 40, 'final')).toBe('D');
    expect(resultFor(30, 40, 'final')).toBe('L');
    expect(resultFor(50, 40, 'live')).toBeNull();
    expect(resultFor(50, 40, 'upcoming')).toBeNull();
    // Owner decision 2026-10-25: results count with provisional bonus.
    expect(resultFor(50, 40, 'provisional')).toBe('W');
    expect(resultFor(50, 40, 'official')).toBe('W');
  });

  it('labels score states and tolerates unknown ones', () => {
    expect(scoreStateLabel('live')).toMatchObject({ label: 'Live', dot: true });
    // No "bonus pending": once every match is over the gameweek reads final.
    expect(scoreStateLabel('provisional')).toEqual({ label: 'Final', tone: 'neutral' });
    expect(scoreStateLabel('official').label).toBe('Final');
    expect(scoreStateLabel('something_new').label).toBe('Scores');
  });

  it('describes a fixture from the player side', () => {
    const f = (status: string, minutes = 0) =>
      ({
        status,
        minutes,
        // Two days ahead of the real clock: a scheduled kickoff this week
        // reads "Sat 14:00" (a fixed date would read with its day and month
        // once a week has passed).
        kickoffAt: new Date(Date.now() + 2 * 86_400_000).toISOString(),
      }) as Schemas['PlayerFixture'];
    expect(fixtureState(f('live', 63))).toEqual({ text: "63'", live: true, done: false });
    expect(fixtureState(f('finished_provisional')).text).toBe('FT');
    expect(fixtureState(f('postponed')).text).toBe('Postponed');
    expect(fixtureState(f('scheduled')).text).toMatch(/^\w{3} \d\d:\d\d$/);
    expect(fixtureState(f('abandoned')).text).toBe('–');
  });

  it('reads auto-sub states', () => {
    expect(autoSubOf('projected_out')).toEqual({ kind: 'out', projected: true });
    expect(autoSubOf('confirmed_in')).toEqual({ kind: 'in', projected: false });
    expect(autoSubOf('none').kind).toBeNull();
    expect(autoSubOf(undefined).kind).toBeNull();
  });

  it('summarises auto-subs by name', () => {
    const starters = xi('4-4-2');
    const bench = [slot('GKP', 12), slot('DEF', 13), slot('MID', 14), slot('FWD', 15)];
    const side = {
      kind: 'team',
      team: { id: 't', name: 't', managerName: 'm' },
      formation: '4-4-2',
      totalPoints: 1,
      benchPoints: 0,
      starters,
      bench,
      autoSubs: [
        {
          status: 'projected',
          slotOut: 2,
          slotIn: 13,
          playerOutId: starters[1]!.player.id,
          playerInId: bench[1]!.player.id,
        },
      ],
    } as Schemas['TeamLineup'];
    const pair = `${starters[1]!.player.displayName} → ${bench[1]!.player.displayName}`;
    // Projected while any game is still to finish.
    expect(autoSubSummary(side, 'live')).toBe(`Auto-sub projected: ${pair}`);
    expect(autoSubSummary(side, 'upcoming')).toBe(`Auto-sub projected: ${pair}`);
    expect(autoSubSummary(side, 'something_new')).toBe(`Auto-sub projected: ${pair}`);
    // Plain once every game is played, bonus pending or not.
    for (const state of ['provisional', 'official', 'final']) {
      expect(autoSubSummary(side, state)).toBe(`Auto-sub: ${pair}`);
    }
    expect(autoSubSummary({ ...side, autoSubs: [] }, 'live')).toBeNull();
  });

  it('writes the auto-sub line for one or several subs', () => {
    expect(autoSubLine([{ out: 'A', in: 'B' }], false)).toBe('Auto-sub projected: A → B');
    expect(autoSubLine([{ out: 'A', in: 'B' }], true)).toBe('Auto-sub: A → B');
    expect(
      autoSubLine(
        [
          { out: 'A', in: 'B' },
          { out: 'C', in: 'D' },
        ],
        true,
      ),
    ).toBe('Auto-subs: A → B, C → D');
    expect(autoSubLine([], false)).toBeNull();
    expect(gameweekPlayed('live')).toBe(false);
    expect(gameweekPlayed('provisional')).toBe(true);
  });

  it('labels stats and sums a season by stat', () => {
    expect(statLabel('defensive_contribution')).toBe('Defensive contributions');
    expect(statLabel('shots_on_target')).toBe('Shots on target');
    const line = (stat: string, value: number, points: number) =>
      ({ stat, value, points }) as Schemas['PointsLine'];
    const totals = seasonStatTotals([
      {
        gameweek: 2,
        points: 0,
        minutes: 0,
        fixtures: [
          { breakdown: [line('minutes', 90, 2), line('goals_scored', 1, 5)] },
        ] as Schemas['PlayerFixture'][],
        stats: data.NO_STATS,
      },
      {
        gameweek: 1,
        points: 0,
        minutes: 0,
        fixtures: [
          { breakdown: [line('goals_scored', 2, 10), line('minutes', 30, 1)] },
          // An unused sub: not an appearance.
          { breakdown: [line('minutes', 0, 0)] },
        ] as Schemas['PlayerFixture'][],
        stats: data.NO_STATS,
      },
    ]);
    expect(totals).toEqual([
      { stat: 'minutes', value: 120, points: 3, fixtures: 2 },
      { stat: 'goals_scored', value: 3, points: 15, fixtures: 2 },
    ]);
  });
});

describe('league gameweeks', () => {
  const home = (start: number, current: number, deadlineAt = '2026-10-16T17:30:00Z') => ({
    currentGameweek: {
      number: current,
      name: `Gameweek ${current}`,
      deadlineAt,
      waiverDeadlineAt: deadlineAt,
      tradeDeadlineAt: deadlineAt,
      finished: false,
    },
    settings: { ...data.LEAGUE_SETTINGS, startGameweek: start, endGameweek: 38 },
  });

  it("never opens before a mid-season league's first gameweek", () => {
    expect(leagueGameweek(home(8, 8), 7)).toBe(8);
    expect(leagueGameweek(home(8, 8))).toBe(8);
    expect(leagueGameweek(home(1, 7), 7)).toBe(7);
    expect(leagueGameweek(home(1, 7), 8)).toBe(8);
    expect(leagueGameweek(home(1, 38), 39)).toBe(38);
  });

  it("treats the league's current gameweek as upcoming until its deadline", () => {
    const at = Date.parse('2026-10-14T12:00:00Z');
    expect(matchupUpcoming(home(8, 8), 8, at)).toBe(true);
    expect(matchupUpcoming(home(8, 8), 9, at)).toBe(true);
    expect(matchupUpcoming(home(1, 7, '2026-10-09T17:30:00Z'), 7, at)).toBe(false);
    expect(matchupUpcoming(home(1, 7, '2026-10-09T17:30:00Z'), 6, at)).toBe(false);
  });
});

describe('hero helpers', () => {
  const fx = (status: string) =>
    ({
      fixtureId: `fx_${status}`,
      opponent: { code: 'SOU', name: 'Southmoor' },
      isHome: true,
      kickoffAt: '2026-10-17T14:00:00Z',
      status,
      minutes: 0,
      points: 0,
      bonusProvisional: false,
      breakdown: [],
    }) as unknown as Schemas['PlayerFixture'];

  it('splits live points into shares that add to 100', () => {
    expect(pointsShare(30, 10)).toEqual({ left: 75, right: 25 });
    expect(pointsShare(1, 2)).toEqual({ left: 33, right: 67 });
    expect(pointsShare(12, 0)).toEqual({ left: 100, right: 0 });
  });

  it('has no share at 0-0, without scores or for negatives only', () => {
    expect(pointsShare(0, 0)).toBeNull();
    expect(pointsShare(-2, 0)).toBeNull();
    expect(pointsShare(null, 4)).toBeNull();
    expect(pointsShare(-1, 3)).toEqual({ left: 0, right: 100 });
  });

  it('says who leads', () => {
    expect(leadOf(5, 3)).toBe('lead');
    expect(leadOf(3, 5)).toBe('trail');
    expect(leadOf(4, 4)).toBe('level');
    expect(leadOf(null, 4)).toBeNull();
  });

  it('dashes points only before any of the player’s games kick off', () => {
    expect(beforeKickoff(slot('MID', 1, { fixtures: [fx('scheduled')] }))).toBe(true);
    expect(beforeKickoff(slot('MID', 1, { fixtures: [fx('finished'), fx('scheduled')] }))).toBe(
      false,
    );
    expect(beforeKickoff(slot('MID', 1, { fixtures: [] }))).toBe(false);
    expect(beforeKickoff(slot('MID', 1, { fixtures: [fx('live')] }))).toBe(false);
  });

  it('counts starters yet to play, by position GK→FWD', () => {
    const xi = [
      slot('FWD', 10, { fixtures: [fx('scheduled')] }),
      slot('DEF', 2, { fixtures: [fx('scheduled')] }),
      slot('DEF', 3, { fixtures: [fx('finished'), fx('scheduled')] }),
      slot('GKP', 1, { fixtures: [fx('scheduled')] }),
      slot('MID', 6, { fixtures: [fx('live')] }),
      slot('MID', 7, { fixtures: [] }),
      slot('MID', 8, { fixtures: [fx('postponed')] }),
      slot('MID', 9, { fixtures: [fx('finished')] }),
    ];
    expect(yetToPlay(xi)).toEqual({ count: 4, positions: 'GK, 2 DEF, FWD' });
    expect(yetToPlay([])).toEqual({ count: 0, positions: '' });
  });

  it('writes a record with the current run and rank', () => {
    const rec = (form: Schemas['FormResult'][]) =>
      teamRecord({ wins: 3, draws: 2, losses: 1, rank: 5, form });
    expect(recordLine(rec(['W', 'L', 'L']))).toBe('3-2-1 ▼2L (#5)');
    expect(recordLine(rec(['L', 'W', 'W', 'W']))).toBe('3-2-1 ▲3W (#5)');
    expect(recordLine(rec(['W', 'D']))).toBe('3-2-1 =1D (#5)');
    // Before any result: no run.
    expect(recordLine(rec([]))).toBe('3-2-1 (#5)');
    // An unknown result (open enum) keeps its letter without an arrow.
    expect(streakLabel({ result: 'X', count: 2 })).toBe('2X');
    const row = data.standings()[0]!;
    expect(teamRecords([row]).get(row.team.id)).toEqual(teamRecord(row));
    expect(teamRecords(undefined).size).toBe(0);
  });
});
