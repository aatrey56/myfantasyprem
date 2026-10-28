/** @jest-environment node */
/**
 * The native-league mock API (accounts, leagues, invites, draft, lineups,
 * waivers, trades) through the generated client. Proves every operation in
 * api/openapi.yaml has a mock handler, that ids follow the public format,
 * and that the stateful flows the screens rely on behave like the contract.
 */
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { createApiClient, unwrap, ApiError, type Schemas } from '@myfantasyprem/api-client';
import { getResponse } from 'msw';

import * as data from '@/mocks/data';
import { handlers, resetMocks } from '@/mocks/handlers';

const mockFetch: typeof fetch = async (input, init) => {
  const request = new Request(input, init);
  const response = await getResponse(handlers, request);
  if (!response) throw new Error(`No mock handler for ${request.method} ${request.url}`);
  return response;
};
const api = createApiClient({ baseUrl: 'http://api.test', fetch: mockFetch });

const OFFICE = data.OFFICE_LEAGUE_ID;
const office = () => data.leagues.get(OFFICE) as Extract<data.MockLeague, { listTrades: unknown }>;
const path = (leagueId: string) => ({ params: { path: { leagueId } } });

/** Every string under a key named `id` or ending in `Id` looks like a public id. */
function expectPublicIds(value: unknown, where = '$') {
  if (Array.isArray(value)) value.forEach((v, i) => expectPublicIds(v, `${where}[${i}]`));
  else if (value && typeof value === 'object') {
    for (const [k, v] of Object.entries(value)) {
      if ((k === 'id' || /[a-z]Id$/.test(k)) && typeof v === 'string' && k !== 'requestId') {
        expect({ where: `${where}.${k}`, v }).toEqual({
          where: `${where}.${k}`,
          v: expect.stringMatching(
            /^(usr|lg|tm|mu|plr|fx|txn|drf|pk|wv|trd|inv|imp)_[0-9a-hjkmnp-tv-z]{26}$/,
          ),
        });
      }
      expectPublicIds(v, `${where}.${k}`);
    }
  }
}

beforeEach(() => resetMocks());

describe('spec coverage', () => {
  it('has a mock handler for every operation in api/openapi.yaml', async () => {
    const spec = readFileSync(join(__dirname, '../../../../api/openapi.yaml'), 'utf8');
    const ops: [string, string][] = [];
    let current = '';
    for (const line of spec.split('\n')) {
      if (line.startsWith('components:')) break;
      const p = /^ {2}(\/\S+):$/.exec(line);
      if (p) current = p[1]!;
      const m = /^ {4}(get|post|put|patch|delete):$/.exec(line);
      if (m && current) ops.push([m[1]!.toUpperCase(), current]);
    }
    expect(ops.length).toBeGreaterThan(60);
    const sample: Record<string, string> = {
      leagueId: OFFICE,
      teamId: office().myTeam.id,
      gw: '7',
      matchupId: office().matchupSummaries(7)[0]!.id,
      playerId: data.PLAYERS[0]!.id,
      importId: 'imp_01k7m2xq8y0000000000000001',
      userId: data.DEMO_USER_ID,
      inviteId: 'inv_01k7m2xq800000000000000401',
      code: data.PUB_INVITE_CODE,
      claimId: 'wv_01k7m2xq800000000000000201',
      tradeId: 'trd_01k7m2xq800000000000000301',
    };
    for (const [method, p] of ops) {
      const url =
        'http://api.test' + p.replace(/\{(\w+)\}/g, (_, name: string) => sample[name] ?? 'x');
      const res = await getResponse(handlers, new Request(url, { method }));
      expect({ op: `${method} ${p}`, handled: Boolean(res) }).toEqual({
        op: `${method} ${p}`,
        handled: true,
      });
    }
  });
});

