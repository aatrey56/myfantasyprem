/**
 * Mock data for every screen, typed against the generated API types. Three
 * tiers, as in production (docs/ARCHITECTURE.md): the global world of
 * players, clubs, fixtures and points (world.ts) shared by every league,
 * per-league state (league.ts, preseason.ts), and the demo user's private
 * lineups, claims and queue inside each league.
 *
 * The mock user is "Sam P." ({@link DEMO_USER_ID}); requests need no token.
 * Leagues:
 *
 * - {@link LEAGUE_ID} "The Sunday Draft": 9 teams mid-season, so one team
 *   plays the league average each gameweek; no trade review.
 * - {@link OFFICE_LEAGUE_ID} "The Office League": 10 teams mid-season, Sam
 *   is commissioner; trades go to a league vote; pending and processed
 *   trades and waiver claims.
 * - {@link DRAFT_LEAGUE_ID} "Draft Day Derby": 8 teams in a live snake
 *   draft, Sam on the clock at pick 43; other managers pick every 2.5 s.
 * - {@link PREDRAFT_LEAGUE_ID} "Five-a-side Friends": inviting (5 of 8),
 *   Sam is commissioner; invite code {@link PREDRAFT_INVITE_CODE}.
 * - {@link PUB_LEAGUE_ID} "The Pub League": Sam is not in it; join it with
 *   invite code {@link PUB_INVITE_CODE}.
 * - {@link LATE_LEAGUE_ID} "Late Kickoff": 6 teams drafted mid-season,
 *   after gameweek 7's deadline, so its first gameweek is 8 (upcoming):
 *   no scores, results or moves yet. Sam is commissioner.
 * - {@link DUO_LEAGUE_ID} "Two-Up League": inviting (2 of 6), Sam is
 *   commissioner and can start the draft at once; the other 4 places
 *   become open seats.
 * - Imported from FPL Draft, playable like any league (imported.ts, ADR
 *   0012): {@link IMPORTED_LEAGUE_ID} (Sam is commissioner, verification
 *   clean, six seats to claim), {@link MISMATCH_LEAGUE_ID} (Sam is
 *   commissioner, two mismatches in the check), {@link PENDING_LEAGUE_ID}
 *   (Sam is commissioner, a 3-for-3 trade accepted on FPL and pending
 *   there) and {@link CLAIM_LEAGUE_ID} (Sam claims a team with
 *   {@link CLAIM_CODE}).
 *
 * Everything is invented: clubs, players and managers are fictional.
 */
import type { Schemas } from '@myfantasyprem/api-client';

import { createImportedLeague, type ImportedLeague } from './imported';
import { createInSeasonLeague, type InSeasonLeague } from './league';
import { createPreseasonLeague, type PreseasonLeague } from './preseason';
import { CURRENT_GW, HOUR, NOW, mockId } from './world';

export * from './world';
export { DEMO_USER_ID, DEMO_USER_NAME } from './league';

export const LEAGUE_ID = 'lg_01k7m2xq8rfv3c9a4t6wbn5hdy';
export const LEAGUE_NAME = 'The Sunday Draft';
export const FPL_LEAGUE_ID = 12345;
export const OFFICE_LEAGUE_ID = 'lg_01k7m2xq8rfv3c9a4t6wbn5hd0';
export const DRAFT_LEAGUE_ID = 'lg_01k7m2xq8rfv3c9a4t6wbn5hd1';
export const PREDRAFT_LEAGUE_ID = 'lg_01k7m2xq8rfv3c9a4t6wbn5hd2';
export const PUB_LEAGUE_ID = 'lg_01k7m2xq8rfv3c9a4t6wbn5hd3';
export const LATE_LEAGUE_ID = 'lg_01k7m2xq8rfv3c9a4t6wbn5hd4';
export const LATE_LEAGUE_NAME = 'Late Kickoff';
export const PREDRAFT_INVITE_CODE = 'K7QM2XRP4D';
/** Imported from FPL Draft; Sam is commissioner; the check is clean. */
export const IMPORTED_LEAGUE_ID = 'lg_01k7m2xq8rfv3c9a4t6wbn5hd5';
export const IMPORTED_LEAGUE_NAME = 'Thursday Night Draft';
/** Imported; Sam is commissioner; the check has two mismatches (informational). */
export const MISMATCH_LEAGUE_ID = 'lg_01k7m2xq8rfv3c9a4t6wbn5hd6';
export const MISMATCH_LEAGUE_NAME = 'The Saturday Club';
/** Pre-draft with 2 of 6 managers; Sam is commissioner and can start the draft. */
export const DUO_LEAGUE_ID = 'lg_01k7m2xq8rfv3c9a4t6wbn5hd8';
export const DUO_LEAGUE_NAME = 'Two-Up League';
/** Imported; Sam is not in it yet: {@link CLAIM_CODE} claims a team. */
export const CLAIM_LEAGUE_ID = 'lg_01k7m2xq8rfv3c9a4t6wbn5hd7';
export const CLAIM_LEAGUE_NAME = 'Away Days Draft';
export const CLAIM_CODE = 'SEATCHKX42';
export const CLAIM_TEAM_NAME = 'Away End Allstars';
/** Imported; Sam is commissioner; a trade accepted on FPL is pending there. */
export const PENDING_LEAGUE_ID = 'lg_01k7m2xq8rfv3c9a4t6wbn5hd9';
export const PENDING_LEAGUE_NAME = 'Midweek Draft Club';
/** An 8-character code from before codes grew to 10: still accepted. */
export const PUB_INVITE_CODE = 'DERBY7XQ';

