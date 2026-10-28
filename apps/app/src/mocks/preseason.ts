/**
 * Mock leagues before their season: a league still inviting managers
 * (`pre_draft`) and a league in the middle of its snake draft (`drafting`).
 *
 * The draft is a small state machine driven by the clock: the other managers
 * pick by rank every OTHER_PICK_MS, the demo user's pick waits for them (or
 * for autopick, or for their clock to run out). Reads call `advance()`, so
 * polling `GET /draft` shows the board filling up.
 */
import type { Schemas } from '@myfantasyprem/api-client';
import { previousSeasonsFor } from './seasons';

import { conflict, forbidden, invalid, notFound } from './errors';
import { DEMO_USER_ID, DEMO_USER_NAME, SQUAD, leagueSettings } from './league';
import {
  CURRENT_GW,
  HOUR,
  NOW,
  PLAYERS,
  SEASON,
  draftRankOf,
  fixturesFor,
  formChangeOf,
  formOf,
  getPlayerRecord,
  gameweek,
  minutesFor,
  mockId,
  mulberry32,
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

/**
 * The mock server's APP_URL: like the real one, invite links open the web
 * app's own origin, so a copied link works in this build. The Expo web dev
 * server where there is no page (Jest, native).
 */
export function mockAppURL(): string {
  const origin = typeof location === 'undefined' ? undefined : location.origin;
  return origin && origin !== 'null' ? origin : 'http://localhost:8081';
}

type TeamRef = Schemas['TeamRef'];
type Position = Schemas['Position'];
type DraftPick = Schemas['DraftPick'];

/** How long each other manager takes to pick in the mock draft room. */
export const OTHER_PICK_MS = 2_500;
const ROUNDS = 15;
/** Invite code alphabet: no 0, O, 1, I, L or U (InviteCode in the contract). */
const CODE_ALPHABET = '23456789ABCDEFGHJKMNPQRSTVWXYZ';

export interface PreseasonConfig {
  id: string;
  name: string;
  idOffset: number;
  status: 'pre_draft' | 'drafting';
  teamCount: number;
  /** Teams that already have a manager: [team name, manager name]. */
  teams: readonly (readonly [string, string])[];
  /** Index of the demo user's team, or -1 when the demo user is not in the league. */
  myTeamIndex: number;
  commissionerIndex: number;
  review: Schemas['TradeReview'];
  pickTimerSeconds: number;
  draftStartsAt: string | null;
  waiverOrder?: Schemas['WaiverOrderRule'];
  /** drafting only: picks already made when the mock starts. */
  picksMade?: number;
  inviteCode: string;
  seed: number;
  createdAt: string;
}

export type PreseasonLeague = ReturnType<typeof createPreseasonLeague>;

export function createPreseasonLeague(cfg: PreseasonConfig) {
  const off = cfg.idOffset;
  let name = cfg.name;
  let status: Schemas['LeagueStatus'] = cfg.status;
  const teams: TeamRef[] = cfg.teams.map(([teamName, managerName], i) => ({
    id: mockId('tm', off + i + 1),
    name: teamName,
    managerName: i === cfg.myTeamIndex ? DEMO_USER_NAME : managerName,
  }));
  const userOfTeam = new Map(
    teams.map((t, i) => [
      t.id,
      i === cfg.myTeamIndex ? DEMO_USER_ID : mockId('usr', off + 100 + i),
    ]),
  );
  /** Teams without a manager: filled at the draft start, taken over by whoever joins later. */
  const openSeats = new Set<string>();
  const managed = () => teams.filter((t) => !openSeats.has(t.id));
  let myTeam: TeamRef | null = teams[cfg.myTeamIndex] ?? null;
  let commissionerUserId = userOfTeam.get(teams[cfg.commissionerIndex]!.id)!;
  const isCommissioner = () => commissionerUserId === DEMO_USER_ID;
  const settings = leagueSettings({
    teamCount: cfg.teamCount,
    review: cfg.review,
    pickTimerSeconds: cfg.pickTimerSeconds,
    draftStartsAt: cfg.draftStartsAt,
    startGameweek: CURRENT_GW + 1,
    waiverOrder: cfg.waiverOrder,
  });
  const joinedAt = new Map(
    teams.map((t, i) => [t.id, new Date(Date.parse(cfg.createdAt) + i * 5 * HOUR).toISOString()]),
  );

  // --------------------------------------------------------------- invites

  const invites: Schemas['Invite'][] = [
    {
      id: mockId('inv', off + 1),
      code: cfg.inviteCode,
      url: `${mockAppURL()}/join/${cfg.inviteCode}`,
      createdAt: cfg.createdAt,
      expiresAt: new Date(NOW + 6 * 24 * HOUR).toISOString(),
      maxUses: null,
      uses: teams.length - 1,
      revoked: false,
      claimTeam: null,
    },
  ];
  let inviteSeq = 1;

  function requireCommissioner() {
    if (!isCommissioner())
      throw forbidden('not-commissioner', 'Only the commissioner can do that.');
  }

  function createInvite(maxUses?: number, expiresInHours?: number): Schemas['Invite'] {
    requireCommissioner();
    if (!hasPlace())
      throw conflict('league-full', 'League full', 'Every team in this league has a manager.');
    inviteSeq++;
    const code = Array.from({ length: 10 }, () => CODE_ALPHABET[Math.floor(rng() * 30)]).join('');
    const inv: Schemas['Invite'] = {
      id: mockId('inv', off + inviteSeq),
      code,
      url: '',
      createdAt: new Date().toISOString(),
      // Like the server: a week unless the commissioner says otherwise.
      expiresAt: new Date(Date.now() + (expiresInHours ?? 168) * HOUR).toISOString(),
      maxUses: maxUses ?? null,
      uses: 0,
      revoked: false,
      claimTeam: null,
    };
    inv.url = `${mockAppURL()}/join/${inv.code}`;
    invites.push(inv);
    return inv;
  }

  function revokeInvite(inviteId: string) {
    requireCommissioner();
    const inv = invites.find((i) => i.id === inviteId);
    if (!inv) throw notFound('Invite');
    inv.revoked = true;
  }

  /** A place to join: before the draft an empty team place, after it an open seat. */
  const hasPlace = () =>
    status === 'pre_draft' ? teams.length < settings.teamCount : openSeats.size > 0;

  const usableInvite = (code: string) =>
    invites.find(
      (i) =>
        i.code === code.toUpperCase() &&
        !i.revoked &&
        (!i.expiresAt || Date.parse(i.expiresAt) > Date.now()) &&
        (i.maxUses === null || i.uses < i.maxUses),
    );

  function invitePreview(code: string): Schemas['InvitePreview'] | undefined {
    const inv = usableInvite(code);
    if (!inv) return undefined;
    return {
      code: inv.code,
      leagueId: cfg.id,
      leagueName: name,
      season: SEASON,
      status,
      teamCount: settings.teamCount,
      memberCount: managed().length,
      commissionerName: teams.find((t) => userOfTeam.get(t.id) === commissionerUserId)!.managerName,
      expiresAt: inv.expiresAt,
      joinable: hasPlace(),
      claimTeam: null,
      claimSignIn: false,
    };
  }

  function acceptInvite(code: string, teamName: string): TeamRef {
    const inv = usableInvite(code);
    if (!inv) throw notFound('Invite');
    if (myTeam)
      throw conflict('already-member', 'Already a member', 'You are already in this league.', {
        leagueId: cfg.id,
      });
    if (!hasPlace())
      throw conflict('league-full', 'League full', 'Every team in this league has a manager.');
    // After the draft: the first open seat, renamed to the joiner's team name.
    const seat = status === 'pre_draft' ? undefined : teams.find((t) => openSeats.has(t.id));
    if (seat) {
      seat.name = teamName;
      seat.managerName = DEMO_USER_NAME;
      openSeats.delete(seat.id);
      autopick.delete(seat.id);
      userOfTeam.set(seat.id, DEMO_USER_ID);
      joinedAt.set(seat.id, new Date().toISOString());
      inv.uses++;
      myTeam = seat;
      return seat;
    }
    const team = {
      id: mockId('tm', off + teams.length + 1),
      name: teamName,
      managerName: DEMO_USER_NAME,
    };
    teams.push(team);
    userOfTeam.set(team.id, DEMO_USER_ID);
    joinedAt.set(team.id, new Date().toISOString());
    inv.uses++;
    myTeam = team;
    return team;
  }

  /** A drafted team whose manager is gone stays, as an open seat. */
  function openSeat(team: TeamRef) {
    team.managerName = 'Open seat';
    openSeats.add(team.id);
    autopick.add(team.id);
    userOfTeam.delete(team.id);
  }

  function leave() {
    if (!myTeam) throw forbidden('not-a-member', 'You are not in this league.');
    if (isCommissioner())
      throw conflict(
        'commissioner-cannot-leave',
        'Commissioner cannot leave',
        'Hand the commissioner role to someone else first.',
      );
    if (status === 'pre_draft') teams.splice(teams.indexOf(myTeam), 1);
    else openSeat(myTeam);
    myTeam = null;
  }

  function removeMember(userId: string) {
    requireCommissioner();
    if (userId === DEMO_USER_ID)
      throw conflict(
        'commissioner-cannot-leave',
        'Commissioner cannot leave',
        'Hand the commissioner role to someone else first.',
      );
    const team = teams.find((t) => userOfTeam.get(t.id) === userId);
    if (!team) throw notFound('Member');
    if (status === 'pre_draft') teams.splice(teams.indexOf(team), 1);
    else openSeat(team);
  }

  function transferCommissioner(userId: string) {
    requireCommissioner();
    if (userId === commissionerUserId) return;
    if (!teams.some((t) => userOfTeam.get(t.id) === userId)) throw notFound('Member');
    commissionerUserId = userId;
  }

  // ----------------------------------------------------------------- draft

  const draftId = mockId('drf', off + 1);
  let order: TeamRef[] = status === 'drafting' ? [...teams] : [];
  const picks: DraftPick[] = [];
  const autopick = new Set<string>();
  /** Sam's "autopick when my clock runs out" is off. */
  let stayManual = false;
  let queue: string[] = [];
  /** Missed-clock autopicks, newest first: the league's activity feed while drafting. */
  const feed: Schemas['DraftAutopickTransaction'][] = [];
  // Sam has been on the clock for 20 s when the mocks load (or reset).
  let clockStartedAt = Date.now() - 20_000;
  let pausedAt: number | null = null;
  let completedAt: string | null = null;
  const rng = mulberry32(cfg.seed);

  const totalPicks = () => order.length * ROUNDS;
  function slotFor(overall: number) {
    const n = order.length;
    const round = Math.ceil(overall / n);
    const idx = (overall - 1) % n;
    return { round, pickInRound: idx + 1, team: order[round % 2 === 1 ? idx : n - 1 - idx]! };
  }

  const taken = () => new Set(picks.map((p) => p.player.id));
  function counts(teamId: string) {
    const c: Record<Position, number> = { GKP: 0, DEF: 0, MID: 0, FWD: 0 };
    picks.filter((p) => p.team.id === teamId).forEach((p) => c[p.player.position]++);
    return c;
  }
  const needs = (teamId: string, pos: Position) => counts(teamId)[pos] < SQUAD[pos];
  const byRank = [...PLAYERS].sort((a, b) => draftRankOf(a.id) - draftRankOf(b.id));

  function choose(
    teamId: string,
    fromQueue: boolean,
  ): { player: PlayerRecord; method: Schemas['DraftPickMethod'] } {
    const gone = taken();
    if (fromQueue) {
      const q = queue
        .map((id) => getPlayerRecord(id)!)
        .find((p) => !gone.has(p.id) && needs(teamId, p.position));
      if (q) return { player: q, method: 'queue' };
    }
    const options = byRank.filter((p) => !gone.has(p.id) && needs(teamId, p.position)).slice(0, 3);
    return { player: options[Math.floor(rng() * options.length)]!, method: 'rank' };
  }

  function record(
    team: TeamRef,
    player: PlayerRecord,
    method: Schemas['DraftPickMethod'],
    at: number,
    missedClock = false,
  ) {
    const overall = picks.length + 1;
    const { round, pickInRound } = slotFor(overall);
    const pk: DraftPick = {
      id: mockId('pk', off + overall),
      overall,
      round,
      pickInRound,
      team,
      player: toPlayerRef(player),
      method,
      pickedAt: new Date(at).toISOString(),
      missedClock,
    };
    picks.push(pk);
    if (missedClock) {
      // As the server: a missed clock is never silent.
      feed.unshift({
        id: mockId('txn', off + 500 + overall),
        kind: 'draft_autopick',
        occurredAt: pk.pickedAt,
        gameweek: settings.startGameweek,
        team,
        player: pk.player,
        overall,
        round,
        method,
      });
    }
    if (picks.length === totalPicks()) {
      status = 'in_season';
      completedAt = pk.pickedAt;
    }
    return pk;
  }

  // Picks already made before the mock starts.
  if (status === 'drafting') {
    for (let i = 0; i < (cfg.picksMade ?? 0); i++) {
      const { team } = slotFor(picks.length + 1);
      const { player } = choose(team.id, false);
      // Every seventh manager let the clock run out.
      const missed = i % 7 === 3;
      record(team, player, missed ? 'rank' : 'manual', NOW - (cfg.picksMade! - i) * 45_000, missed);
    }
  }

  /** Lets the other managers (and autopick) catch up with the clock. */
  function advance(now = Date.now()) {
    if (status !== 'drafting' || pausedAt !== null) return;
    while (picks.length < totalPicks()) {
      const { team } = slotFor(picks.length + 1);
      const mine = team.id === myTeam?.id;
      const deadline = clockStartedAt + settings.draft.pickTimerSeconds * 1000;
      if (openSeats.has(team.id)) {
        // An open seat autopicks without waiting for its clock.
        record(team, choose(team.id, false).player, 'rank', clockStartedAt);
      } else if (mine && autopick.has(team.id)) {
        const c = choose(team.id, true);
        record(team, c.player, c.method, now);
        clockStartedAt = now;
      } else if (mine && now >= deadline) {
        // Missed pick: autopick picks, and stays on unless Sam turned that off.
        if (!stayManual) autopick.add(team.id);
        const c = choose(team.id, true);
        record(team, c.player, c.method, deadline, true);
        clockStartedAt = deadline;
      } else if (!mine && now >= clockStartedAt + OTHER_PICK_MS) {
        const c = choose(team.id, false);
        clockStartedAt += OTHER_PICK_MS;
        record(team, c.player, autopick.has(team.id) ? 'rank' : 'manual', clockStartedAt);
      } else {
        return;
      }
    }
  }

  function draft(): Schemas['Draft'] {
    advance();
    const onClock =
      status === 'drafting' && picks.length < totalPicks() ? slotFor(picks.length + 1) : null;
    return {
      id: draftId,
      status:
        status === 'pre_draft'
          ? 'scheduled'
          : status === 'drafting'
            ? pausedAt !== null
              ? 'paused'
              : 'in_progress'
            : 'complete',
      type: 'snake',
      rounds: ROUNDS,
      pickTimerSeconds: settings.draft.pickTimerSeconds,
      startsAt: settings.draft.startsAt,
      order: order.map((team, i) => ({ slot: i + 1, team, autopick: autopick.has(team.id) })),
      picks: [...picks],
      currentPick: onClock
        ? {
            overall: picks.length + 1,
            round: onClock.round,
            pickInRound: onClock.pickInRound,
            team: onClock.team,
            startedAt: new Date(clockStartedAt).toISOString(),
            deadlineAt:
              pausedAt !== null
                ? null
                : new Date(
                    onClock.team.id === myTeam?.id
                      ? clockStartedAt + settings.draft.pickTimerSeconds * 1000
                      : clockStartedAt + OTHER_PICK_MS,
                  ).toISOString(),
          }
        : null,
      completedAt,
    };
  }

  function updateDraft(body: Schemas['DraftUpdate']) {
    requireCommissioner();
    if (status !== 'pre_draft')
      throw conflict('draft-started', 'Draft started', 'The draft has started.');
    if (body.order && body.randomizeOrder)
      throw invalid('invalid-request', 'Send order or randomizeOrder, not both.');
    if (body.order) {
      const ids = new Set(teams.map((t) => t.id));
      if (body.order.length !== teams.length || !body.order.every((id) => ids.has(id)))
        throw invalid('invalid-request', 'order must list every team once.');
      order = body.order.map((id) => teams.find((t) => t.id === id)!);
    }
    if (body.randomizeOrder) {
      order = [...teams]
        .map((t) => ({ t, k: rng() }))
        .sort((a, b) => a.k - b.k)
        .map((x) => x.t);
    }
    if (body.pickTimerSeconds !== undefined)
      settings.draft.pickTimerSeconds = body.pickTimerSeconds;
    if (body.startsAt !== undefined) settings.draft.startsAt = body.startsAt;
    return draft();
  }

  function startDraft() {
    requireCommissioner();
    if (status !== 'pre_draft')
      throw conflict('draft-started', 'Draft started', 'The draft has already started.');
    // As the server: at least 2 teams with a manager; empty places become open seats.
    if (managed().length < 2)
      throw conflict(
        'not-enough-managers',
        'Not enough managers',
        `${managed().length} of ${settings.teamCount} teams have a manager: at least 2 teams need a manager before the draft can start.`,
      );
    let seatNo = 0;
    while (teams.length < settings.teamCount) {
      const seat: TeamRef = {
        id: mockId('tm', off + teams.length + 1),
        name: `Open seat ${++seatNo}`,
        managerName: 'Open seat',
      };
      teams.push(seat);
      openSeats.add(seat.id);
      autopick.add(seat.id);
    }
    order = [...order, ...teams.filter((t) => !order.includes(t))];
    status = 'drafting';
    clockStartedAt = Date.now();
    return draft();
  }

  function pauseDraft() {
    requireCommissioner();
    advance();
    if (status !== 'drafting' || pausedAt !== null)
      throw conflict('draft-not-live', 'Draft not live', 'Only a running draft can be paused.');
    pausedAt = Date.now();
    return draft();
  }

  function resumeDraft() {
    requireCommissioner();
    if (status !== 'drafting' || pausedAt === null)
      throw conflict('draft-not-paused', 'Draft not paused', 'The draft is not paused.');
    pausedAt = null;
    clockStartedAt = Date.now();
    return draft();
  }

  function makePick(playerId: string): DraftPick {
    advance();
    if (!myTeam) throw forbidden('not-a-member', 'You are not in this league.');
    if (status !== 'drafting' || pausedAt !== null)
      throw conflict('draft-not-live', 'Draft not live', 'The draft is not running.');
    const { team } = slotFor(picks.length + 1);
    if (team.id !== myTeam.id)
      throw conflict('not-your-turn', 'Not your turn', `${team.name} is on the clock.`);
    const player = getPlayerRecord(playerId);
    if (!player) throw notFound('Player');
    if (taken().has(playerId))
      throw conflict('player-taken', 'Player taken', `${player.displayName} has been drafted.`);
    if (!needs(team.id, player.position))
      throw invalid(
        'position-full',
        `Your squad already has ${SQUAD[player.position]} ${player.position}.`,
        [{ code: 'position_full', message: `No ${player.position} places left.`, playerId }],
      );
    const pk = record(team, player, 'manual', Date.now());
    clockStartedAt = Date.now();
    queue = queue.filter((id) => id !== playerId);
    return pk;
  }

  function queueView(): Schemas['DraftQueue'] {
    if (!myTeam) throw forbidden('not-a-member', 'You are not in this league.');
    advance();
    const gone = taken();
    queue = status === 'in_season' ? [] : queue.filter((id) => !gone.has(id));
    return {
      team: myTeam,
      autopick: autopick.has(myTeam.id),
      autopickOnTimeout: !stayManual,
      players: queue.map((id) => ({
        player: toPlayerRef(getPlayerRecord(id)!),
        draftRank: draftRankOf(id),
      })),
    };
  }

  function setQueue(ids: string[]) {
    if (!myTeam) throw forbidden('not-a-member', 'You are not in this league.');
    const unknown = ids.filter((id) => !getPlayerRecord(id));
    if (unknown.length)
      throw invalid(
        'invalid-request',
        'Unknown players in the queue.',
        unknown.map((id) => ({ code: 'unknown_player', message: 'No such player.', playerId: id })),
      );
    queue = [...ids];
    return queueView();
  }

  function setAutopick(body: Schemas['DraftAutopickUpdate']) {
    if (!myTeam) throw forbidden('not-a-member', 'You are not in this league.');
    if (body.onTimeout !== undefined) stayManual = !body.onTimeout;
    if (body.enabled === true) autopick.add(myTeam.id);
    if (body.enabled === false) autopick.delete(myTeam.id);
    return queueView();
  }

  // ------------------------------------------------------------- reads

  const ownerOf = (playerId: string): TeamRef | null =>
    picks.find((p) => p.player.id === playerId)?.team ?? null;

  function playerListItem(p: PlayerRecord): Schemas['PlayerListItem'] {
    const totals = seasonTotals(p.id);
    const o = ownerOf(p.id);
    return {
      player: toPlayerRef(p),
      owner: o,
      rosterStatus: o ? 'rostered' : 'free_agent',
      waiverClearsAt: null,
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
    const item = playerListItem(p);
    return {
      player: item.player,
      season: SEASON,
      owner: item.owner,
      rosterStatus: item.rosterStatus,
      waiverClearsAt: null,
      totalPoints: item.totalPoints,
      form: item.form,
      draftRank: item.draftRank,
      minutes: item.minutes,
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

  const emptyRecord = (rank: number): Schemas['Record'] => ({
    rank,
    played: 0,
    wins: 0,
    draws: 0,
    losses: 0,
    pointsFor: 0,
    pointsAgainst: 0,
    leaguePoints: 0,
  });

  function standings(): Schemas['StandingRow'][] {
    return teams.map((team, i) => ({ ...emptyRecord(i + 1), team, form: [] }));
  }

  function teamDetail(teamId: string): Schemas['TeamDetail'] | undefined {
    const i = teams.findIndex((t) => t.id === teamId);
    if (i < 0) return undefined;
    advance();
    return {
      team: teams[i]!,
      record: emptyRecord(i + 1),
      roster: picks
        .filter((p) => p.team.id === teamId)
        .map((p, k) => ({
          slot: k + 1,
          player: p.player,
          totalPoints: seasonTotals(p.player.id).points,
          gameweekPoints: 0,
          acquiredVia: 'draft' as const,
          nextFixtures: [],
        })),
      results: [],
      nextOpponent: null,
    };
  }

  function summary(): Schemas['LeagueSummary'] {
    advance();
    return {
      id: cfg.id,
      name,
      season: SEASON,
      status,
      source: 'native',
      teamCount: settings.teamCount,
      memberCount: managed().length,
      fplLeagueId: null,
    };
  }

  const viewer = (): Schemas['LeagueViewer'] => ({
    role: isCommissioner() ? 'commissioner' : 'member',
    team: myTeam,
  });

  function home(): Schemas['LeagueHome'] {
    return {
      league: summary(),
      settings,
      viewer: viewer(),
      // Drafted: the league starts at its first gameweek (ADR 0010).
      currentGameweek: gameweek(
        status === 'in_season' ? Math.max(CURRENT_GW, settings.startGameweek) : CURRENT_GW,
      ),
      standings: standings(),
      matchups: [],
    };
  }

  function members(): Schemas['Member'][] {
    return managed().map((t, i) => ({
      user: { id: userOfTeam.get(t.id)!, displayName: t.managerName },
      team: t,
      role: (userOfTeam.get(t.id) === commissionerUserId
        ? 'commissioner'
        : 'member') as Schemas['LeagueRole'],
      joinedAt: joinedAt.get(t.id)!,
    }));
  }

  function myLeague(): Schemas['MyLeague'] | undefined {
    if (!myTeam) return undefined;
    return {
      league: summary(),
      team: myTeam,
      role: viewer().role,
      record: null,
      currentMatchup: null,
      draftStartsAt: settings.draft.startsAt,
    };
  }

  function rename(newName: string) {
    requireCommissioner();
    name = newName;
  }

  return {
    id: cfg.id,
    get name() {
      return name;
    },
    get status() {
      return status;
    },
    get myTeam() {
      return myTeam;
    },
    teams: teams as readonly TeamRef[],
    settings,
    isCommissioner,
    summary,
    home,
    viewer,
    members,
    myLeague,
    ownerOf,
    teamById: (id: string) => teams.find((t) => t.id === id),
    teamDetail,
    standings,
    playerListItem,
    playerDetail,
    invites: () => {
      requireCommissioner();
      return [...invites].reverse();
    },
    createInvite,
    revokeInvite,
    invitePreview,
    acceptInvite,
    leave,
    removeMember,
    transferCommissioner,
    rename,
    draft,
    /** Lets the draft catch up to `now` (tests: finish it at once). */
    advance,
    updateDraft,
    startDraft,
    pauseDraft,
    resumeDraft,
    makePick,
    queueView,
    setQueue,
    setAutopick,
    transactions: () => feed as readonly Schemas['Transaction'][],
  };
}