describe('accounts', () => {
  it('signs in, reads and updates the profile', async () => {
    const session = unwrap(
      await api.POST('/v1/auth/email/verify', { body: { token: 'a'.repeat(32) } }),
    );
    expect(session.data).toMatchObject({ tokenType: 'Bearer', user: { id: data.DEMO_USER_ID } });
    const expired = await api.POST('/v1/auth/email/verify', {
      body: { token: 'expired-token-0000' },
    });
    expect(expired.response.status).toBe(401);
    const started = await api.POST('/v1/auth/email', { body: { email: 'sam@example.com' } });
    expect(started.response.status).toBe(202);
    const updated = unwrap(await api.PATCH('/v1/me', { body: { displayName: 'Sam Parker' } }));
    expect(updated.data.displayName).toBe('Sam Parker');
    const bad = await api.PATCH('/v1/me', { body: { avatarColor: 'blue' } });
    expect(bad.response.status).toBe(422);
    expect((bad.error as Schemas['Problem']).errors?.[0]?.field).toBe('/avatarColor');
  });

  it("lists the user's leagues with a live matchup where the season has started", async () => {
    const mine = unwrap(await api.GET('/v1/me/leagues')).data;
    const byId = new Map(mine.map((m) => [m.league.id, m]));
    expect([...byId.keys()].sort()).toEqual(
      [
        data.LEAGUE_ID,
        OFFICE,
        data.DRAFT_LEAGUE_ID,
        data.PREDRAFT_LEAGUE_ID,
        data.DUO_LEAGUE_ID,
        data.LATE_LEAGUE_ID,
        data.IMPORTED_LEAGUE_ID,
        data.MISMATCH_LEAGUE_ID,
        data.PENDING_LEAGUE_ID,
      ].sort(),
    );
    expect(byId.get(data.IMPORTED_LEAGUE_ID)!.league).toMatchObject({
      source: 'fpl_import',
      fplLeagueId: data.FPL_LEAGUE_ID,
    });
    expect(byId.get(OFFICE)).toMatchObject({
      role: 'commissioner',
      league: { status: 'in_season' },
    });
    expect(byId.get(OFFICE)!.currentMatchup?.state).toBe('live');
    expect(byId.get(data.DRAFT_LEAGUE_ID)!.league.status).toBe('drafting');
    expect(byId.get(data.PREDRAFT_LEAGUE_ID)!.currentMatchup).toBeNull();
    expect(byId.get(data.LATE_LEAGUE_ID)!.currentMatchup).toMatchObject({
      gameweek: data.CURRENT_GW + 1,
      state: 'upcoming',
    });
    expectPublicIds(mine);
  });

  it('serves a league drafted mid-season from its first gameweek only', async () => {
    const late = { params: { path: { leagueId: data.LATE_LEAGUE_ID } } };
    const first = data.CURRENT_GW + 1;
    const home = unwrap(await api.GET('/v1/leagues/{leagueId}', late)).data;
    expect(home.settings.startGameweek).toBe(first);
    expect(home.currentGameweek.number).toBe(first);
    expect(home.matchups).toHaveLength(3);
    expect(home.matchups.every((m) => m.gameweek === first && m.state === 'upcoming')).toBe(true);
    expect(home.standings.every((r) => r.played === 0)).toBe(true);
    const schedule = unwrap(await api.GET('/v1/leagues/{leagueId}/schedule', late)).data;
    expect(schedule[0]!.gameweek).toBe(first);
    const feed = unwrap(await api.GET('/v1/leagues/{leagueId}/transactions', late)).data;
    expect(feed).toEqual([]);
    const other = home.standings.find((r) => r.team.id !== home.viewer.team?.id)!.team.id;
    const before = await api.GET('/v1/leagues/{leagueId}/teams/{teamId}/lineup', {
      params: {
        path: { leagueId: data.LATE_LEAGUE_ID, teamId: other },
        query: { gameweek: data.CURRENT_GW },
      },
    });
    expect(before.response.status).toBe(404);
  });
});