const SUNDAY_TEAMS = [
  ['Sunday League Heroes', 'Sam P.'],
  ["Gaffer's Army", 'Priya N.'],
  ['Expected Goals Club', 'Dan W.'],
  ['Park The Bus FC', 'Tomasz K.'],
  ['Bottle Job United', 'Chloe R.'],
  ['The Wing Backs', 'Marcus L.'],
  ['Clean Sheet Society', 'Aisha B.'],
  ['Late Winner Syndicate', 'Jonas E.'],
  ['Rotation Roulette', 'Hannah G.'],
] as const;

const OFFICE_TEAMS = [
  ['Galacticos on a Budget', 'Sam P.'],
  ['Spreadsheet Wanderers', 'Ruth A.'],
  ['Reply All Rovers', 'Kwame O.'],
  ['Standup Strikers', 'Mei L.'],
  ['Out of Office XI', 'Gareth H.'],
  ['Synergy City', 'Fatima Z.'],
  ['The Deadline Merchants', 'Luca B.'],
  ['Quarterly Targets', 'Niamh C.'],
  ['Hot Desk Hotspurs', 'Oskar J.'],
  ['Circle Back United', 'Dev P.'],
] as const;

const DRAFT_TEAMS = [
  ['Snake Order Saints', 'Ben T.'],
  ['First Pick Fever', 'Ana M.'],
  ['Value Hunters', 'Rory D.'],
  ['Bench Boost Bandits', 'Yuki S.'],
  ['The Sleepers', 'Imran K.'],
  ['Draft Day Disasters', 'Sam P.'],
  ['Positional Scarcity', 'Elena V.'],
  ['Late Round Steals', 'Tom W.'],
] as const;

const PREDRAFT_TEAMS = [
  ['Five-a-side Legends', 'Sam P.'],
  ['Jumpers for Goalposts', 'Chris B.'],
  ['Rush Goalie FC', 'Sofia R.'],
  ['Next Goal Wins', 'Adam F.'],
  ['Wall Pass Wanderers', 'Leah M.'],
] as const;

const DUO_TEAMS = [
  ['Two-Up Titans', 'Sam P.'],
  ['Second Man In', 'Wes D.'],
] as const;

const LATE_TEAMS = [
  ['Better Late Than Never', 'Sam P.'],
  ['Transfer Window Shoppers', 'Grace O.'],
  ['International Break FC', 'Femi A.'],
  ['The January Signings', 'Lars N.'],
  ['Second Half Specialists', 'Zoe H.'],
  ['Injury Time Athletic', 'Kofi D.'],
] as const;
const IMPORTED_TEAMS = [
  ['Thursday Thunder', 'Sam P.'],
  ['Kebab Shop Rovers', 'Alex M.'],
  ['Nil Nil Merchants', 'Bella K.'],
  ['Route One FC', 'Priya N.'],
  ['False Nine Club', 'Callum S.'],
  ['Two Banks of Four', 'Dana F.'],
  ['Inverted Fullbacks', 'Eddie W.'],
  ['The Overlap', 'Farah J.'],
] as const;

