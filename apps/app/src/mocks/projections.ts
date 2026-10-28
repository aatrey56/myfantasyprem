/**
 * Mock data for ADR 0014: fixture difficulty, a league gameweek's Expected
 * points and win odds, and start/sit accuracy, from the mock world's real
 * (simulated) points through the app's copy of the rules in
 * lib/projections. Invented players and clubs only.
 */
import type { Schemas } from '@myfantasyprem/api-client';

import type { InSeasonLeague } from './league';
import {
  CLUBS,
  CURRENT_GW,
  FORM_GW,
  fixturesFor,
  gameweekFixtures,
  getPlayerRecord,
  isOver,
  PLAYERS,
  pointsFor,
} from './world';
import { form } from '@/lib/form';
import {
  accuracy,
  expectedPoints,
  finalOdds,
  fixtureFactor,
  oddsSeed,
  optimalXI,
  rankDifficulty,
  recentGameweeks,
  SAMPLES,
  seasonAccuracy,
  winOdds,
  type ClubRanking,
  type DifficultyLine,
  type DifficultyMatch,
  type SimTeam,
} from '@/lib/projections';

type Position = Schemas['Position'];

/** In the mocks every gameweek before the live one is completed. */
const completed = (gw: number) => gw < CURRENT_GW;

function difficultyOver(window: readonly number[]) {
  const matches: DifficultyMatch[] = [];
  const lines: DifficultyLine[] = [];
  for (const gw of window) {
    for (const f of gameweekFixtures(gw)) {
      if (isOver(f.status))
        matches.push({ fixtureId: f.fixtureId, home: f.home.code, away: f.away.code });
    }
    for (const p of PLAYERS) {
      for (const f of fixturesFor(p.id, gw)) {
        lines.push({
          fixtureId: f.fixtureId,
          club: p.club.code,
          position: p.position,
          points: f.points,
        });
      }
    }
  }
  return rankDifficulty(matches, lines);
}

const clubByCode = new Map(CLUBS.map((c) => [c.code, c]));
const POSITIONS: readonly Position[] = ['GKP', 'DEF', 'MID', 'FWD'];

/** GET /v1/fixture-difficulty. */
export function fixtureDifficulty(): Schemas['FixtureDifficulty'] {
  const window = recentGameweeks(FORM_GW, completed);
  const ranked = difficultyOver(window);
  return {
    formGameweek: FORM_GW,
    recentGameweeks: window,
    positions: POSITIONS.map((position) => ({
      position,
      clubs: [...(ranked.get(position)?.values() ?? [])]
        .sort((a, b) => a.rank - b.rank || a.club.localeCompare(b.club))
        .map((r: ClubRanking) => ({
          club: clubByCode.get(r.club)!,
          rank: r.rank,
          pointsConceded: r.conceded,
          matches: r.matches,
        })),
    })),
  };
}

const clubFixtures = (code: string, gw: number) =>
  gameweekFixtures(gw).filter(
    (f) => f.status !== 'postponed' && (f.home.code === code || f.away.code === code),
  );

function formAt(playerId: string, formGw: number): number | null {
  const matches = Array.from({ length: Math.max(formGw - 1, 0) }, (_, i) => ({
    gameweek: i + 1,
    points: pointsFor(playerId, i + 1),
  }));
  return form(matches, formGw);
}