describe('leagues and invites', () => {
  it('serves a 10-team league with no average opponent and FPL-standard settings', async () => {
    const home = unwrap(await api.GET('/v1/leagues/{leagueId}', path(OFFICE))).data;
    expect(home.standings).toHaveLength(10);
    expect(home.matchups).toHaveLength(5);
    expect(home.matchups.every((m) => m.home.kind === 'team' && m.away.kind === 'team')).toBe(true);
    expect(home.settings).toMatchObject({
      teamCount: 10,
      scoring: 'fpl_standard',
      roster: { GKP: 2, DEF: 5, MID: 5, FWD: 3 },
      lineup: { minStarters: { GKP: 1, DEF: 3, MID: 2, FWD: 1 } },
      trades: { review: 'league_vote' },
    });
    expect(home.viewer.role).toBe('commissioner');
    expectPublicIds(home);
  });

  it('creates a league, invites, and joins another league by code', async () => {
    const createdLeague = await api.POST('/v1/leagues', {
      body: { name: 'Work Five', teamCount: 6, teamName: 'My Team' },
    });
    expect(createdLeague.response.status).toBe(201);
    const id = createdLeague.data!.data.league.id;
    expect(createdLeague.response.headers.get('Location')).toBe(`/v1/leagues/${id}`);
    const invite = unwrap(
      await api.POST('/v1/leagues/{leagueId}/invites', { ...path(id), body: {} }),
    );
    expect(invite.data.code).toMatch(/^[2-9A-HJKMNP-TV-Z]{10}$/);

    const notMember = await api.GET('/v1/leagues/{leagueId}', path(data.PUB_LEAGUE_ID));
    expect(notMember.response.status).toBe(403);
    const preview = unwrap(
      await api.GET('/v1/invites/{code}', { params: { path: { code: data.PUB_INVITE_CODE } } }),
    );
    expect(preview.data).toMatchObject({
      leagueId: data.PUB_LEAGUE_ID,
      leagueName: 'The Pub League',
      joinable: true,
    });
    const joined = await api.POST('/v1/invites/{code}/accept', {
      params: { path: { code: data.PUB_INVITE_CODE } },
      body: { teamName: 'Late Arrivals' },
    });
    expect(joined.response.status).toBe(201);
    const again = await api.POST('/v1/invites/{code}/accept', {
      params: { path: { code: data.PUB_INVITE_CODE } },
      body: { teamName: 'Twice' },
    });
    expect(again.response.status).toBe(409);
    // The problem names the league so the app can open it directly.
    expect(again.error).toMatchObject({ leagueId: data.PUB_LEAGUE_ID });
    expect((again.error as Schemas['Problem']).type).toMatch(/already-member$/);
    const members = unwrap(
      await api.GET('/v1/leagues/{leagueId}/members', path(data.PUB_LEAGUE_ID)),
    );
    expect(members.data.some((m) => m.user.id === data.DEMO_USER_ID)).toBe(true);
  });

  it('locks draft settings once a league has drafted', async () => {
    const locked = await api.PATCH('/v1/leagues/{leagueId}', {
      ...path(OFFICE),
      body: { settings: { teamCount: 12 } },
    });
    expect(locked.response.status).toBe(409);
    const renamed = unwrap(
      await api.PATCH('/v1/leagues/{leagueId}', {
        ...path(OFFICE),
        body: { name: 'Office Champions' },
      }),
    );
    expect(renamed.data.league.name).toBe('Office Champions');
    const notCommish = await api.PATCH('/v1/leagues/{leagueId}', {
      ...path(data.LEAGUE_ID),
      body: { name: 'Mine' },
    });
    expect(notCommish.response.status).toBe(403);
  });
});

describe('draft room', () => {
  const draftPath = path(data.DRAFT_LEAGUE_ID);

  it('shows a live snake draft with Sam on the clock', async () => {
    const draft = unwrap(await api.GET('/v1/leagues/{leagueId}/draft', draftPath)).data;
    expect(draft.status).toBe('in_progress');
    expect(draft.order).toHaveLength(8);
    expect(draft.picks.length).toBeGreaterThanOrEqual(42);
    draft.picks.forEach((p, i) => {
      expect(p.overall).toBe(i + 1);
      const round = Math.ceil(p.overall / 8);
      const idx = (p.overall - 1) % 8;
      const slot = round % 2 === 1 ? idx : 7 - idx;
      expect(p.team.id).toBe(draft.order[slot]!.team.id);
    });
    expect(new Set(draft.picks.map((p) => p.player.id)).size).toBe(draft.picks.length);
    expect(draft.currentPick?.team.managerName).toBe('Sam P.');
    expectPublicIds(draft);
  });

  it('refuses taken players and full positions, then accepts a legal pick', async () => {
    const draft = unwrap(await api.GET('/v1/leagues/{leagueId}/draft', draftPath)).data;
    const taken = await api.POST('/v1/leagues/{leagueId}/draft/picks', {
      ...draftPath,
      body: { playerId: draft.picks[0]!.player.id },
    });
    expect(taken.response.status).toBe(409);
    expect((taken.error as Schemas['Problem']).type).toMatch(/player-taken$/);

    const free = unwrap(
      await api.GET('/v1/leagues/{leagueId}/players', {
        params: {
          path: { leagueId: data.DRAFT_LEAGUE_ID },
          query: { status: 'available', sort: 'draft_rank', limit: 100 },
        },
      }),
    ).data;
    const mineSoFar = draft.picks.filter((p) => p.team.managerName === 'Sam P.');
    const full = (['GKP', 'DEF', 'MID', 'FWD'] as const).find(
      (pos) =>
        mineSoFar.filter((p) => p.player.position === pos).length >=
        { GKP: 2, DEF: 5, MID: 5, FWD: 3 }[pos],
    );
    if (full) {
      const res = await api.POST('/v1/leagues/{leagueId}/draft/picks', {
        ...draftPath,
        body: { playerId: free.find((f) => f.player.position === full)!.player.id },
      });
      expect(res.response.status).toBe(422);
    }
    const needed = free.find((f) => f.player.position !== full)!;
    const pick = await api.POST('/v1/leagues/{leagueId}/draft/picks', {
      ...draftPath,
      body: { playerId: needed.player.id },
    });
    expect(pick.response.status).toBe(201);
    const after = unwrap(await api.GET('/v1/leagues/{leagueId}/draft', draftPath)).data;
    expect(after.currentPick?.team.managerName).not.toBe('Sam P.');
    const notYourTurn = await api.POST('/v1/leagues/{leagueId}/draft/picks', {
      ...draftPath,
      body: { playerId: free[5]!.player.id },
    });
    expect(notYourTurn.response.status).toBe(409);
  });

  it('keeps a private queue and an autopick switch', async () => {
    const ids = data.PLAYERS.slice(-3).map((p) => p.id);
    const q = unwrap(
      await api.PUT('/v1/leagues/{leagueId}/draft/queue', {
        ...draftPath,
        body: { playerIds: ids },
      }),
    ).data;
    expect(q.players.map((p) => p.player.id)).toEqual(ids);
    const auto = unwrap(
      await api.PUT('/v1/leagues/{leagueId}/draft/autopick', {
        ...draftPath,
        body: { enabled: true },
      }),
    ).data;
    expect(auto.autopick).toBe(true);
  });
});