const MISMATCH_TEAMS = [
  ['Saturday Strollers', 'Sam P.'],
  ['Half-Time Oranges', 'Gina L.'],
  ['Corner Flag Crew', 'Hugo B.'],
  ['Offside Trap XI', 'Iris T.'],
  ['The Back Post', 'Jack R.'],
  ['Stoppage Time FC', 'Kim D.'],
] as const;

const PENDING_TEAMS = [
  ['Midweek Marauders', 'Sam P.'],
  ['Floodlit FC', 'Quinn A.'],
  ['Wet Tuesday Wanderers', 'Rosa E.'],
  ['Cup Run Collective', 'Theo H.'],
  ['Squad Rotation Rovers', 'Uma K.'],
  ['Late Winner XI', 'Vik S.'],
] as const;

const CLAIM_TEAMS = [
  ['Ticket Stub United', 'Leo V.'],
  ['Coach Trip Athletic', 'Mona G.'],
  [CLAIM_TEAM_NAME, 'Sam P.'],
  ['Motorway Services FC', 'Nat O.'],
  ['Programme Collectors', 'Owen C.'],
  ['Scarf Sellers', 'Pia H.'],
] as const;

const PUB_TEAMS = [
  ['The Last Orders', 'Martin G.'],
  ['Pint and a Parlay', 'Jo S.'],
  ['Quiz Night Rovers', 'Ollie P.'],
  ['Darts Board Dynamos', 'Kezia N.'],
  ['Lock-in Athletic', 'Pete R.'],
  ['Happy Hour Hammers', 'Ravi M.'],
  ['The Snug XI', 'Bea T.'],
] as const;

export type MockLeague = InSeasonLeague | PreseasonLeague | ImportedLeague;
export type { InSeasonLeague, ImportedLeague };
export const isInSeason = (l: MockLeague): l is InSeasonLeague | ImportedLeague =>
  'matchupSummaries' in l;
export const isImported = (l: MockLeague): l is ImportedLeague => 'imported' in l;

