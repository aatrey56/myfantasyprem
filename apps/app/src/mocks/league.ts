/**
 * The per-league tier of the mock data: one in-season league built from the
 * shared world (players, fixtures, points). Teams, ownership (a snake draft
 * by quality), lineups with FPL auto-subs, the H2H schedule (with the
 * league-average opponent when the team count is odd), standings, the
 * activity feed, waivers and trades.
 *
 * Gameweeks up to the live one are computed once, at build time, so later
 * roster moves never rewrite history. Lineups from the next gameweek on, the
 * waiver claims and the trades are mutable: the MSW handlers call the
 * action methods, which throw a MockError for a broken rule.
 */
import type { Schemas } from '@myfantasyprem/api-client';
import { previousSeasonsFor } from './seasons';

import { conflict, forbidden, invalid, MockError, notFound } from './errors';
import { chanceOfPlaying } from './status';
import {
  appearancesOf,
  clubMatchesOf,
  CURRENT_GW,
  HOUR,
  NOW,
  PLAYERS,
  SEASON,
  draftRankOf,
  fixturesFor,
  fixturesInGameweek,
  formChangeOf,
  formOf,
  gameweek,
  getPlayerRecord,
  isOver,
  minutesFor,
  mockId,
  mulberry32,
  pick,
  pointsFor,
  positionRankOf,
  startsOf,
  seasonStatsOf,
  seasonTotals,
  statsFor,
  toPlayerRef,
  rowFixturesFor,
  upcomingFor,
  type PlayerRecord,
} from './world';

type Position = Schemas['Position'];
type TeamRef = Schemas['TeamRef'];
type LineupSlot = Schemas['LineupSlot'];
type TeamLineup = Schemas['TeamLineup'];
type AverageOpponent = Schemas['AverageOpponent'];
type MatchupSide = Schemas['MatchupSide'];
type MatchupDetailSide = Schemas['MatchupDetailSide'];
type MatchupSummary = Schemas['MatchupSummary'];
type MatchupDetail = Schemas['MatchupDetail'];
type Opponent = Schemas['Opponent'];
type StandingRow = Schemas['StandingRow'];
type FormResult = Schemas['FormResult'];
type Transaction = Schemas['Transaction'];
type ScoreState = Schemas['ScoreState'];
type Trade = Schemas['Trade'];
type WaiverClaim = Schemas['WaiverClaim'];
type TradeReview = Schemas['TradeReview'];

/** The signed-in user in mock builds. */
export const DEMO_USER_ID = mockId('usr', 1);
export const DEMO_USER_NAME = 'Sam P.';

export const SQUAD: Record<Position, number> = { GKP: 2, DEF: 5, MID: 5, FWD: 3 };
export const MIN_STARTERS: Record<Position, number> = { GKP: 1, DEF: 3, MID: 2, FWD: 1 };
export const MAX_STARTERS: Record<Position, number> = { GKP: 1, DEF: 5, MID: 5, FWD: 3 };
const POSITION_WORD: Record<Position, string> = {
  GKP: 'goalkeepers',
  DEF: 'defenders',
  MID: 'midfielders',
  FWD: 'forwards',
};

export function leagueSettings(opts: {
  teamCount: number;
  review: TradeReview;
  pickTimerSeconds?: number;
  draftStartsAt?: string | null;
  startGameweek?: number;
  waiverOrder?: Schemas['WaiverOrderRule'];
}): Schemas['LeagueSettings'] {
  return {
    teamCount: opts.teamCount,
    scoring: 'fpl_standard',
    schedule: 'h2h',
    roster: { ...SQUAD },
    lineup: {
      starters: 11,
      minStarters: { ...MIN_STARTERS },
      maxStarters: { ...MAX_STARTERS },
      benchGoalkeeperFirst: true,
    },
    draft: {
      type: 'snake',
      pickTimerSeconds: opts.pickTimerSeconds ?? 60,
      startsAt: opts.draftStartsAt ?? null,
    },
    waivers: { type: 'rolling_priority', order: opts.waiverOrder ?? 'reverse_standings' },
    trades: {
      review: opts.review,
      reviewHours: opts.review === 'none' ? 0 : 24,
      vetoThreshold: 0.5,
      deadlineGameweek: 30,
    },
    startGameweek: opts.startGameweek ?? 1,
    endGameweek: 38,
  };
}

/** Starter and bench rules (RESEARCH.md section 7). Returns every violation. */
export function lineupViolations(
  roster: ReadonlyMap<string, Position>,
  starters: readonly string[],
  bench: readonly string[],
): Schemas['ProblemError'][] {
  const out: Schemas['ProblemError'][] = [];
  if (starters.length !== 11)
    out.push({ code: 'wrong_starter_count', message: 'Pick exactly 11 starters.' });
  if (bench.length !== 4)
    out.push({ code: 'wrong_bench_count', message: 'Put exactly 4 players on the bench.' });
  const seen = new Set<string>();
  for (const id of [...starters, ...bench]) {
    if (seen.has(id)) {
      out.push({ code: 'duplicate_player', message: 'A player appears twice.', playerId: id });
      continue;
    }
    seen.add(id);
    if (!roster.has(id)) {
      out.push({
        code: 'player_not_on_roster',
        message: 'That player is not on this roster.',
        playerId: id,
      });
    }
  }
  for (const id of [...roster.keys()].sort()) {
    if (!seen.has(id)) {
      out.push({
        code: 'player_missing',
        message: 'Every roster player must be in the XI or on the bench.',
        playerId: id,
      });
    }
  }
  const counts: Record<Position, number> = { GKP: 0, DEF: 0, MID: 0, FWD: 0 };
  starters.forEach((id) => {
    const pos = roster.get(id);
    if (pos) counts[pos]++;
  });
  for (const pos of ['GKP', 'DEF', 'MID', 'FWD'] as const) {
    if (counts[pos] < MIN_STARTERS[pos]) {
      out.push({
        code: `too_few_${POSITION_WORD[pos]}`,
        message: `Start at least ${MIN_STARTERS[pos]} ${POSITION_WORD[pos]}.`,
      });
    }
    if (counts[pos] > MAX_STARTERS[pos]) {
      out.push({
        code: `too_many_${POSITION_WORD[pos]}`,
        message: `Start at most ${MAX_STARTERS[pos]} ${POSITION_WORD[pos]}.`,
      });
    }
  }
  const firstBench = bench[0];
  if (firstBench && roster.has(firstBench) && roster.get(firstBench) !== 'GKP') {
    out.push({
      code: 'bench_goalkeeper_not_first',
      message: 'The first bench slot must be your backup goalkeeper.',
      playerId: firstBench,
    });
  }
  return out;
}

export interface InSeasonConfig {
  id: string;
  name: string;
  /** Added to every numeric id seed, so leagues never share ids. */
  idOffset: number;
  teams: readonly (readonly [string, string])[];
  /** Index of the demo user's team. */
  myTeamIndex: number;
  /** Index of the commissioner's team. */
  commissionerIndex: number;
  review: TradeReview;
  /** 0 reproduces a pure quality-order draft; more shuffles it. */
  draftJitter: number;
  seed: number;
  createdAt: string;
  /**
   * The league's first scoring gameweek (default 1). Later than the
   * current one for a league drafted mid-season, after this gameweek's
   * deadline: no scores, results or transactions yet (ADR 0010).
   */
  startGameweek?: number;
}

const FORMATIONS = ['4-4-2', '3-5-2', '4-3-3', '5-3-2', '3-4-3', '4-5-1', '5-4-1'] as const;
const parseFormation = (f: string) => f.split('-').map(Number) as [number, number, number];

/** Placeholder for the league-average opponent in the schedule. */
const AV = 'AV';

export type InSeasonLeague = ReturnType<typeof createInSeasonLeague>;

/** How long a simulated manager takes to accept a trade Sam offers. */
export const OTHER_MANAGER_RESPONSE_MS = 4_000;

/**
 * The mock transaction window. The `transactions-locked` scenario sets
 * `locked` (gameweek deadline passed, gameweek not finished): unowned
 * players show as locked until the next waiver run.
 */
export const mockPhase = { locked: false };