describe('starting a draft early', () => {
  it('refuses with fewer than 2 managers: not-enough-managers', async () => {
    const created = await api.POST('/v1/leagues', {
      body: { name: 'Solo', teamCount: 2, teamName: 'Just Me' },
    });
    const id = created.data!.data.league.id;
    const res = await api.POST('/v1/leagues/{leagueId}/draft/start', path(id));
    expect(res.response.status).toBe(409);
    expect((res.error as Schemas['Problem']).type).toMatch(/not-enough-managers$/);
  });

  it('starts with 2 of 6 managers: open seats autopick, a later joiner takes one over', async () => {
    const duo = path(data.DUO_LEAGUE_ID);
    const before = unwrap(await api.GET('/v1/leagues/{leagueId}', duo)).data;
    expect(before.league).toMatchObject({ teamCount: 6, memberCount: 2, status: 'pre_draft' });

    const started = unwrap(await api.POST('/v1/leagues/{leagueId}/draft/start', duo)).data;
    expect(started.order).toHaveLength(6);
    const seats = started.order.filter((s) => s.team.managerName === 'Open seat');
    expect(seats.map((s) => s.team.name)).toEqual([
      'Open seat 1',
      'Open seat 2',
      'Open seat 3',
      'Open seat 4',
    ]);
    expect(seats.every((s) => s.autopick)).toBe(true);

    // The two managers' turns come first; open seats then pick without waiting.
    (
      data.leagues.get(data.DUO_LEAGUE_ID) as Extract<data.MockLeague, { advance: unknown }>
    ).advance(Date.now() + 5 * 60_000);
    const after = unwrap(await api.GET('/v1/leagues/{leagueId}', duo)).data;
    expect(after.league).toMatchObject({ memberCount: 2, status: 'drafting' });
    const members = unwrap(await api.GET('/v1/leagues/{leagueId}/members', duo)).data;
    expect(members).toHaveLength(2);
    const draft = unwrap(await api.GET('/v1/leagues/{leagueId}/draft', duo)).data;
    const openIds = new Set(seats.map((s) => s.team.id));
    const openPicks = draft.picks.filter((p) => openIds.has(p.team.id));
    expect(openPicks.length).toBeGreaterThan(0);
    expect(openPicks.every((p) => p.method === 'rank')).toBe(true);
  });
});

