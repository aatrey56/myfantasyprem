import type { Schemas } from '@myfantasyprem/api-client';

import * as data from './data';
import { previousSeasonsFor } from './seasons';

type DetailSide = Schemas['MatchupDetailSide'];
const lineupOf = (side: DetailSide) => (side.kind === 'team' ? side : undefined);

const ID = (prefix: string) => new RegExp(`^${prefix}_[0-9a-hjkmnp-tv-z]{26}$`);

describe('mock data invariants', () => {
  it('builds a 9-team league with 15-man squads', () => {
    expect(data.TEAMS).toHaveLength(9);
    for (const team of data.TEAMS) {
      const detail = data.teamDetail(team.id);
      expect(detail?.roster).toHaveLength(15);
      const counts = { GKP: 0, DEF: 0, MID: 0, FWD: 0 };
      detail?.roster.forEach((r) => counts[r.player.position]++);
      expect(counts).toEqual({ GKP: 2, DEF: 5, MID: 5, FWD: 3 });
    }
  });

  it('uses opaque prefixed ULID ids that match the contract patterns', () => {
    expect(data.LEAGUE_ID).toMatch(ID('lg'));
    data.TEAMS.forEach((t) => expect(t.id).toMatch(ID('tm')));
    data.PLAYERS.forEach((p) => expect(p.id).toMatch(ID('plr')));
    data.TRANSACTIONS.forEach((t) => expect(t.id).toMatch(ID('txn')));
    data.matchupSummaries(data.CURRENT_GW).forEach((m) => expect(m.id).toMatch(ID('mu')));
    const ids = data.PLAYERS.map((p) => p.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it('puts 11 starters in slots 1-11 and 4 on the bench, with a keeper in slot 12', () => {
    for (const summary of data.matchupSummaries(data.CURRENT_GW)) {
      const detail = data.matchupDetail(data.CURRENT_GW, summary.id)!;
      for (const side of [detail.home, detail.away]) {
        const lineup = lineupOf(side);
        if (!lineup) continue;
        expect(lineup.starters.map((s) => s.slot)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11]);
        expect(lineup.bench.map((s) => s.slot)).toEqual([12, 13, 14, 15]);
        expect(lineup.starters[0]?.player.position).toBe('GKP');
        expect(lineup.bench[0]?.player.position).toBe('GKP');
      }
    }
  });

  it('gives exactly one team the league average each gameweek, scored floor(mean of all teams)', () => {
    for (let gw = 1; gw <= data.CURRENT_GW; gw++) {
      const summaries = data.matchupSummaries(gw);
      expect(summaries).toHaveLength(5);
      const withAverage = summaries.filter(
        (m) => m.home.kind === 'average' || m.away.kind === 'average',
      );
      expect(withAverage).toHaveLength(1);
      const teamTotals = summaries.flatMap((m) =>
        [m.home, m.away].flatMap((s) => (s.kind === 'team' ? [s.points] : [])),
      );
      expect(teamTotals).toHaveLength(9);
      const expected = Math.floor(teamTotals.reduce((a, b) => a + b, 0) / 9);
      const avg = [withAverage[0]!.home, withAverage[0]!.away].find((s) => s.kind === 'average');
      expect(avg).toEqual({ kind: 'average', points: expected, basis: 'floor_mean_all_teams' });
    }
    const detail = data.matchupDetail(data.CURRENT_GW, data.averageMatchupId(data.CURRENT_GW))!;
    expect([detail.home.kind, detail.away.kind].sort()).toEqual(['average', 'team']);
  });

  it('keeps every fixture breakdown consistent with its points', () => {
    for (const p of data.PLAYERS) {
      for (let gw = 1; gw <= data.CURRENT_GW; gw++) {
        for (const f of data.fixturesFor(p.id, gw)) {
          expect(f.breakdown.reduce((s, l) => s + l.points, 0)).toBe(f.points);
          if (f.status === 'scheduled') expect(f.minutes).toBe(0);
        }
      }
    }
  });

  it('adds up team totals from starters and applied auto-subs', () => {
    for (let gw = 1; gw <= data.CURRENT_GW; gw++) {
      for (const summary of data.matchupSummaries(gw)) {
        const detail = data.matchupDetail(gw, summary.id)!;
        for (const [side, summarySide] of [
          [detail.home, summary.home],
          [detail.away, summary.away],
        ] as const) {
          const lineup = lineupOf(side);
          if (!lineup) continue;
          const counted = [...lineup.starters, ...lineup.bench]
            .filter((s) => (s.slot <= 11 ? !s.autoSub.endsWith('_out') : s.autoSub.endsWith('_in')))
            .reduce((sum, s) => sum + s.points, 0);
          expect(lineup.totalPoints).toBe(counted);
          expect(summarySide.kind === 'team' && summarySide.points).toBe(counted);
        }
      }
    }
  });

  it('only subs a bench player who played, in a legal formation', () => {
    for (let gw = 1; gw <= data.CURRENT_GW; gw++) {
      for (const summary of data.matchupSummaries(gw)) {
        const detail = data.matchupDetail(gw, summary.id)!;
        for (const side of [detail.home, detail.away]) {
          const lineup = lineupOf(side);
          if (!lineup) continue;
          const slots = [...lineup.starters, ...lineup.bench];
          for (const sub of lineup.autoSubs) {
            const out = slots.find((s) => s.slot === sub.slotOut)!;
            const into = slots.find((s) => s.slot === sub.slotIn)!;
            expect(out.minutes).toBe(0);
            expect(into.minutes).toBeGreaterThan(0);
            expect(sub.status).toBe(gw < data.CURRENT_GW ? 'confirmed' : 'projected');
            if (out.player.position === 'GKP') expect(into.player.position).toBe('GKP');
          }
          expect(lineup.starters.filter((s) => s.player.position === 'GKP')).toHaveLength(1);
        }
      }
    }
  });

  it('has a live gameweek with scheduled, live and provisionally finished fixtures', () => {
    const statuses = new Set<string>();
    for (const p of data.PLAYERS)
      data.fixturesFor(p.id, data.CURRENT_GW).forEach((f) => statuses.add(f.status));
    expect([...statuses].sort()).toEqual(['finished_provisional', 'live', 'scheduled']);
  });

  it('has a double and a blank in the next gameweek on rostered players', () => {
    const roster = data.TEAMS.flatMap((t) => data.teamDetail(t.id)!.roster);
    const next = (r: (typeof roster)[number]) =>
      r.nextFixtures.filter((f) => f.gameweek === data.CURRENT_GW + 1);
    const dgw = roster.filter((r) => next(r).length === 2);
    const blank = roster.filter((r) => next(r).length === 0);
    expect(dgw.length).toBeGreaterThan(0);
    expect(blank.length).toBeGreaterThan(0);
    dgw.forEach((r) => expect(data.DGW_CLUBS).toContain(r.player.club.code));
    blank.forEach((r) => expect(data.BLANK_CLUBS).toContain(r.player.club.code));
  });

  it('lists every remaining fixture on roster rows, not just three', () => {
    const roster = data.TEAMS.flatMap((t) => data.teamDetail(t.id)!.roster);
    roster.forEach((r) => expect(r.nextFixtures.length).toBeGreaterThan(3));
  });

  it('computes standings from completed gameweeks, without the average opponent', () => {
    const table = data.standings();
    expect(table).toHaveLength(9);
    expect(table.map((r) => r.rank)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9]);
    for (const row of table) {
      expect(row.played).toBe(data.CURRENT_GW - 1);
      expect(row.leaguePoints).toBe(row.wins * 3 + row.draws);
      expect(row.wins + row.draws + row.losses).toBe(row.played);
    }
    for (let i = 1; i < table.length; i++) {
      expect(table[i - 1]!.leaguePoints).toBeGreaterThanOrEqual(table[i]!.leaguePoints);
    }
    const results = data.TEAMS.flatMap((t) => data.teamDetail(t.id)!.results);
    expect(results.some((r) => r.opponent.kind === 'average')).toBe(true);
  });

  it('covers every transaction kind, claim outcome and the trade states FPL exposes', () => {
    const kinds = new Set(data.TRANSACTIONS.map((t) => t.kind));
    expect([...kinds].sort()).toEqual(['free_agent', 'trade', 'waiver']);
    const trades = data.TRANSACTIONS.filter((t) => t.kind === 'trade');
    expect(new Set(trades.map((t) => t.status))).toEqual(new Set(['processed', 'vetoed']));
    const reasons = new Set(
      data.TRANSACTIONS.flatMap((t) =>
        (t.kind === 'waiver' || t.kind === 'free_agent') && t.result.outcome === 'denied'
          ? [t.result.reason]
          : [],
      ),
    );
    expect(reasons).toEqual(new Set(['player_in_unavailable', 'player_out_unavailable']));
  });
  it('gives every player per-game stats that agree with the points breakdown', () => {
    let played = 0;
    for (const p of data.PLAYERS) {
      for (let gw = 1; gw <= data.CURRENT_GW; gw++) {
        const fixtures = data.fixturesFor(p.id, gw);
        const s = data.statsFor(p.id, gw);
        if (fixtures.length === 0) {
          expect(s).toEqual(data.NO_STATS);
          continue;
        }
        const sum = (stat: string) =>
          fixtures
            .flatMap((f) => f.breakdown)
            .reduce((t, l) => t + (l.stat === stat ? l.value : 0), 0);
        if (data.minutesFor(p.id, gw) === 0) continue;
        played++;
        expect(s.goalsScored).toBe(sum('goals_scored'));
        expect(s.assists).toBe(sum('assists'));
        expect(s.yellowCards).toBe(sum('yellow_cards'));
        expect(s.bonus).toBe(sum('bonus'));
        expect(s.expectedGoalInvolvements).toBeCloseTo(s.expectedGoals! + s.expectedAssists!, 2);
        expect(s.tackles!).toBeLessThanOrEqual(s.cbit!);
        if (p.position === 'DEF' && s.defensiveContributionPoints)
          expect(s.cbit!).toBeGreaterThanOrEqual(10);
        if (p.position !== 'DEF' && p.position !== 'GKP' && s.defensiveContributionPoints) {
          expect(s.cbit! + s.recoveries!).toBeGreaterThanOrEqual(12);
        }
      }
    }
    expect(played).toBeGreaterThan(500);
    for (const pos of ['GKP', 'DEF', 'MID', 'FWD'] as const) {
      const ranks = data.PLAYERS.filter((p) => p.position === pos).map((p) =>
        data.positionRankOf(p.id),
      );
      expect(Math.min(...(ranks as number[]))).toBe(1);
    }
  });
});

describe('transferCommissioner mock', () => {
  const pre = () => data.leagues.get(data.PREDRAFT_LEAGUE_ID)!;
  const other = (l: data.MockLeague) =>
    l.members().find((m) => m.user.id !== data.DEMO_USER_ID)!.user.id;

  afterEach(() => data.resetMockLeagues());

  it.each([
    ['pre-draft', () => data.leagues.get(data.PREDRAFT_LEAGUE_ID)!],
    ['in-season', () => data.leagues.get(data.OFFICE_LEAGUE_ID)!],
  ])('moves the role in a %s league', (_n, get) => {
    const l = get();
    expect(l.viewer().role).toBe('commissioner');
    const target = other(l);
    l.transferCommissioner(target);
    expect(l.viewer().role).toBe('member');
    const roles = l.members().filter((m) => m.role === 'commissioner');
    expect(roles.map((m) => m.user.id)).toEqual([target]);
    expect(() => l.transferCommissioner(target)).toThrow(/commissioner/);
  });

  it('is a no-op for yourself and 404s for strangers', () => {
    const l = pre();
    l.transferCommissioner(data.DEMO_USER_ID);
    expect(l.viewer().role).toBe('commissioner');
    expect(() => l.transferCommissioner('usr_01k7m2xq8rfv3c9a4t6wbn5hzz')).toThrow(/not found/i);
  });
});

describe('gameweekFixtures mock', () => {
  const statuses = (gw: number) =>
    data.gameweekFixtures(gw).reduce<Record<string, number>>((acc, f) => {
      acc[f.status] = (acc[f.status] ?? 0) + 1;
      return acc;
    }, {});

  it('mixes finished, live and upcoming matches in the live gameweek', () => {
    const fixtures = data.gameweekFixtures(data.CURRENT_GW);
    expect(statuses(data.CURRENT_GW)).toEqual({ finished_provisional: 4, live: 3, scheduled: 3 });
    for (const f of fixtures) {
      expect(f.fixtureId).toMatch(ID('fx'));
      expect(f.gameweek).toBe(data.CURRENT_GW);
      const scored = f.status !== 'scheduled';
      expect(f.homeScore === null).toBe(!scored);
      expect(f.awayScore === null).toBe(!scored);
    }
    const keys = fixtures.map((f) => `${f.kickoffAt}|${f.fixtureId}`);
    expect(keys).toEqual([...keys].sort());
  });

  it('agrees with the players’ fixtures', () => {
    const byId = new Map(data.gameweekFixtures(data.CURRENT_GW).map((f) => [f.fixtureId, f]));
    for (const p of data.PLAYERS.slice(0, 40)) {
      for (const pf of data.fixturesFor(p.id, data.CURRENT_GW)) {
        const f = byId.get(pf.fixtureId)!;
        expect(f).toBeDefined();
        expect([f.status, f.homeScore, f.awayScore, f.kickoffAt]).toEqual([
          pf.status,
          pf.homeScore,
          pf.awayScore,
          pf.kickoffAt,
        ]);
        expect(pf.isHome ? f.home : f.away).toEqual(p.club);
      }
    }
  });

  it('is finished before and scheduled after the live gameweek', () => {
    expect(statuses(data.CURRENT_GW - 1)).toEqual({ finished: 10 });
    expect(statuses(data.DGW_GW)).toEqual({ scheduled: 10 });
    expect(data.gameweekFixtures(data.DGW_GW).every((f) => f.homeScore === null)).toBe(true);
  });

  it('gives live matches a clock: one in each half and one at half time', () => {
    const live = data.gameweekFixtures(data.CURRENT_GW).filter((f) => f.status === 'live');
    expect(live.map((f) => [f.minute, f.period]).sort()).toEqual([
      [38, 'first_half'],
      [45, 'half_time'],
      [71, 'second_half'],
    ]);
    const others = data.gameweekFixtures(data.CURRENT_GW).filter((f) => f.status !== 'live');
    expect(others.every((f) => f.minute === null && f.period === null)).toBe(true);
  });

  it('postpones one fixture (no date, listed last) and blanks a whole gameweek', () => {
    const fixtures = data.gameweekFixtures(data.POSTPONED_GW);
    expect(statuses(data.POSTPONED_GW)).toEqual({ scheduled: 9, postponed: 1 });
    expect(fixtures.at(-1)).toMatchObject({ status: 'postponed', kickoffAt: null });
    // Its clubs have no game that gameweek.
    const pp = fixtures.at(-1)!;
    const player = data.PLAYERS.find((p) => p.club.code === pp.home.code)!;
    expect(data.fixturesInGameweek(player.id, data.POSTPONED_GW)).toEqual([]);
    expect(data.gameweekFixtures(data.BLANK_GW)).toEqual([]);
    expect(data.findFixture(pp.fixtureId)).toEqual(pp);
  });

  it('builds box scores that agree with the players’ fixtures', () => {
    const f = data.gameweekFixtures(data.CURRENT_GW - 1)[0]!;
    const { home, away } = data.fixtureLines(f);
    let goals = 0;
    for (const line of home) {
      expect(line.player.club).toEqual(f.home);
      if (!line.entry) continue;
      expect(line.entry.fixtureId).toBe(f.fixtureId);
      const scored = line.entry.breakdown.find((l) => l.stat === 'goals_scored')?.value ?? 0;
      expect(line.stats.goalsScored).toBe(scored);
      goals += scored;
    }
    expect(goals).toBeLessThanOrEqual(f.homeScore!);
    expect(away.every((l) => l.player.club.code === f.away.code)).toBe(true);
    expect(home.some((l) => (l.entry?.minutes ?? 0) > 0)).toBe(true);
  });
});

describe('previousSeasonsFor', () => {
  it('never invents a negative count, and nulls what FPL did not track', () => {
    for (let i = 0; i < 200; i++) {
      for (const s of previousSeasonsFor(`plr_${i}`)) {
        for (const v of Object.values(s))
          if (typeof v === 'number') expect(v).toBeGreaterThanOrEqual(0);
        const y = Number(s.season.slice(0, 4));
        expect(s.ictIndex === null).toBe(y < 2016);
        expect(s.expectedGoals === null).toBe(y < 2022);
        expect(s.defensiveContribution === null).toBe(y < 2024);
      }
    }
  });
});