export function createInSeasonLeague(cfg: InSeasonConfig) {
  const off = cfg.idOffset;
  const START = cfg.startGameweek ?? 1;
  /** The league's current gameweek: the season's, never before its first. */
  const LEAGUE_GW = Math.max(CURRENT_GW, START);
  const TEAMS: TeamRef[] = cfg.teams.map(([name, managerName], i) => ({
    id: mockId('tm', off + i + 1),
    name,
    managerName: i === cfg.myTeamIndex ? DEMO_USER_NAME : managerName,
  }));
  const teamById = new Map(TEAMS.map((t) => [t.id, t]));
  const userOfTeam = new Map(
    TEAMS.map((t, i) => [
      t.id,
      i === cfg.myTeamIndex ? DEMO_USER_ID : mockId('usr', off + 100 + i),
    ]),
  );
  const myTeam = TEAMS[cfg.myTeamIndex] as TeamRef;
  const commissionerTeam = TEAMS[cfg.commissionerIndex] as TeamRef;
  let commissionerUserId = userOfTeam.get(commissionerTeam.id) as string;
  const isCommissioner = () => commissionerUserId === DEMO_USER_ID;

  // ------------------------------------------------------------ ownership

  /** Snake draft by quality: best players go first, as in a real draft. */
  function buildOwnership() {
    const r = mulberry32(cfg.seed);
    const owner = new Map<string, string>();
    const roster = new Map<string, PlayerRecord[]>(TEAMS.map((t) => [t.id, []]));
    for (const position of ['GKP', 'DEF', 'MID', 'FWD'] as const) {
      const ranked = PLAYERS.filter((p) => p.position === position)
        .map((p) => ({ p, key: p.quality + (cfg.draftJitter ? (r() - 0.5) * cfg.draftJitter : 0) }))
        .sort((a, b) => b.key - a.key)
        .map((x) => x.p);
      for (let round = 0; round < SQUAD[position]; round++) {
        const order = round % 2 === 0 ? TEAMS : [...TEAMS].reverse();
        order.forEach((team, i) => {
          const player = ranked[round * TEAMS.length + i];
          if (!player) return;
          owner.set(player.id, team.id);
          roster.get(team.id)?.push(player);
        });
      }
    }
    return { owner, roster };
  }

  const ownership = buildOwnership();

  /**
   * The board of the draft that built the rosters: a snake over the initial
   * squads (trades and waivers since do not rewrite it), each team's
   * players in draft-rank order, one gameweek-deadline hour apart.
   */
  function draftPicks(startedAt: number): Schemas['DraftPick'][] {
    const n = TEAMS.length;
    const squads = TEAMS.map((t) =>
      [...(ownership.roster.get(t.id) ?? [])].sort((a, b) => draftRankOf(a.id) - draftRankOf(b.id)),
    );
    const picks: Schemas['DraftPick'][] = [];
    const rounds = squads[0]?.length ?? 0;
    for (let round = 1; round <= rounds; round++) {
      for (let i = 0; i < n; i++) {
        const t = round % 2 === 1 ? i : n - 1 - i;
        const player = squads[t]?.[round - 1];
        if (!player) continue;
        const overall = picks.length + 1;
        picks.push({
          id: mockId('pk', off + 5_000 + overall),
          overall,
          round,
          pickInRound: i + 1,
          team: TEAMS[t] as TeamRef,
          player: toPlayerRef(player),
          method: 'manual',
          pickedAt: new Date(startedAt + overall * 60_000).toISOString(),
          missedClock: false,
        });
      }
    }
    return picks;
  }

  /** Current owner; changes with waivers, free-agent moves and trades. */
  const owner = new Map(ownership.owner);
  const acquiredVia = new Map<string, Schemas['RosterEntry']['acquiredVia']>();
  const ownerOf = (playerId: string): TeamRef | null => {
    const id = owner.get(playerId);
    return id ? (teamById.get(id) ?? null) : null;
  };

  // -------------------------------------------------------------- lineups

  interface Lineup {
    formation: string;
    /** Slots 1..15 in order. */
    slots: PlayerRecord[];
  }

  function buildLineup(teamId: string, teamIndex: number): Lineup {
    const roster = (ownership.roster.get(teamId) ?? [])
      .slice()
      .sort((a, b) => b.quality - a.quality);
    const formation = FORMATIONS[teamIndex % FORMATIONS.length] as string;
    const [d, m, f] = parseFormation(formation);
    const by = (pos: Position) => roster.filter((p) => p.position === pos);
    const gk = by('GKP');
    const def = by('DEF');
    const mid = by('MID');
    const fwd = by('FWD');
    const starters = [
      gk[0],
      ...def.slice(0, d),
      ...mid.slice(0, m),
      ...fwd.slice(0, f),
    ] as PlayerRecord[];
    const benchOutfield = [...def.slice(d), ...mid.slice(m), ...fwd.slice(f)].sort(
      (a, b) => b.quality - a.quality,
    );
    return { formation, slots: [...starters, gk[1] as PlayerRecord, ...benchOutfield] };
  }

  const BASE_LINEUPS = new Map(TEAMS.map((t, i) => [t.id, buildLineup(t.id, i)]));
  /** Lineups saved for future gameweeks: teamId -> [fromGw, slots], carried over. */
  const savedLineups = new Map<string, { fromGw: number; slots: PlayerRecord[] }[]>();

  const formationOf = (slots: readonly PlayerRecord[]) => {
    const c: Record<Position, number> = { GKP: 0, DEF: 0, MID: 0, FWD: 0 };
    slots.slice(0, 11).forEach((p) => c[p.position]++);
    return `${c.DEF}-${c.MID}-${c.FWD}`;
  };

  /** The lineup a team uses in `gw`; future gameweeks carry the latest saved one. */
  function lineupFor(teamId: string, gw: number): Lineup {
    const base = BASE_LINEUPS.get(teamId) as Lineup;
    if (gw <= CURRENT_GW) return base;
    const saved = (savedLineups.get(teamId) ?? []).filter((s) => s.fromGw <= gw).at(-1);
    const slots = saved ? saved.slots : currentBase.get(teamId)!;
    return { formation: formationOf(slots), slots };
  }
  /** The base lineup with roster moves applied (what carries over by default). */
  const currentBase = new Map(TEAMS.map((t) => [t.id, [...BASE_LINEUPS.get(t.id)!.slots]]));

  function fixturesDone(fixtures: Schemas['PlayerFixture'][]) {
    return fixtures.length === 0 || fixtures.every((f) => isOver(f.status));
  }

  /** FPL auto-sub rules: first bench player who played, bench order, legal formation. */
  function autoSubs(lineup: Lineup, gw: number) {
    const subs: Schemas['AutoSub'][] = [];
    const slots = lineup.slots.map((p, i) => ({ p, slot: i + 1 }));
    const bench = slots.slice(11);
    const used = new Set<number>();
    const counts = { GKP: 0, DEF: 0, MID: 0, FWD: 0 } as Record<Position, number>;
    slots.slice(0, 11).forEach((s) => counts[s.p.position]++);
    const confirmed = gw < CURRENT_GW;
    for (const starter of slots.slice(0, 11)) {
      const fx = fixturesFor(starter.p.id, gw);
      if (minutesFor(starter.p.id, gw) > 0 || !fixturesDone(fx)) continue;
      const candidate = bench.find((b) => {
        if (used.has(b.slot) || minutesFor(b.p.id, gw) === 0) return false;
        if (starter.p.position === 'GKP') return b.p.position === 'GKP';
        if (b.p.position === 'GKP') return false;
        const next = { ...counts };
        next[starter.p.position]--;
        next[b.p.position]++;
        return next.DEF >= 3 && next.MID >= 2 && next.FWD >= 1;
      });
      if (!candidate) continue;
      used.add(candidate.slot);
      counts[starter.p.position]--;
      counts[candidate.p.position]++;
      subs.push({
        status: confirmed ? 'confirmed' : 'projected',
        slotOut: starter.slot,
        slotIn: candidate.slot,
        playerOutId: starter.p.id,
        playerInId: candidate.p.id,
      });
    }
    return subs;
  }

  function toSlot(
    p: PlayerRecord,
    slot: number,
    gw: number,
    subs: Schemas['AutoSub'][],
  ): LineupSlot {
    const out = subs.find((s) => s.slotOut === slot);
    const into = subs.find((s) => s.slotIn === slot);
    const autoSub: Schemas['AutoSubState'] = out
      ? out.status === 'confirmed'
        ? 'confirmed_out'
        : 'projected_out'
      : into
        ? into.status === 'confirmed'
          ? 'confirmed_in'
          : 'projected_in'
        : 'none';
    return {
      slot,
      player: toPlayerRef(p),
      points: pointsFor(p.id, gw),
      minutes: minutesFor(p.id, gw),
      autoSub,
      fixtures: fixturesFor(p.id, gw),
    };
  }

  const lineupCache = new Map<string, TeamLineup>();

  function teamLineup(teamId: string, gw: number): TeamLineup {
    const key = `${teamId}:${gw}`;
    const cached = lineupCache.get(key);
    if (cached) return cached;
    const team = teamById.get(teamId) as TeamRef;
    const lineup = lineupFor(teamId, gw);
    const subs = autoSubs(lineup, gw);
    const slots = lineup.slots.map((p, i) => toSlot(p, i + 1, gw, subs));
    const starters = slots.slice(0, 11);
    const bench = slots.slice(11);
    // Starters count unless subbed out; bench players count only once subbed in.
    const totalPoints = slots
      .filter((s) =>
        s.slot <= 11
          ? s.autoSub !== 'projected_out' && s.autoSub !== 'confirmed_out'
          : s.autoSub === 'projected_in' || s.autoSub === 'confirmed_in',
      )
      .reduce((sum, s) => sum + s.points, 0);
    const benchPoints = bench
      .filter((s) => s.autoSub !== 'projected_in' && s.autoSub !== 'confirmed_in')
      .reduce((sum, s) => sum + s.points, 0);
    const result: TeamLineup = {
      kind: 'team',
      team,
      formation: lineup.formation,
      totalPoints,
      benchPoints,
      starters,
      bench,
      autoSubs: subs,
    };
    lineupCache.set(key, result);
    return result;
  }

  /**
   * The league-average opponent: the floor of the mean gameweek points of
   * every real team, including the one it plays.
   */
  function averageOpponent(gw: number): AverageOpponent {
    const total = TEAMS.reduce((sum, t) => sum + teamLineup(t.id, gw).totalPoints, 0);
    return {
      kind: 'average',
      points: Math.floor(total / TEAMS.length),
      basis: 'floor_mean_all_teams',
    };
  }

  // ------------------------------------------------------------- schedule

  const SLOTS = [...TEAMS.map((t) => t.id), ...(TEAMS.length % 2 === 1 ? [AV] : [])];
  const ROUNDS = SLOTS.length - 1;

  /** Round robin by the circle method; with an odd count one team plays the average. */
  function roundPairings(round: number): [string, string][] {
    const fixed = SLOTS[0] as string;
    const rest = SLOTS.slice(1);
    const rot = round % rest.length;
    const rotated = [...rest.slice(rest.length - rot), ...rest.slice(0, rest.length - rot)];
    const order = [fixed, ...rotated];
    const pairs: [string, string][] = [];
    for (let i = 0; i < order.length / 2; i++) {
      const a = order[i] as string;
      const b = order[order.length - 1 - i] as string;
      // The real team is always home against the average.
      pairs.push(b === AV || (round % 2 === 0 && a !== AV) ? [a, b] : [b, a]);
    }
    return pairs;
  }

  const pairingsFor = (gw: number) => (gw < START ? [] : roundPairings((gw - START) % ROUNDS));
  const matchupId = (gw: number, k: number) => mockId('mu', off + gw * 10 + k);
  const scoreStateFor = (gw: number): ScoreState =>
    gw < CURRENT_GW ? 'final' : gw === CURRENT_GW ? 'live' : 'upcoming';
  const hasScores = (gw: number) => gw >= START && gw <= CURRENT_GW;

  function summarySide(slot: string, gw: number): MatchupSide {
    if (slot === AV) return averageOpponent(gw);
    const t = teamLineup(slot, gw);
    return { kind: 'team', team: t.team, points: t.totalPoints };
  }

  const detailSide = (slot: string, gw: number): MatchupDetailSide =>
    slot === AV ? averageOpponent(gw) : teamLineup(slot, gw);

  const sidePoints = (side: MatchupSide | MatchupDetailSide) =>
    side.kind === 'average' ? side.points : 'points' in side ? side.points : side.totalPoints;

  function matchupSummaries(gw: number): MatchupSummary[] {
    return pairingsFor(gw).map(([home, away], i) => ({
      id: matchupId(gw, i + 1),
      gameweek: gw,
      state: scoreStateFor(gw),
      home: summarySide(home, gw),
      away: summarySide(away, gw),
    }));
  }

  function matchupDetail(gw: number, id: string): MatchupDetail | undefined {
    if (!hasScores(gw)) return undefined;
    const pairs = pairingsFor(gw);
    const index = pairs.findIndex((_, i) => matchupId(gw, i + 1) === id);
    if (index < 0) return undefined;
    const [home, away] = pairs[index] as [string, string];
    return {
      id,
      gameweek: gw,
      state: scoreStateFor(gw),
      home: detailSide(home, gw),
      away: detailSide(away, gw),
    };
  }

  function averageMatchupId(gw: number): string | undefined {
    const index = pairingsFor(gw).findIndex(([h, a]) => h === AV || a === AV);
    return index < 0 ? undefined : matchupId(gw, index + 1);
  }

  const opponentOf = (slot: string): Opponent =>
    slot === AV ? { kind: 'average' } : { kind: 'team', team: teamById.get(slot) as TeamRef };

  function schedule(): Schemas['ScheduleWeek'][] {
    return Array.from({ length: 39 - START }, (_, i) => START + i).map((gw) => ({
      gameweek: gw,
      matchups: pairingsFor(gw).map(([h, a], k) => ({
        id: matchupId(gw, k + 1),
        gameweek: gw,
        home: opponentOf(h),
        away: opponentOf(a),
      })),
    }));
  }

  // Compute every played gameweek now, so later roster moves never change history.
  for (let gw = START; gw <= CURRENT_GW; gw++) TEAMS.forEach((t) => teamLineup(t.id, gw));

  // ------------------------------------------------------------ standings

  interface Tally {
    teamId: string;
    wins: number;
    draws: number;
    losses: number;
    pointsFor: number;
    pointsAgainst: number;
    form: FormResult[];
    results: Schemas['TeamResult'][];
  }

  function buildTallies() {
    const tally = new Map<string, Tally>(
      TEAMS.map((t) => [
        t.id,
        {
          teamId: t.id,
          wins: 0,
          draws: 0,
          losses: 0,
          pointsFor: 0,
          pointsAgainst: 0,
          form: [],
          results: [],
        },
      ]),
    );
    // Completed gameweeks only: the live one does not count yet. The average
    // opponent's result counts for the real team but it has no row itself.
    for (let gw = START; gw < CURRENT_GW; gw++) {
      pairingsFor(gw).forEach(([home, away], i) => {
        const id = matchupId(gw, i + 1);
        const points = {
          [home]: sidePoints(summarySide(home, gw)),
          [away]: sidePoints(summarySide(away, gw)),
        };
        for (const [me, other] of [
          [home, away],
          [away, home],
        ] as const) {
          if (me === AV) continue;
          const t = tally.get(me) as Tally;
          const mine = points[me] as number;
          const theirs = points[other] as number;
          const result: FormResult = mine > theirs ? 'W' : mine < theirs ? 'L' : 'D';
          if (result === 'W') t.wins++;
          else if (result === 'L') t.losses++;
          else t.draws++;
          t.pointsFor += mine;
          t.pointsAgainst += theirs;
          t.form.push(result);
          t.results.unshift({
            gameweek: gw,
            matchupId: id,
            opponent: opponentOf(other),
            pointsFor: mine,
            pointsAgainst: theirs,
            result,
          });
        }
      });
    }
    return tally;
  }

  const TALLIES = buildTallies();

  function standings(): StandingRow[] {
    const leaguePoints = (t: Tally) => t.wins * 3 + t.draws;
    return [...TALLIES.values()]
      .sort((a, b) => leaguePoints(b) - leaguePoints(a) || b.pointsFor - a.pointsFor)
      .map((t, i) => ({
        rank: i + 1,
        team: teamById.get(t.teamId) as TeamRef,
        played: t.wins + t.draws + t.losses,
        wins: t.wins,
        draws: t.draws,
        losses: t.losses,
        pointsFor: t.pointsFor,
        pointsAgainst: t.pointsAgainst,
        leaguePoints: leaguePoints(t),
        form: t.form.slice(-5),
      }));
  }

  function record(teamId: string): Schemas['Record'] | undefined {
    const row = standings().find((s) => s.team.id === teamId);
    if (!row) return undefined;
    const { rank, played, wins, draws, losses, pointsFor, pointsAgainst, leaguePoints } = row;
    return { rank, played, wins, draws, losses, pointsFor, pointsAgainst, leaguePoints };
  }

  /**
   * Roster order for the team page. The viewer (Sam) sees his own pending
   * lineup; another team's next lineup is private until the deadline, as on
   * the real server, so theirs follows the last locked lineup, with players
   * who joined since appended by position.
   */
  function rosterOrder(teamId: string): PlayerRecord[] {
    if (teamId === myTeam.id) return lineupFor(teamId, CURRENT_GW + 1).slots;
    const roster = rosterOf(teamId);
    const locked = lineupFor(teamId, CURRENT_GW).slots.filter((p) => roster.has(p.id));
    const seen = new Set(locked.map((p) => p.id));
    const order: Position[] = ['GKP', 'DEF', 'MID', 'FWD'];
    const joined = [...roster.keys()]
      .filter((id) => !seen.has(id))
      .map((id) => getPlayerRecord(id)!)
      .sort((a, b) => order.indexOf(a.position) - order.indexOf(b.position));
    return [...locked, ...joined];
  }

  /**
   * A future gameweek's preview order. Mine is the lineup set for `gw`;
   * another team's stays private until its deadline, so it follows the
   * roster order above: its last locked lineup, or before the league's
   * first gameweek (no locked lineup yet) the default order, with no
   * lineup gameweek.
   */
  function previewLineup(teamId: string, gw: number) {
    const mine = teamId === myTeam.id;
    const slots = mine ? lineupFor(teamId, gw).slots : rosterOrder(teamId);
    const src = mine ? gw : Math.min(gw, CURRENT_GW);
    return {
      formation: formationOf(slots),
      lineupGameweek: src >= START ? src : undefined,
      starters: slots.slice(0, 11).map(toPlayerRef),
      bench: slots.slice(11).map(toPlayerRef),
    };
  }

  function teamDetail(teamId: string): Schemas['TeamDetail'] | undefined {
    const team = teamById.get(teamId);
    const tally = TALLIES.get(teamId);
    const rec = record(teamId);
    if (!team || !tally || !rec) return undefined;
    const nextGw = CURRENT_GW + 1;
    const pairs = pairingsFor(nextGw);
    const nextIdx = pairs.findIndex(([a, b]) => a === teamId || b === teamId);
    const [h, a] = pairs[nextIdx] as [string, string];
    return {
      team,
      record: rec,
      roster: rosterOrder(teamId).map((p, i) => ({
        slot: i + 1,
        player: toPlayerRef(p),
        totalPoints: seasonTotals(p.id).points,
        gameweekPoints: pointsFor(p.id, CURRENT_GW),
        acquiredVia: acquiredVia.get(p.id) ?? 'draft',
        nextFixtures: rowFixturesFor(p.id),
      })),
      results: tally.results,
      nextOpponent: {
        gameweek: nextGw,
        matchupId: matchupId(nextGw, nextIdx + 1),
        opponent: opponentOf(h === teamId ? a : h),
        deadlineAt: gameweek(nextGw).deadlineAt,
      },
    };
  }

  // -------------------------------------------------------------- players

  const nextWaiverRun = () => gameweek(CURRENT_GW + 1).waiverDeadlineAt;
  /** Recently dropped players: on waivers until the next run. */
  const onWaivers = new Map<string, string>();
  {
    const r = mulberry32(cfg.seed + 7);
    const free = PLAYERS.filter((p) => !owner.has(p.id) && p.availability === 'available');
    const decent = free.sort((a, b) => b.quality - a.quality).slice(0, 40);
    for (let i = 0; i < 8; i++) onWaivers.set(pick(decent, r).id, nextWaiverRun());
  }

  function rosterStatus(playerId: string): Schemas['RosterStatus'] {
    if (owner.has(playerId)) return 'rostered';
    if (mockPhase.locked) return 'locked';
    return onWaivers.has(playerId) ? 'waivers' : 'free_agent';
  }

  /** When an unowned player clears: the next run while locked, else his waivers. */
  const clearsAt = (playerId: string): string | null =>
    owner.has(playerId)
      ? null
      : mockPhase.locked
        ? nextWaiverRun()
        : (onWaivers.get(playerId) ?? null);

  /** The league's transaction window: free agency, or locked (scenario). */
  function transactionPhase(): Schemas['TransactionPhase'] {
    const next = gameweek(CURRENT_GW + 1);
    return mockPhase.locked
      ? { phase: 'locked', gameweek: next.number, until: next.waiverDeadlineAt }
      : { phase: 'free_agency', gameweek: next.number, until: next.deadlineAt };
  }

  function playerListItem(p: PlayerRecord): Schemas['PlayerListItem'] {
    const totals = seasonTotals(p.id);
    return {
      player: toPlayerRef(p),
      owner: ownerOf(p.id),
      rosterStatus: rosterStatus(p.id),
      waiverClearsAt: clearsAt(p.id),
      totalPoints: totals.points,
      gameweekPoints: pointsFor(p.id, CURRENT_GW),
      form: formOf(p.id),
      draftRank: draftRankOf(p.id),
      minutes: totals.minutes,
      sortValue: totals.points,
      formChange: formChangeOf(p.id),
      seasonStats: seasonStatsOf(p.id),
      nextFixtures: rowFixturesFor(p.id),
    };
  }

  function playerDetail(playerId: string): Schemas['PlayerDetail'] | undefined {
    const p = getPlayerRecord(playerId);
    if (!p) return undefined;
    const totals = seasonTotals(playerId);
    return {
      player: toPlayerRef(p),
      season: SEASON,
      owner: ownerOf(playerId),
      rosterStatus: rosterStatus(playerId),
      waiverClearsAt: clearsAt(playerId),
      totalPoints: totals.points,
      form: formOf(playerId),
      draftRank: draftRankOf(playerId),
      minutes: totals.minutes,
      starts: startsOf(playerId),
      positionRank: positionRankOf(playerId),
      gameweeks: Array.from({ length: CURRENT_GW }, (_, i) => CURRENT_GW - i).map((gw) => ({
        gameweek: gw,
        points: pointsFor(playerId, gw),
        minutes: minutesFor(playerId, gw),
        fixtures: fixturesFor(playerId, gw),
        stats: statsFor(playerId, gw),
      })),
      upcoming: upcomingFor(playerId),
      previousSeasons: previousSeasonsFor(playerId),
    };
  }

  // --------------------------------------------------------- transactions

  type MockTxn =
    | 'waiver'
    | 'waiver_in_unavailable'
    | 'waiver_out_unavailable'
    | 'free_agent'
    | 'trade'
    | 'trade_vetoed';

  /** The activity feed: decided claims, free-agent moves and decided trades. */
  function buildTransactions(): Transaction[] {
    const r = mulberry32(cfg.seed + 99);
    const owned = PLAYERS.filter((p) => ownership.owner.has(p.id));
    const free = PLAYERS.filter((p) => !ownership.owner.has(p.id));
    const kinds: MockTxn[] = [];
    // A league that has not reached its first gameweek has made no moves.
    if (START > CURRENT_GW) return [];
    for (let i = 0; i < 40; i++) {
      const roll = r();
      kinds.push(
        roll < 0.3
          ? 'waiver'
          : roll < 0.45
            ? 'waiver_in_unavailable'
            : roll < 0.5
              ? 'waiver_out_unavailable'
              : roll < 0.88
                ? 'free_agent'
                : roll < 0.96
                  ? 'trade'
                  : 'trade_vetoed',
      );
    }
    kinds[0] = 'trade';
    kinds[1] = 'trade_vetoed';
    kinds[2] = 'waiver_in_unavailable';
    kinds[3] = 'waiver_out_unavailable';
    return kinds.map((kind, i): Transaction => {
      const gw = Math.max(START, CURRENT_GW - Math.floor(i / 6));
      const id = mockId('txn', off + 1000 - i);
      const occurredAt = new Date(NOW - (i + 1) * 5.5 * HOUR).toISOString();
      const team = pick(TEAMS, r);
      if (kind === 'trade' || kind === 'trade_vetoed') {
        const other = pick(
          TEAMS.filter((t) => t.id !== team.id),
          r,
        );
        const pos = pick(['DEF', 'MID', 'FWD'] as const, r);
        const mine = pick(
          owned.filter((p) => p.position === pos && ownership.owner.get(p.id) === team.id),
          r,
        );
        const theirs = pick(
          owned.filter((p) => p.position === pos && ownership.owner.get(p.id) === other.id),
          r,
        );
        return {
          id,
          kind: 'trade',
          occurredAt,
          gameweek: gw,
          status: kind === 'trade_vetoed' ? 'vetoed' : 'processed',
          sourceState: null,
          offeringTeam: team,
          receivingTeam: other,
          offered: [toPlayerRef(mine)],
          requested: [toPlayerRef(theirs)],
        };
      }
      const waiver = kind !== 'free_agent';
      const pos = pick(['GKP', 'DEF', 'MID', 'MID', 'FWD'] as const, r);
      const playerIn = toPlayerRef(
        pick(
          free.filter((p) => p.position === pos),
          r,
        ),
      );
      const playerOut = toPlayerRef(
        pick(
          owned.filter((p) => p.position === pos && ownership.owner.get(p.id) === team.id),
          r,
        ),
      );
      const result: Schemas['ClaimResult'] =
        kind === 'waiver_in_unavailable'
          ? { outcome: 'denied', reason: 'player_in_unavailable' }
          : kind === 'waiver_out_unavailable'
            ? { outcome: 'denied', reason: 'player_out_unavailable' }
            : { outcome: 'approved' };
      const base = {
        id,
        occurredAt,
        gameweek: gw,
        team,
        playerIn,
        playerOut,
        result,
        sourceState: null,
      };
      return waiver
        ? { ...base, kind: 'waiver', priority: 1 + Math.floor(r() * TEAMS.length) }
        : { ...base, kind: 'free_agent' };
    });
  }

  /** Newest first. Actions prepend to it. */
  const transactions: Transaction[] = buildTransactions();
  let txnSeq = 2000;

  // ------------------------------------------------------- roster moves

  function rosterOf(teamId: string): Map<string, Position> {
    return new Map(
      [...owner.entries()]
        .filter(([, t]) => t === teamId)
        .map(([pid]) => [pid, getPlayerRecord(pid)!.position]),
    );
  }

  /** Swaps one player for another in every current and saved lineup of a team. */
  function swapInLineups(teamId: string, outId: string, inId: string) {
    const p = getPlayerRecord(inId)!;
    const swap = (slots: PlayerRecord[]) => {
      const i = slots.findIndex((s) => s.id === outId);
      if (i >= 0) slots[i] = p;
    };
    swap(currentBase.get(teamId)!);
    (savedLineups.get(teamId) ?? []).forEach((s) => swap(s.slots));
    for (const key of [...lineupCache.keys()]) {
      const gw = Number(key.split(':')[1]);
      if (gw > CURRENT_GW) lineupCache.delete(key);
    }
  }

  function movePlayer(
    playerId: string,
    to: string | null,
    via: Schemas['RosterEntry']['acquiredVia'],
  ) {
    if (to) {
      owner.set(playerId, to);
      acquiredVia.set(playerId, via);
    } else {
      owner.delete(playerId);
      acquiredVia.delete(playerId);
    }
  }

  // ------------------------------------------------------------- lineups API

  /** The lineup resource. Before the deadline only the owner may see it. */
  function lineupView(teamId: string, gw: number): Schemas['Lineup'] {
    const team = teamById.get(teamId);
    if (!team) throw notFound('Team');
    const locked = gw <= CURRENT_GW;
    if (!locked && teamId !== myTeam.id) {
      throw forbidden('lineup-private', "Other managers' lineups are hidden until the deadline.");
    }
    if (gw < START) {
      throw new MockError(
        404,
        'no-roster',
        'No roster',
        'This team has no players for that gameweek yet (the draft has not run).',
      );
    }
    const lineup = lineupFor(teamId, gw);
    // A started gameweek carries the scores, so a team page needs no matchup.
    const scored = hasScores(gw) ? teamLineup(teamId, gw) : null;
    const scoreOf = (slot: number) =>
      scored ? [...scored.starters, ...scored.bench].find((s) => s.slot === slot) : undefined;
    const entry = (p: PlayerRecord, i: number): Schemas['LineupEntry'] => {
      const s = scoreOf(i + 1);
      return {
        slot: i + 1,
        player: toPlayerRef(p),
        fixtures: fixturesInGameweekOrPlayed(p.id, gw),
        season: {
          form: formOf(p.id),
          totalPoints: seasonTotals(p.id).points,
          appearances: appearancesOf(p.id),
          clubMatches: clubMatchesOf(p.id),
        },
        chanceOfPlaying: chanceOfPlaying(toPlayerRef(p)),
        ...(s
          ? {
              points: s.points,
              autoSub: s.autoSub,
              bonusProvisional: s.fixtures.some((f) => f.bonusProvisional),
            }
          : {}),
      };
    };
    return {
      team,
      gameweek: gw,
      locked,
      locksAt: gameweek(gw).deadlineAt,
      formation: lineup.formation,
      ...(scored
        ? {
            totalPoints: scored.totalPoints,
            benchPoints: scored.benchPoints,
            scoreState: scoreStateFor(gw),
          }
        : {}),
      starters: lineup.slots.slice(0, 11).map(entry),
      bench: lineup.slots.slice(11).map((p, i) => entry(p, i + 11)),
      updatedAt: new Date(NOW - 26 * HOUR).toISOString(),
    };
  }

  function setLineup(teamId: string, gw: number, starters: string[], bench: string[]) {
    if (!teamById.has(teamId)) throw notFound('Team');
    if (teamId !== myTeam.id)
      throw forbidden('not-team-owner', "Only the team's manager can set its lineup.");
    if (gw <= CURRENT_GW) {
      throw conflict(
        'lineup-locked',
        'Lineup locked',
        `Gameweek ${gw} has started. Changes now apply from gameweek ${CURRENT_GW + 1}.`,
      );
    }
    const errors = lineupViolations(rosterOf(teamId), starters, bench);
    if (errors.length > 0)
      throw invalid('invalid-lineup', 'That lineup breaks the formation rules.', errors);
    const slots = [...starters, ...bench].map((id) => getPlayerRecord(id)!);
    const list = (savedLineups.get(teamId) ?? []).filter((s) => s.fromGw < gw);
    list.push({ fromGw: gw, slots });
    savedLineups.set(teamId, list);
    for (const key of [...lineupCache.keys()]) {
      const [t, g] = key.split(':');
      if (t === teamId && Number(g) >= gw) lineupCache.delete(key);
    }
    return lineupView(teamId, gw);
  }

  // ------------------------------------------------------------- waivers

  const priority = () =>
    [...standings()].reverse().map((row, i) => ({ rank: i + 1, team: row.team }));

  function waiverState(): Schemas['WaiverState'] {
    return {
      type: 'rolling_priority',
      order: settings.waivers.order,
      nextRunGameweek: CURRENT_GW + 1,
      nextRunAt: nextWaiverRun(),
      lastRunAt: gameweek(CURRENT_GW).waiverDeadlineAt,
      priority: priority(),
    };
  }

  let claimSeq = 0;
  const claims: WaiverClaim[] = [];
  {
    // Two pending claims and three decided ones for the demo user's team.
    const mine = [...rosterOf(myTeam.id).keys()].map((id) => getPlayerRecord(id)!);
    const waiverPlayers = [...onWaivers.keys()].map((id) => getPlayerRecord(id)!);
    const bench = currentBase.get(myTeam.id)!.slice(11);
    let order = 1;
    for (const add of waiverPlayers) {
      if (order > 2) break;
      const drop = bench.find((b) => b.position === add.position);
      if (!drop || claims.some((c) => c.add.id === add.id)) continue;
      claims.push(newClaim(add, drop, order++, 'pending', null));
    }
    const free = PLAYERS.filter((p) => !owner.has(p.id) && !onWaivers.has(p.id));
    const decided: [Schemas['WaiverClaimStatus'], Schemas['WaiverClaimFailure'] | null][] = [
      ['won', null],
      ['lost', 'player_in_unavailable'],
      ['lost', 'player_out_unavailable'],
    ];
    decided.forEach(([status, reason], i) => {
      const drop = mine[(i * 5 + 3) % mine.length]!;
      const add = free.filter((p) => p.position === drop.position)[i * 3]!;
      const c = newClaim(add, drop, i + 1, status, reason);
      c.gameweek = CURRENT_GW;
      c.processAt = gameweek(CURRENT_GW).waiverDeadlineAt;
      c.processedAt = c.processAt;
      claims.push(c);
    });
  }

  function newClaim(
    add: PlayerRecord,
    drop: PlayerRecord,
    order: number,
    status: Schemas['WaiverClaimStatus'],
    failureReason: Schemas['WaiverClaimFailure'] | null,
  ): WaiverClaim {
    return {
      id: mockId('wv', off + ++claimSeq),
      team: myTeam,
      add: toPlayerRef(add),
      drop: toPlayerRef(drop),
      order,
      status,
      failureReason,
      gameweek: CURRENT_GW + 1,
      processAt: nextWaiverRun(),
      processedAt: null,
      createdAt: new Date(NOW - (10 - claimSeq) * HOUR).toISOString(),
    };
  }

  const pending = () =>
    claims.filter((c) => c.status === 'pending').sort((a, b) => a.order - b.order);

  function listClaims(status: 'pending' | 'processed'): WaiverClaim[] {
    if (status === 'pending') return pending();
    return claims
      .filter((c) => c.status === 'won' || c.status === 'lost')
      .sort((a, b) => (b.processedAt ?? '').localeCompare(a.processedAt ?? ''));
  }

  function checkSwap(addId: string, dropId: string) {
    const add = getPlayerRecord(addId);
    const drop = getPlayerRecord(dropId);
    if (!add) throw notFound('Player');
    if (!drop || owner.get(dropId) !== myTeam.id) {
      throw invalid('drop-not-on-roster', 'The player to drop is not on your team.', [
        {
          code: 'player_not_on_roster',
          message: 'The player to drop is not on your team.',
          field: '/dropPlayerId',
          playerId: dropId,
        },
      ]);
    }
    if (add.position !== drop.position) {
      throw invalid('position-mismatch', 'Drop a player of the same position.', [
        {
          code: 'position_mismatch',
          message: `${add.displayName} is a ${add.position}; drop a ${add.position}.`,
          field: '/dropPlayerId',
        },
      ]);
    }
    return { add, drop };
  }

  function createClaim(addId: string, dropId: string): WaiverClaim {
    const { add, drop } = checkSwap(addId, dropId);
    if (!onWaivers.has(addId)) {
      throw conflict(
        'player-not-on-waivers',
        'Not on waivers',
        owner.has(addId)
          ? `${add.displayName} is on a team.`
          : `${add.displayName} is a free agent: add him straight away.`,
      );
    }
    if (pending().some((c) => c.add.id === addId && c.drop.id === dropId)) {
      throw conflict('duplicate-claim', 'Duplicate claim', 'You already have this claim.');
    }
    const c = newClaim(add, drop, pending().length + 1, 'pending', null);
    claims.push(c);
    return c;
  }

  function findPending(claimId: string) {
    const c = claims.find((x) => x.id === claimId);
    if (!c) throw notFound('Claim');
    if (c.status !== 'pending')
      throw conflict('claim-not-pending', 'Claim decided', 'That claim was already processed.');
    return c;
  }

  function updateClaim(claimId: string, dropId: string): WaiverClaim {
    const c = findPending(claimId);
    const { drop } = checkSwap(c.add.id, dropId);
    c.drop = toPlayerRef(drop);
    return c;
  }

  function cancelClaim(claimId: string) {
    const c = findPending(claimId);
    c.status = 'cancelled';
    pending().forEach((x, i) => (x.order = i + 1));
  }

  function reorderClaims(ids: string[]): WaiverClaim[] {
    const current = pending();
    const same = ids.length === current.length && current.every((c) => ids.includes(c.id));
    if (!same)
      throw invalid('claim-order', 'Send the ids of every pending claim, once each, in order.');
    ids.forEach((id, i) => (claims.find((c) => c.id === id)!.order = i + 1));
    return pending();
  }

  function addFreeAgent(addId: string, dropId: string): Schemas['FreeAgentTransaction'] {
    const { add, drop } = checkSwap(addId, dropId);
    if (owner.has(addId))
      throw conflict('player-taken', 'Player taken', `${add.displayName} is on a team.`);
    if (onWaivers.has(addId)) {
      throw conflict(
        'player-on-waivers',
        'On waivers',
        `${add.displayName} is on waivers until ${onWaivers.get(addId)}; put in a claim.`,
      );
    }
    movePlayer(dropId, null, 'free_agent');
    movePlayer(addId, myTeam.id, 'free_agent');
    onWaivers.set(dropId, nextWaiverRun());
    swapInLineups(myTeam.id, dropId, addId);
    const txn: Schemas['FreeAgentTransaction'] = {
      id: mockId('txn', off + ++txnSeq),
      kind: 'free_agent',
      occurredAt: new Date().toISOString(),
      gameweek: CURRENT_GW + 1,
      team: myTeam,
      playerIn: toPlayerRef(add),
      playerOut: toPlayerRef(drop),
      result: { outcome: 'approved' },
      sourceState: null,
    };
    transactions.unshift(txn);
    return txn;
  }

  // -------------------------------------------------------------- trades

  const reviewHours = cfg.review === 'none' ? 0 : 24;
  const otherManagers = TEAMS.length - 2;
  const votesNeeded = Math.ceil(otherManagers * 0.5);
  let tradeSeq = 0;
  interface TradeState {
    trade: Omit<Trade, 'viewerActions'>;
    votes: Map<string, Schemas['TradeVote']>;
    /** Device time when the simulated manager accepts an offer Sam made. */
    respondAt?: number;
  }
  const trades: TradeState[] = [];

  function rosterPlayers(teamId: string, pos: Position) {
    return currentBase
      .get(teamId)!
      .filter((p) => p.position === pos)
      .sort((a, b) => b.quality - a.quality);
  }

  function seedTrade(
    from: TeamRef,
    to: TeamRef,
    status: Schemas['TradeStatus'],
    pos: Position,
    hoursAgo: number,
    extra: Partial<Omit<Trade, 'viewerActions'>> = {},
    votes: [number, Schemas['TradeVote']][] = [],
  ) {
    const offered = rosterPlayers(from.id, pos)[1]!;
    const requested = rosterPlayers(to.id, pos)[2]!;
    const createdAt = new Date(NOW - hoursAgo * HOUR).toISOString();
    const state: TradeState = {
      trade: {
        id: mockId('trd', off + ++tradeSeq),
        status,
        offeringTeam: from,
        receivingTeam: to,
        offered: [toPlayerRef(offered)],
        requested: [toPlayerRef(requested)],
        message: null,
        createdAt,
        expiresAt: nextWaiverRun(),
        respondedAt:
          status === 'offered' ? null : new Date(NOW - (hoursAgo - 2) * HOUR).toISOString(),
        review: null,
        processedAt:
          status === 'processed' ? new Date(NOW - (hoursAgo - 26) * HOUR).toISOString() : null,
        effectiveGameweek: status === 'processed' ? CURRENT_GW : null,
        failureReason: null,
        ...extra,
      },
      votes: new Map(votes.map(([i, v]) => [TEAMS[i]!.id, v])),
    };
    const reviewed = status === 'in_review' || status === 'processed' || status === 'vetoed';
    if (cfg.review !== 'none' && reviewed && !extra.review) state.trade.review = reviewState(state);
    trades.push(state);
  }

  function reviewState(s: TradeState): Schemas['TradeReviewState'] {
    const vetoVotes = [...s.votes.values()].filter((v) => v === 'veto').length;
    const allowVotes = [...s.votes.values()].filter((v) => v === 'allow').length;
    const endsAt =
      s.trade.status === 'in_review'
        ? new Date(
            Date.parse(s.trade.respondedAt ?? s.trade.createdAt) + reviewHours * HOUR,
          ).toISOString()
        : (s.trade.review?.endsAt ?? null);
    return {
      mode: cfg.review,
      endsAt,
      vetoVotes,
      allowVotes,
      votesNeeded: cfg.review === 'league_vote' ? votesNeeded : null,
      myVote: s.votes.get(myTeam.id) ?? null,
      decidedBy: s.trade.review?.decidedBy ?? null,
    };
  }

  {
    const others = TEAMS.filter((t) => t.id !== myTeam.id);
    const [a, b, c, d, e] = others as [TeamRef, TeamRef, TeamRef, TeamRef, TeamRef];
    seedTrade(a, myTeam, 'offered', 'MID', 3, {
      message: 'Need a midfielder for the double. Fair?',
    });
    seedTrade(myTeam, b, 'offered', 'FWD', 6);
    if (cfg.review !== 'none') {
      seedTrade(c, d, 'in_review', 'DEF', 10, {}, [
        [TEAMS.indexOf(e), 'veto'],
        [TEAMS.indexOf(b), 'allow'],
      ]);
    }
    seedTrade(d, e, 'processed', 'MID', 60);
    seedTrade(myTeam, c, 'declined', 'DEF', 80);
    seedTrade(b, myTeam, 'withdrawn', 'FWD', 90);
    seedTrade(e, a, 'expired', 'MID', 120);
    if (cfg.review !== 'none')
      seedTrade(a, b, 'vetoed', 'FWD', 140, {
        review: {
          mode: cfg.review,
          endsAt: new Date(NOW - 110 * HOUR).toISOString(),
          vetoVotes: votesNeeded,
          allowVotes: 1,
          votesNeeded: cfg.review === 'league_vote' ? votesNeeded : null,
          myVote: 'veto',
          decidedBy: null,
        },
      });
  }

  function viewerActions(t: Omit<Trade, 'viewerActions'>): Schemas['TradeAction'][] {
    const mine = myTeam.id;
    const out: Schemas['TradeAction'][] = [];
    if (t.status === 'offered' && t.receivingTeam.id === mine) out.push('accept', 'decline');
    if (t.status === 'offered' && t.offeringTeam.id === mine) out.push('withdraw');
    if (t.status === 'in_review') {
      const inTrade = t.offeringTeam.id === mine || t.receivingTeam.id === mine;
      if (cfg.review === 'league_vote' && !inTrade) out.push('vote');
      // A commissioner cannot review a trade of their own team.
      if (isCommissioner() && !inTrade) out.push('review');
    }
    return out;
  }

  const tradeView = (s: TradeState): Trade => {
    if (s.trade.review && s.trade.status === 'in_review') s.trade.review = reviewState(s);
    return { ...s.trade, viewerActions: viewerActions(s.trade) };
  };

  /** Offers between two other teams stay private until accepted. */
  const visible = (s: TradeState) =>
    s.trade.status !== 'offered' ||
    s.trade.offeringTeam.id === myTeam.id ||
    s.trade.receivingTeam.id === myTeam.id;

  function listTrades(status?: string | null, teamId?: string | null): Trade[] {
    const open = ['offered', 'accepted', 'in_review'];
    return trades
      .filter(visible)
      .filter((s) =>
        !status
          ? true
          : status === 'open'
            ? open.includes(s.trade.status)
            : s.trade.status === status,
      )
      .filter(
        (s) => !teamId || s.trade.offeringTeam.id === teamId || s.trade.receivingTeam.id === teamId,
      )
      .sort((a, b) => b.trade.createdAt.localeCompare(a.trade.createdAt))
      .map(tradeView);
  }

  function findTrade(tradeId: string) {
    const s = trades.find((x) => x.trade.id === tradeId);
    if (!s || !visible(s)) throw notFound('Trade');
    return s;
  }
  const getTrade = (tradeId: string) => tradeView(findTrade(tradeId));

  function proposeTrade(
    receivingTeamId: string,
    offeredIds: string[],
    requestedIds: string[],
    message?: string,
  ) {
    const to = teamById.get(receivingTeamId);
    if (!to) throw notFound('Team');
    if (to.id === myTeam.id) throw invalid('trade-shape', 'You cannot trade with yourself.');
    const posKey = (ids: string[]) =>
      ids
        .map((id) => getPlayerRecord(id)?.position ?? '?')
        .sort()
        .join(',');
    const errors: Schemas['ProblemError'][] = [];
    offeredIds.forEach((id) => {
      if (owner.get(id) !== myTeam.id)
        errors.push({
          code: 'player_not_on_roster',
          message: 'Not on your team.',
          playerId: id,
          field: '/offeredPlayerIds',
        });
    });
    requestedIds.forEach((id) => {
      if (owner.get(id) !== to.id)
        errors.push({
          code: 'player_not_on_roster',
          message: `Not on ${to.name}.`,
          playerId: id,
          field: '/requestedPlayerIds',
        });
    });
    if (posKey(offeredIds) !== posKey(requestedIds)) {
      errors.push({
        code: 'position_mismatch',
        message: 'Both sides must trade the same number of players in the same positions.',
      });
    }
    if (errors.length) throw invalid('trade-shape', 'That trade is not allowed.', errors);
    const s: TradeState = {
      trade: {
        id: mockId('trd', off + ++tradeSeq),
        status: 'offered',
        offeringTeam: myTeam,
        receivingTeam: to,
        offered: offeredIds.map((id) => toPlayerRef(getPlayerRecord(id)!)),
        requested: requestedIds.map((id) => toPlayerRef(getPlayerRecord(id)!)),
        message: message ?? null,
        createdAt: new Date().toISOString(),
        expiresAt: nextWaiverRun(),
        respondedAt: null,
        review: null,
        processedAt: null,
        effectiveGameweek: null,
        failureReason: null,
      },
      votes: new Map(),
      respondAt: Date.now() + OTHER_MANAGER_RESPONSE_MS,
    };
    trades.push(s);
    return tradeView(s);
  }

  /**
   * The other managers are simulated: a few seconds after Sam offers a
   * trade, the receiving manager accepts it. With no review it processes
   * at once (and joins the activity feed); otherwise it goes into review.
   * Runs lazily before every request to the league.
   */
  function settleTrades(now = Date.now()) {
    for (const s of trades) {
      if (s.respondAt === undefined || s.respondAt > now) continue;
      s.respondAt = undefined;
      if (s.trade.status !== 'offered') continue;
      s.trade.respondedAt = new Date(now).toISOString();
      if (cfg.review === 'none') {
        execute(s);
      } else {
        s.trade.status = 'in_review';
        s.trade.review = reviewState(s);
      }
    }
  }

  function execute(s: TradeState) {
    const from = s.trade.offeringTeam.id;
    const to = s.trade.receivingTeam.id;
    // Pair by position (the offer's order need not match), so each player
    // takes the slot of a same-position player and formations stay legal.
    const unpaired = [...s.trade.requested];
    s.trade.offered.forEach((p) => {
      const at = unpaired.findIndex((r) => r.position === p.position);
      const back = unpaired.splice(at, 1)[0]!;
      movePlayer(p.id, to, 'trade');
      movePlayer(back.id, from, 'trade');
      swapInLineups(from, p.id, back.id);
      swapInLineups(to, back.id, p.id);
    });
    s.trade.status = 'processed';
    s.trade.processedAt = new Date().toISOString();
    s.trade.effectiveGameweek = CURRENT_GW + 1;
    transactions.unshift({
      id: mockId('txn', off + ++txnSeq),
      kind: 'trade',
      occurredAt: s.trade.processedAt,
      gameweek: CURRENT_GW + 1,
      status: 'processed',
      sourceState: null,
      offeringTeam: s.trade.offeringTeam,
      receivingTeam: s.trade.receivingTeam,
      offered: s.trade.offered,
      requested: s.trade.requested,
    });
  }

  /**
   * A re-sync of an imported league (ADR 0012): open trades fail with
   * `league_resynced` and pending claims are cancelled. Returns what it
   * closed, for the league that replaces this one to keep in its history.
   */
  function cancelOpen() {
    const open = ['offered', 'accepted', 'in_review'];
    const closedTrades = trades.filter((s) => open.includes(s.trade.status));
    closedTrades.forEach((s) => {
      s.trade.status = 'failed';
      s.trade.failureReason = 'league_resynced';
      s.respondAt = undefined;
    });
    const closedClaims = pending();
    closedClaims.forEach((c) => (c.status = 'cancelled'));
    return { trades: closedTrades, claims: closedClaims };
  }

  /** Keeps what another instance closed (new ids, so none collide with this one's). */
  function adoptClosed(from: ReturnType<typeof cancelOpen>) {
    for (const s of from.trades) {
      if (trades.some((x) => x.trade.id === s.trade.id)) continue;
      s.trade.id = mockId('trd', off + ++tradeSeq);
      trades.push(s);
    }
    for (const c of from.claims) {
      if (claims.some((x) => x.id === c.id)) continue;
      c.id = mockId('wv', off + ++claimSeq);
      claims.push(c);
    }
  }

  const notOpen = () => conflict('trade-not-open', 'Trade closed', 'That trade is no longer open.');

  function acceptTrade(tradeId: string) {
    const s = findTrade(tradeId);
    if (s.trade.receivingTeam.id !== myTeam.id)
      throw forbidden('not-your-trade', 'Only the receiving team can accept.');
    if (s.trade.status !== 'offered') throw notOpen();
    s.trade.respondedAt = new Date().toISOString();
    if (cfg.review === 'none') {
      execute(s);
    } else {
      s.trade.status = 'in_review';
      s.trade.review = reviewState(s);
    }
    return tradeView(s);
  }

  function declineTrade(tradeId: string) {
    const s = findTrade(tradeId);
    if (s.trade.receivingTeam.id !== myTeam.id)
      throw forbidden('not-your-trade', 'Only the receiving team can decline.');
    if (s.trade.status !== 'offered') throw notOpen();
    s.trade.status = 'declined';
    s.trade.respondedAt = new Date().toISOString();
    return tradeView(s);
  }

  function withdrawTrade(tradeId: string) {
    const s = findTrade(tradeId);
    if (s.trade.offeringTeam.id !== myTeam.id)
      throw forbidden('not-your-trade', 'Only the offering team can withdraw.');
    if (s.trade.status !== 'offered') throw notOpen();
    s.trade.status = 'withdrawn';
    return tradeView(s);
  }

  function voteOnTrade(tradeId: string, vote: Schemas['TradeVote']) {
    const s = findTrade(tradeId);
    if (cfg.review !== 'league_vote')
      throw conflict('no-league-vote', 'No vote', 'This league does not vote on trades.');
    if (s.trade.offeringTeam.id === myTeam.id || s.trade.receivingTeam.id === myTeam.id) {
      throw forbidden('in-trade', 'Teams in the trade cannot vote on it.');
    }
    if (s.trade.status !== 'in_review')
      throw conflict('trade-not-in-review', 'Not in review', 'Voting has closed.');
    s.votes.set(myTeam.id, vote);
    s.trade.review = reviewState(s);
    if (s.trade.review.vetoVotes >= votesNeeded) s.trade.status = 'vetoed';
    return tradeView(s);
  }

  function reviewTrade(tradeId: string, decision: 'approve' | 'veto') {
    const s = findTrade(tradeId);
    if (!isCommissioner())
      throw forbidden('not-commissioner', 'Only the commissioner can review trades.');
    if (s.trade.offeringTeam.id === myTeam.id || s.trade.receivingTeam.id === myTeam.id)
      throw forbidden('own-trade', 'You cannot review a trade of your own team.');
    if (s.trade.status !== 'in_review')
      throw conflict('trade-not-in-review', 'Not in review', 'That trade is not in review.');
    s.trade.review = {
      ...reviewState(s),
      decidedBy: { id: DEMO_USER_ID, displayName: DEMO_USER_NAME },
    };
    if (decision === 'veto') s.trade.status = 'vetoed';
    else execute(s);
    return tradeView(s);
  }

  // --------------------------------------------------------------- league

  const settings = leagueSettings({
    teamCount: TEAMS.length,
    review: cfg.review,
    startGameweek: START,
  });

  function summary(): Schemas['LeagueSummary'] {
    return {
      id: cfg.id,
      name: cfg.name,
      season: SEASON,
      status: 'in_season',
      source: 'native',
      teamCount: TEAMS.length,
      memberCount: TEAMS.length,
      fplLeagueId: null,
    };
  }

  const viewer = (): Schemas['LeagueViewer'] => ({
    role: isCommissioner() ? 'commissioner' : 'member',
    team: myTeam,
  });

  function home(): Schemas['LeagueHome'] {
    return {
      transactionPhase: transactionPhase(),
      league: summary(),
      settings,
      viewer: viewer(),
      currentGameweek: gameweek(LEAGUE_GW),
      standings: standings(),
      matchups: matchupSummaries(LEAGUE_GW),
    };
  }

  function members(): Schemas['Member'][] {
    const list = TEAMS.map((t, i) => ({
      user: { id: userOfTeam.get(t.id)!, displayName: t.managerName },
      team: t,
      role: (userOfTeam.get(t.id) === commissionerUserId
        ? 'commissioner'
        : 'member') as Schemas['LeagueRole'],
      joinedAt: new Date(Date.parse(cfg.createdAt) + i * 3 * HOUR).toISOString(),
    }));
    return [
      ...list.filter((m) => m.role === 'commissioner'),
      ...list.filter((m) => m.role !== 'commissioner'),
    ];
  }

  function myLeague(): Schemas['MyLeague'] {
    const mu = matchupSummaries(LEAGUE_GW).find(
      (m) =>
        (m.home.kind === 'team' && m.home.team.id === myTeam.id) ||
        (m.away.kind === 'team' && m.away.team.id === myTeam.id),
    );
    return {
      league: summary(),
      team: myTeam,
      role: viewer().role,
      record: record(myTeam.id) ?? null,
      currentMatchup: mu ?? null,
      draftStartsAt: null,
    };
  }

  function rename(name: string) {
    if (!isCommissioner())
      throw forbidden('not-commissioner', 'Only the commissioner can rename the league.');
    cfg.name = name;
  }

  function transferCommissioner(userId: string) {
    if (!isCommissioner())
      throw forbidden('not-commissioner', 'Only the commissioner can do that.');
    if (userId === commissionerUserId) return;
    if (![...userOfTeam.values()].includes(userId)) throw notFound('Member');
    commissionerUserId = userId;
  }

  return {
    id: cfg.id,
    name: cfg.name,
    status: 'in_season' as const,
    rename,
    transferCommissioner,
    TEAMS: TEAMS as readonly TeamRef[],
    myTeam,
    isCommissioner,
    get commissionerUserId() {
      return commissionerUserId;
    },
    settings,
    createdAt: cfg.createdAt,
    /** The league's current gameweek: never before its first (startGameweek). */
    leagueGameweek: LEAGUE_GW,
    // reads
    summary,
    home,
    viewer,
    members,
    myLeague,
    ownerOf,
    draftPicks,
    teamById: (id: string) => teamById.get(id),
    teamDetail,
    standings,
    schedule,
    matchupSummaries,
    matchupDetail,
    averageMatchupId,
    hasScores,
    scoreStateFor,
    sidePoints,
    playerListItem,
    playerDetail,
    rosterStatus,
    transactionPhase,
    transactions: () => transactions as readonly Transaction[],
    lineupView,
    previewLineup,
    fixturesInGameweek: fixturesInGameweekOrPlayed,
    /** Player ids in slot order (1-15) of the lineup a team uses in `gw` (projections). */
    lineupSlots: (teamId: string, gw: number) => lineupFor(teamId, gw).slots.map((p) => p.id),
    waiverState,
    listClaims,
    listTrades,
    getTrade,
    // actions
    setLineup,
    createClaim,
    updateClaim,
    cancelClaim,
    reorderClaims,
    addFreeAgent,
    proposeTrade,
    acceptTrade,
    declineTrade,
    withdrawTrade,
    voteOnTrade,
    reviewTrade,
    settleTrades,
    cancelOpen,
    adoptClosed,
  };
}

/** A gameweek's fixtures for a lineup entry: played fixtures in the past, upcoming ones ahead. */
function fixturesInGameweekOrPlayed(playerId: string, gw: number): Schemas['UpcomingFixture'][] {
  if (gw > CURRENT_GW) return fixturesInGameweek(playerId, gw);
  return fixturesFor(playerId, gw).map((f) => ({
    fixtureId: f.fixtureId,
    gameweek: gw,
    opponent: f.opponent,
    isHome: f.isHome,
    kickoffAt: f.kickoffAt,
    status: f.status,
  }));
}

export { MockError };