describe('lineups', () => {
  it('hides other lineups before the deadline and validates the formation', async () => {
    const league = office();
    const mine = league.myTeam.id;
    const other = league.TEAMS[1]!.id;
    const lineupPath = (teamId: string, gameweek?: number) => ({
      params: { path: { leagueId: OFFICE, teamId }, query: gameweek ? { gameweek } : {} },
    });
    const lineup = unwrap(
      await api.GET('/v1/leagues/{leagueId}/teams/{teamId}/lineup', lineupPath(mine)),
    ).data;
    expect(lineup).toMatchObject({ gameweek: 8, locked: false });
    expect(lineup.starters).toHaveLength(11);
    expect(lineup.bench[0]!.player.position).toBe('GKP');
    const hidden = await api.GET('/v1/leagues/{leagueId}/teams/{teamId}/lineup', lineupPath(other));
    expect(hidden.response.status).toBe(403);
    const past = unwrap(
      await api.GET('/v1/leagues/{leagueId}/teams/{teamId}/lineup', lineupPath(other, 7)),
    ).data;
    expect(past.locked).toBe(true);

    const ids = (e: Schemas['LineupEntry'][]) => e.map((x) => x.player.id);
    const starters = ids(lineup.starters);
    const bench = ids(lineup.bench);
    // Swap the starting keeper and the bench keeper: still legal.
    const swapped = unwrap(
      await api.PUT('/v1/leagues/{leagueId}/teams/{teamId}/lineup', {
        params: { path: { leagueId: OFFICE, teamId: mine } },
        body: {
          gameweek: 8,
          starters: [bench[0]!, ...starters.slice(1)],
          bench: [starters[0]!, ...bench.slice(1)],
        },
      }),
    ).data;
    expect(swapped.starters[0]!.player.id).toBe(bench[0]);
    // An outfield player on the first bench slot breaks two rules.
    const illegal = await api.PUT('/v1/leagues/{leagueId}/teams/{teamId}/lineup', {
      params: { path: { leagueId: OFFICE, teamId: mine } },
      body: {
        gameweek: 8,
        starters: [...starters.slice(0, 10), bench[0]!],
        bench: [starters[10]!, ...bench.slice(1)],
      },
    });
    expect(illegal.response.status).toBe(422);
    const codes = (illegal.error as Schemas['Problem']).errors!.map((e) => e.code);
    expect(codes).toContain('bench_goalkeeper_not_first');
    const locked = await api.PUT('/v1/leagues/{leagueId}/teams/{teamId}/lineup', {
      params: { path: { leagueId: OFFICE, teamId: mine } },
      body: { gameweek: 7, starters, bench },
    });
    expect(locked.response.status).toBe(409);
    expect((locked.error as Schemas['Problem']).type).toMatch(/lineup-locked$/);
  });

  it('carries points and auto-subs once the gameweek has started, matching the matchup', async () => {
    const league = office();
    const mine = league.myTeam.id;
    const live = unwrap(
      await api.GET('/v1/leagues/{leagueId}/teams/{teamId}/lineup', {
        params: { path: { leagueId: OFFICE, teamId: mine }, query: { gameweek: 7 } },
      }),
    ).data;
    expect(live.scoreState).toBe('live');
    const muId = league
      .matchupSummaries(7)
      .find((m) => [m.home, m.away].some((s) => s.kind === 'team' && s.team.id === mine))!.id;
    const detail = league.matchupDetail(7, muId)!;
    const side = [detail.home, detail.away].find(
      (s) => s.kind === 'team' && s.team.id === mine,
    ) as Schemas['TeamLineup'];
    expect(live.totalPoints).toBe(side.totalPoints);
    expect(live.benchPoints).toBe(side.benchPoints);
    for (const e of [...live.starters, ...live.bench]) {
      const slot = [...side.starters, ...side.bench].find((s) => s.player.id === e.player.id)!;
      expect(e.points).toBe(slot.points);
      expect(e.autoSub).toBe(slot.autoSub);
    }
    // Before kickoff there are no points.
    const next = unwrap(
      await api.GET('/v1/leagues/{leagueId}/teams/{teamId}/lineup', {
        params: { path: { leagueId: OFFICE, teamId: mine } },
      }),
    ).data;
    expect(next.totalPoints).toBeUndefined();
    expect(next.starters[0]!.points).toBeUndefined();
  });
});