function buildLeagues(): Map<string, MockLeague> {
  const created = new Date(NOW - 60 * 24 * HOUR).toISOString();
  const leagues: MockLeague[] = [
    createInSeasonLeague({
      id: LEAGUE_ID,
      name: LEAGUE_NAME,
      idOffset: 0,
      teams: SUNDAY_TEAMS,
      myTeamIndex: 0,
      commissionerIndex: 1,
      review: 'none',
      draftJitter: 0,
      seed: 0,
      createdAt: created,
    }),
    createInSeasonLeague({
      id: OFFICE_LEAGUE_ID,
      name: 'The Office League',
      idOffset: 1_000_000,
      teams: OFFICE_TEAMS,
      myTeamIndex: 0,
      commissionerIndex: 0,
      review: 'league_vote',
      draftJitter: 0.35,
      seed: 4242,
      createdAt: created,
    }),
    createPreseasonLeague({
      id: DRAFT_LEAGUE_ID,
      name: 'Draft Day Derby',
      idOffset: 2_000_000,
      status: 'drafting',
      teamCount: 8,
      teams: DRAFT_TEAMS,
      myTeamIndex: 5,
      commissionerIndex: 5,
      review: 'commissioner',
      pickTimerSeconds: 90,
      draftStartsAt: new Date(NOW - 35 * 60_000).toISOString(),
      picksMade: 42,
      inviteCode: 'H4WK3RSZ',
      seed: 77,
      createdAt: new Date(NOW - 9 * 24 * HOUR).toISOString(),
    }),
    createPreseasonLeague({
      id: PREDRAFT_LEAGUE_ID,
      name: 'Five-a-side Friends',
      idOffset: 3_000_000,
      status: 'pre_draft',
      teamCount: 8,
      teams: PREDRAFT_TEAMS,
      myTeamIndex: 0,
      commissionerIndex: 0,
      review: 'none',
      pickTimerSeconds: 60,
      draftStartsAt: new Date(NOW + 3 * 24 * HOUR).toISOString(),
      inviteCode: PREDRAFT_INVITE_CODE,
      seed: 5,
      createdAt: new Date(NOW - 2 * 24 * HOUR).toISOString(),
    }),
    createPreseasonLeague({
      id: DUO_LEAGUE_ID,
      name: DUO_LEAGUE_NAME,
      idOffset: 10_000_000,
      status: 'pre_draft',
      teamCount: 6,
      teams: DUO_TEAMS,
      myTeamIndex: 0,
      commissionerIndex: 0,
      review: 'none',
      pickTimerSeconds: 60,
      draftStartsAt: null,
      inviteCode: 'DQ2M7XK3RP',
      seed: 9,
      createdAt: new Date(NOW - 24 * HOUR).toISOString(),
    }),
    createPreseasonLeague({
      id: PUB_LEAGUE_ID,
      name: 'The Pub League',
      idOffset: 4_000_000,
      status: 'pre_draft',
      teamCount: 10,
      teams: PUB_TEAMS,
      myTeamIndex: -1,
      commissionerIndex: 0,
      review: 'commissioner',
      pickTimerSeconds: 120,
      draftStartsAt: new Date(NOW + 5 * 24 * HOUR).toISOString(),
      inviteCode: PUB_INVITE_CODE,
      seed: 6,
      createdAt: new Date(NOW - 24 * HOUR).toISOString(),
    }),
    createInSeasonLeague({
      id: LATE_LEAGUE_ID,
      name: LATE_LEAGUE_NAME,
      idOffset: 5_000_000,
      teams: LATE_TEAMS,
      myTeamIndex: 0,
      commissionerIndex: 0,
      review: 'commissioner',
      draftJitter: 0.5,
      seed: 808,
      createdAt: new Date(NOW - 3 * 24 * HOUR).toISOString(),
      startGameweek: CURRENT_GW + 1,
    }),
    createImportedLeague({
      id: IMPORTED_LEAGUE_ID,
      name: IMPORTED_LEAGUE_NAME,
      idOffset: 6_000_000,
      teams: IMPORTED_TEAMS,
      myTeamIndex: 0,
      commissionerIndex: 0,
      seed: 31,
      createdAt: new Date(NOW - 2 * 24 * HOUR).toISOString(),
      fplLeagueId: FPL_LEAGUE_ID,
      joined: true,
      claimedBy: { 3: 'Priya N.' },
    }),
    createImportedLeague({
      id: MISMATCH_LEAGUE_ID,
      name: MISMATCH_LEAGUE_NAME,
      idOffset: 7_000_000,
      teams: MISMATCH_TEAMS,
      myTeamIndex: 0,
      commissionerIndex: 0,
      seed: 32,
      createdAt: new Date(NOW - 3 * 24 * HOUR).toISOString(),
      fplLeagueId: 23456,
      joined: true,
      shadow: (teams) => [
        {
          key: `player_points:gw7:${teams[0]!.id}`,
          kind: 'player_points',
          gameweek: 7,
          team: teams[0]!,
          status: 'mismatch',
          ours: '6',
          theirs: '7',
          note: 'Saka: ours 6, FPL 7.',
          explanation: null,
        },
        {
          key: `points:gw7:${teams[1]!.id}`,
          kind: 'points',
          gameweek: 7,
          team: teams[1]!,
          status: 'pending',
          ours: '48',
          theirs: '45',
          note: 'FPL is still updating this gameweek.',
          explanation: null,
        },
      ],
      failing: (teams) => [
        {
          key: `points:gw5:${teams[2]!.id}`,
          kind: 'points',
          gameweek: 5,
          team: teams[2]!,
          status: 'mismatch',
          ours: '61',
          theirs: '63',
          note: null,
          explanation: null,
        },
        {
          key: `result:gw5:${teams[2]!.id}`,
          kind: 'result',
          gameweek: 5,
          team: teams[2]!,
          status: 'mismatch',
          ours: '61-62 L',
          theirs: '63-62 W',
          note: null,
          explanation: null,
        },
      ],
    }),
    createImportedLeague({
      id: PENDING_LEAGUE_ID,
      name: PENDING_LEAGUE_NAME,
      idOffset: 11_000_000,
      teams: PENDING_TEAMS,
      myTeamIndex: 0,
      commissionerIndex: 0,
      seed: 34,
      createdAt: new Date(NOW - 5 * 24 * HOUR).toISOString(),
      fplLeagueId: 45678,
      joined: true,
      claimedBy: { 1: 'Quinn A.' },
      pendingTrade: { fplTradeId: 343149, from: 2, to: 3, positions: ['DEF', 'MID', 'FWD'] },
    }),
    createImportedLeague({
      id: CLAIM_LEAGUE_ID,
      name: CLAIM_LEAGUE_NAME,
      idOffset: 8_000_000,
      teams: CLAIM_TEAMS,
      myTeamIndex: 2,
      commissionerIndex: 0,
      seed: 33,
      createdAt: new Date(NOW - 4 * 24 * HOUR).toISOString(),
      fplLeagueId: 34567,
      joined: false,
      claimedBy: { 0: 'Leo V.', 1: 'Mona G.' },
      claimCodes: { 2: CLAIM_CODE },
    }),
  ];
  return new Map(leagues.map((l) => [l.id, l]));
}