/** GET /v1/leagues/{id}/gameweeks/{gw}/projections. */
export function leagueProjections(
  league: InSeasonLeague,
  gw: number,
): Schemas['LeagueProjections'] {
  const formGw = Math.min(gw, FORM_GW);
  const window = recentGameweeks(formGw, completed);
  const ranked = difficultyOver(window);
  const week = league.schedule().find((w) => w.gameweek === gw);
  const myTeamId = league.viewer().team?.id;
  // Lineups for a gameweek ahead are private: others' come from the live one.
  const open = gw > CURRENT_GW;

  const players: Schemas['PlayerProjection'][] = [];
  const expected = new Map<string, number | null>();
  const rostered = new Set(league.TEAMS.flatMap((t) => league.lineupSlots(t.id, gw)));
  for (const id of [...rostered].sort()) {
    const p = getPlayerRecord(id)!;
    const fixtures = clubFixtures(p.club.code, gw).map((f) => {
      const isHome = f.home.code === p.club.code;
      const opponent = isHome ? f.away : f.home;
      const rank = ranked.get(p.position)?.get(opponent.code)?.rank ?? null;
      return {
        fixtureId: f.fixtureId,
        opponent,
        isHome,
        difficultyRank: rank,
        factor: fixtureFactor(rank),
      };
    });
    const f = formAt(id, formGw);
    const e =
      f === null
        ? null
        : expectedPoints(
            f,
            fixtures.map((x) => x.difficultyRank),
          );
    expected.set(id, e);
    players.push({ playerId: id, form: f, expected: e, fixtures });
  }

  const sims: SimTeam[] = [];
  const teams: Schemas['TeamProjection'][] = league.TEAMS.map((team) => {
    const mine = team.id === myTeamId;
    const slots =
      open && !mine ? league.lineupSlots(team.id, CURRENT_GW) : league.lineupSlots(team.id, gw);
    const starters = slots.slice(0, 11);
    let tenths = 0;
    let any = false;
    for (const id of starters) {
      const e = expected.get(id);
      if (e !== null && e !== undefined) {
        tenths += Math.round(e * 10);
        any = true;
      }
    }
    sims.push({
      id: team.id,
      starters: starters.map((id) => {
        const club = getPlayerRecord(id)!.club.code;
        return {
          actual: pointsFor(id, gw),
          remaining: clubFixtures(club, gw).filter((f) => f.status === 'scheduled').length,
          history: window.map((h) => pointsFor(id, h)),
        };
      }),
    });
    return {
      team,
      expected: any ? tenths / 10 : null,
      lineupBasis: open && !mine ? 'last_locked' : 'lineup',
      starters,
    };
  });

  const mode: Schemas['OddsMode'] =
    gw < CURRENT_GW ? 'final' : gw === CURRENT_GW ? 'live' : 'pre_game';
  const bucket = mode === 'live' ? Math.floor(Date.now() / 300_000) : 0;
  const finals = mode === 'final' ? league.matchupSummaries(gw) : [];
  const teamId = (o: Schemas['Opponent']) => (o.kind === 'team' ? o.team.id : null);
  const matchups: Schemas['MatchupOdds'][] = (week?.matchups ?? []).map((m) => {
    if (mode === 'final') {
      const s = finals.find((x) => x.id === m.id)!;
      return {
        matchupId: m.id,
        homeTeamId: teamId(m.home),
        awayTeamId: teamId(m.away),
        mode,
        odds: finalOdds(league.sidePoints(s.home), league.sidePoints(s.away)),
        samples: 0,
      };
    }
    const sides = { homeTeamId: teamId(m.home), awayTeamId: teamId(m.away) };
    if (window.length === 0) return { matchupId: m.id, ...sides, mode, odds: null, samples: 0 };
    return {
      matchupId: m.id,
      ...sides,
      mode,
      odds: winOdds(sims, teamId(m.home), teamId(m.away), oddsSeed(m.id, gw, bucket), SAMPLES),
      samples: SAMPLES,
    };
  });

  return { gameweek: gw, formGameweek: formGw, recentGameweeks: window, players, teams, matchups };
}

/** GET /v1/leagues/{id}/start-sit. */
export function startSit(league: InSeasonLeague): Schemas['TeamStartSit'][] {
  const weeks = league
    .schedule()
    .map((w) => w.gameweek)
    .filter(completed);
  const rows = league.TEAMS.map((team) => {
    const gameweeks: Schemas['StartSitWeek'][] = [];
    for (const gw of weeks) {
      const summary = league
        .matchupSummaries(gw)
        .flatMap((m) => [m.home, m.away])
        .find((s) => s.kind === 'team' && s.team.id === team.id);
      if (!summary) continue;
      const squad = league.lineupSlots(team.id, gw).map((id) => ({
        id,
        position: getPlayerRecord(id)!.position,
        points: pointsFor(id, gw),
      }));
      const best = optimalXI(squad);
      if (!best) continue;
      const points = league.sidePoints(summary);
      gameweeks.push({
        gameweek: gw,
        points,
        optimalPoints: best.points,
        accuracy: accuracy(points, best.points),
      });
    }
    const season = seasonAccuracy(
      gameweeks.map((w) => w.accuracy).filter((a): a is number => a !== null),
    );
    return { team, seasonAccuracy: season, gameweeks };
  });
  return rows.sort((a, b) => {
    if (a.seasonAccuracy === null) return b.seasonAccuracy === null ? 0 : 1;
    if (b.seasonAccuracy === null) return -1;
    return b.seasonAccuracy - a.seasonAccuracy;
  });
}