describe('waivers and free agents', () => {
  it('claims, reorders and cancels, and adds a free agent', async () => {
    const lp = path(OFFICE);
    const state = unwrap(await api.GET('/v1/leagues/{leagueId}/waivers', lp)).data;
    expect(state.priority).toHaveLength(10);
    const pending = unwrap(await api.GET('/v1/leagues/{leagueId}/waivers/claims', lp)).data;
    expect(pending.length).toBeGreaterThanOrEqual(1);
    const processed = unwrap(
      await api.GET('/v1/leagues/{leagueId}/waivers/claims', {
        params: { path: { leagueId: OFFICE }, query: { status: 'processed' } },
      }),
    ).data;
    expect(new Set(processed.map((c) => c.failureReason))).toEqual(
      new Set([null, 'player_in_unavailable', 'player_out_unavailable']),
    );

    const players = unwrap(
      await api.GET('/v1/leagues/{leagueId}/players', {
        params: { path: { leagueId: OFFICE }, query: { limit: 100, status: 'all' } },
      }),
    ).data;
    const all = [...players];
    let cursorPage = players.length;
    while (cursorPage === 100) {
      const next = unwrap(
        await api.GET('/v1/leagues/{leagueId}/players', {
          params: {
            path: { leagueId: OFFICE },
            query: { limit: 100, cursor: btoa(String(all.length)) },
          },
        }),
      ).data;
      all.push(...next);
      cursorPage = next.length;
    }
    const roster = all.filter((p) => p.owner?.id === office().myTeam.id);
    expect(roster).toHaveLength(15);
    const onWaivers = all.filter((p) => p.rosterStatus === 'waivers');
    const free = all.filter((p) => p.rosterStatus === 'free_agent');
    expect(onWaivers.length).toBeGreaterThan(0);
    expect(onWaivers.every((p) => p.waiverClearsAt)).toBe(true);

    const target = onWaivers.find((w) => !pending.some((c) => c.add.id === w.player.id))!;
    const drop = roster.find((r) => r.player.position === target.player.position)!;
    const wrongDrop = roster.find((r) => r.player.position !== target.player.position)!;
    const mismatch = await api.POST('/v1/leagues/{leagueId}/waivers/claims', {
      ...lp,
      body: { addPlayerId: target.player.id, dropPlayerId: wrongDrop.player.id },
    });
    expect(mismatch.response.status).toBe(422);
    const claim = await api.POST('/v1/leagues/{leagueId}/waivers/claims', {
      ...lp,
      body: { addPlayerId: target.player.id, dropPlayerId: drop.player.id },
    });
    expect(claim.response.status).toBe(201);
    const ids = unwrap(await api.GET('/v1/leagues/{leagueId}/waivers/claims', lp)).data.map(
      (c) => c.id,
    );
    const reordered = unwrap(
      await api.PUT('/v1/leagues/{leagueId}/waivers/claim-order', {
        ...lp,
        body: { claimIds: [...ids].reverse() },
      }),
    ).data;
    expect(reordered.map((c) => c.id)).toEqual([...ids].reverse());
    const cancel = await api.DELETE('/v1/leagues/{leagueId}/waivers/claims/{claimId}', {
      params: { path: { leagueId: OFFICE, claimId: claim.data!.data.id } },
    });
    expect(cancel.response.status).toBe(204);

    const onWaiversAdd = await api.POST('/v1/leagues/{leagueId}/free-agent-moves', {
      ...lp,
      body: { addPlayerId: target.player.id, dropPlayerId: drop.player.id },
    });
    expect(onWaiversAdd.response.status).toBe(409);
    const fa = free.find((f) => f.player.position === drop.player.position)!;
    const move = await api.POST('/v1/leagues/{leagueId}/free-agent-moves', {
      ...lp,
      body: { addPlayerId: fa.player.id, dropPlayerId: drop.player.id },
    });
    expect(move.response.status).toBe(201);
    const team = unwrap(
      await api.GET('/v1/leagues/{leagueId}/teams/{teamId}', {
        params: { path: { leagueId: OFFICE, teamId: office().myTeam.id } },
      }),
    ).data;
    expect(team.roster.map((r) => r.player.id)).toContain(fa.player.id);
    expect(team.roster.find((r) => r.player.id === fa.player.id)!.acquiredVia).toBe('free_agent');
    const feed = unwrap(await api.GET('/v1/leagues/{leagueId}/transactions', lp)).data;
    expect(feed[0]).toMatchObject({ kind: 'free_agent', playerIn: { id: fa.player.id } });
  });
});