/** Every mock league by id. Stateful: mutating handlers change it. */
export const leagues = buildLeagues();
let createdSeq = 0;

/** Rebuild every league from scratch (tests). */
export function resetMockLeagues() {
  leagues.clear();
  buildLeagues().forEach((l, id) => leagues.set(id, l));
  createdSeq = 0;
}

/** A new pre-draft league with the demo user as commissioner (POST /v1/leagues). */
export function createMockLeague(body: Schemas['LeagueCreate']): PreseasonLeague {
  createdSeq++;
  const id = mockId('lg', 9_000_000 + createdSeq);
  const league = createPreseasonLeague({
    id,
    name: body.name,
    idOffset: 9_000_000 + createdSeq * 1000,
    status: 'pre_draft',
    teamCount: body.teamCount,
    teams: [[body.teamName, 'Sam P.']],
    myTeamIndex: 0,
    commissionerIndex: 0,
    review: body.settings?.trades?.review ?? 'none',
    pickTimerSeconds: body.settings?.draft?.pickTimerSeconds ?? 60,
    draftStartsAt: body.settings?.draft?.startsAt ?? null,
    waiverOrder: body.settings?.waivers?.order,
    inviteCode: `QZNEWKR4M${'23456789ABCDEFGHJKMNPQRSTVWXYZ'[createdSeq % 30]}`,
    seed: 100 + createdSeq,
    createdAt: new Date().toISOString(),
  });
  leagues.set(id, league);
  return league;
}

// ---------------------------------------------------------- back-compat

/**
 * The 9-team Sunday league. The names below predate the multi-league mocks
 * and are kept for the existing tests and screens.
 */
const sunday = () => leagues.get(LEAGUE_ID) as InSeasonLeague;

export const TEAMS = sunday().TEAMS;
export const getTeamRef = (id: string) => sunday().teamById(id);
export const ownerOf = (playerId: string) => sunday().ownerOf(playerId);
export const matchupSummaries = (gw: number) => sunday().matchupSummaries(gw);
export const matchupDetail = (gw: number, id: string) => sunday().matchupDetail(gw, id);
export const averageMatchupId = (gw: number) => sunday().averageMatchupId(gw) as string;
export const scoreStateFor = (gw: number) => sunday().scoreStateFor(gw);
export const hasScores = (gw: number) => sunday().hasScores(gw);
export const sidePoints = (side: Schemas['MatchupSide'] | Schemas['MatchupDetailSide']) =>
  sunday().sidePoints(side);
export const standings = () => sunday().standings();
export const teamDetail = (teamId: string) => sunday().teamDetail(teamId);
export const playerListItem = (p: Parameters<InSeasonLeague['playerListItem']>[0]) =>
  sunday().playerListItem(p);
export const playerDetail = (playerId: string) => sunday().playerDetail(playerId);
export const TRANSACTIONS = sunday().transactions();
export const LEAGUE_SETTINGS = sunday().settings;
export const leagueSummary = () => sunday().summary();

/**
 * What `GET /v1/imports/preview` returns: the FPL side of
 * {@link IMPORTED_LEAGUE_ID}, which every mock import opens.
 */
export function importPreview(): Schemas['ImportPreview'] {
  return {
    fplLeagueId: FPL_LEAGUE_ID,
    name: IMPORTED_LEAGUE_NAME,
    teamCount: IMPORTED_TEAMS.length,
    seats: IMPORTED_TEAMS.map(([teamName, managerName], i) => ({
      seat: i + 1,
      teamName,
      managerName,
    })),
    currentGameweek: CURRENT_GW,
    scoring: 'h2h',
    draftComplete: true,
    importable: true,
    problem: null,
    alreadyImportedLeagueId: null,
  };
}