describe('trades', () => {
  it('covers every trade state and the actions the viewer may take', async () => {
    const trades = unwrap(await api.GET('/v1/leagues/{leagueId}/trades', path(OFFICE))).data;
    expect(new Set(trades.map((t) => t.status))).toEqual(
      new Set(['offered', 'in_review', 'processed', 'declined', 'withdrawn', 'expired', 'vetoed']),
    );
    const toMe = trades.find((t) => t.status === 'offered' && t.viewerActions.includes('accept'))!;
    const fromMe = trades.find(
      (t) => t.status === 'offered' && t.viewerActions.includes('withdraw'),
    )!;
    const review = trades.find((t) => t.status === 'in_review')!;
    expect(review.viewerActions).toEqual(expect.arrayContaining(['vote', 'review']));
    expect(review.review).toMatchObject({
      mode: 'league_vote',
      vetoVotes: 1,
      allowVotes: 1,
      votesNeeded: 4,
    });
    expectPublicIds(trades);

    const accepted = unwrap(
      await api.POST('/v1/leagues/{leagueId}/trades/{tradeId}/accept', {
        params: { path: { leagueId: OFFICE, tradeId: toMe.id } },
      }),
    ).data;
    expect(accepted.status).toBe('in_review');
    const withdrawn = unwrap(
      await api.POST('/v1/leagues/{leagueId}/trades/{tradeId}/withdraw', {
        params: { path: { leagueId: OFFICE, tradeId: fromMe.id } },
      }),
    ).data;
    expect(withdrawn.status).toBe('withdrawn');
    const twice = await api.POST('/v1/leagues/{leagueId}/trades/{tradeId}/withdraw', {
      params: { path: { leagueId: OFFICE, tradeId: fromMe.id } },
    });
    expect(twice.response.status).toBe(409);
    const voted = unwrap(
      await api.POST('/v1/leagues/{leagueId}/trades/{tradeId}/votes', {
        params: { path: { leagueId: OFFICE, tradeId: review.id } },
        body: { vote: 'veto' },
      }),
    ).data;
    expect(voted.review).toMatchObject({ vetoVotes: 2, myVote: 'veto' });
    const approved = unwrap(
      await api.POST('/v1/leagues/{leagueId}/trades/{tradeId}/review', {
        params: { path: { leagueId: OFFICE, tradeId: review.id } },
        body: { decision: 'approve' },
      }),
    ).data;
    expect(approved).toMatchObject({
      status: 'processed',
      review: { decidedBy: { id: data.DEMO_USER_ID } },
    });
  });

  it('rejects a trade whose positions do not match', async () => {
    const league = office();
    const other = league.TEAMS[3]!;
    const mine = league.teamDetail(league.myTeam.id)!.roster;
    const theirs = league.teamDetail(other.id)!.roster;
    const bad = await api.POST('/v1/leagues/{leagueId}/trades', {
      ...path(OFFICE),
      body: {
        receivingTeamId: other.id,
        offeredPlayerIds: [mine.find((r) => r.player.position === 'FWD')!.player.id],
        requestedPlayerIds: [theirs.find((r) => r.player.position === 'DEF')!.player.id],
      },
    });
    expect(bad.response.status).toBe(422);
    expect((bad.error as Schemas['Problem']).type).toMatch(/trade-shape$/);
    const good = await api.POST('/v1/leagues/{leagueId}/trades', {
      ...path(OFFICE),
      body: {
        receivingTeamId: other.id,
        offeredPlayerIds: [mine.find((r) => r.player.position === 'FWD')!.player.id],
        requestedPlayerIds: [theirs.find((r) => r.player.position === 'FWD')!.player.id],
        message: 'Straight swap?',
      },
    });
    expect(good.response.status).toBe(201);
    expect(good.data!.data).toMatchObject({ status: 'offered', viewerActions: ['withdraw'] });
  });

  it('has the other manager accept an offer a few seconds later', async () => {
    const SUNDAY = data.LEAGUE_ID;
    const league = data.leagues.get(SUNDAY) as ReturnType<typeof office>;
    const other = league.TEAMS[2]!;
    const mine = league.teamDetail(league.myTeam.id)!.roster;
    const theirs = league.teamDetail(other.id)!.roster;
    const give = mine.find((r) => r.player.position === 'MID')!.player;
    const get = theirs.find((r) => r.player.position === 'MID')!.player;
    const offered = unwrap(
      await api.POST('/v1/leagues/{leagueId}/trades', {
        ...path(SUNDAY),
        body: {
          receivingTeamId: other.id,
          offeredPlayerIds: [give.id],
          requestedPlayerIds: [get.id],
        },
      }),
    ).data;
    expect(offered.status).toBe('offered');
    // Not yet: still waiting for the other manager.
    league.settleTrades(Date.now() + 1_000);
    expect(league.getTrade(offered.id).status).toBe('offered');
    // The Sunday Draft has no trade review, so it processes at once.
    league.settleTrades(Date.now() + 60_000);
    const after = unwrap(
      await api.GET('/v1/leagues/{leagueId}/trades/{tradeId}', {
        params: { path: { leagueId: SUNDAY, tradeId: offered.id } },
      }),
    ).data;
    expect(after).toMatchObject({ status: 'processed', effectiveGameweek: 8 });
    expect(league.ownerOf(get.id)?.id).toBe(league.myTeam.id);
    const feed = unwrap(await api.GET('/v1/leagues/{leagueId}/transactions', path(SUNDAY))).data;
    expect(feed[0]).toMatchObject({
      kind: 'trade',
      status: 'processed',
      offered: [{ id: give.id }],
    });
  });
});

describe('trade execution', () => {
  const SUNDAY = data.LEAGUE_ID;
  const sunday = () => data.leagues.get(SUNDAY) as ReturnType<typeof office>;
  const POS = ['GKP', 'DEF', 'MID', 'FWD'];

  /** Sam offers a MID and a FWD (in that order) for a FWD and a MID (reversed); it processes. */
  async function crossOrderTrade() {
    const league = sunday();
    const other = league.TEAMS[2]!;
    const lineup = unwrap(
      await api.GET('/v1/leagues/{leagueId}/teams/{teamId}/lineup', {
        params: { path: { leagueId: SUNDAY, teamId: league.myTeam.id }, query: {} },
      }),
    ).data;
    const starter = (pos: string) => lineup.starters.find((e) => e.player.position === pos)!;
    const theirs = league.teamDetail(other.id)!.roster;
    const their = (pos: string) => theirs.find((r) => r.player.position === pos)!.player;
    const offered = [starter('MID').player.id, starter('FWD').player.id];
    const requested = [their('FWD').id, their('MID').id];
    const trade = unwrap(
      await api.POST('/v1/leagues/{leagueId}/trades', {
        ...path(SUNDAY),
        body: {
          receivingTeamId: other.id,
          offeredPlayerIds: offered,
          requestedPlayerIds: requested,
        },
      }),
    ).data;
    league.settleTrades(Date.now() + 60_000);
    expect(league.getTrade(trade.id).status).toBe('processed');
    return { league, other, lineup, offered, requested, theirs };
  }

  it('swaps players position for position, so lineups keep their formation', async () => {
    const { league, lineup } = await crossOrderTrade();
    const after = unwrap(
      await api.GET('/v1/leagues/{leagueId}/teams/{teamId}/lineup', {
        params: { path: { leagueId: SUNDAY, teamId: league.myTeam.id }, query: {} },
      }),
    ).data;
    const shape = (l: typeof lineup) => l.starters.map((e) => e.player.position);
    expect(shape(after)).toEqual(shape(lineup));
    expect(after.formation).toBe(lineup.formation);
  });

  it("does not reveal another team's pending lineup order in its team detail", async () => {
    const { other, requested, theirs } = await crossOrderTrade();
    const detail = unwrap(
      await api.GET('/v1/leagues/{leagueId}/teams/{teamId}', {
        params: { path: { leagueId: SUNDAY, teamId: other.id } },
      }),
    ).data;
    // Last locked order (before the trade) for players still there; newcomers
    // follow, by position.
    const kept = theirs.map((r) => r.player).filter((p) => !requested.includes(p.id));
    const ids = detail.roster.map((r) => r.player.id);
    expect(ids.slice(0, kept.length)).toEqual(kept.map((p) => p.id));
    const newcomers = detail.roster.slice(kept.length).map((r) => r.player.position);
    expect(newcomers).toEqual([...newcomers].sort((a, b) => POS.indexOf(a) - POS.indexOf(b)));
    expect(detail.roster.map((r) => r.slot)).toEqual(detail.roster.map((_, i) => i + 1));
  });
});

describe('errors', () => {
  it('answers season-only endpoints for a league that has not drafted with 409', async () => {
    const res = await api.POST('/v1/leagues/{leagueId}/trades', {
      ...path(data.PREDRAFT_LEAGUE_ID),
      body: {
        receivingTeamId: 'tm_01k7m2xq800000000000000001',
        offeredPlayerIds: [],
        requestedPlayerIds: [],
      },
    });
    expect(res.response.status).toBe(409);
    expect(() => unwrap(res)).toThrow(ApiError);
  });
});
