/** @jest-environment node */
/**
 * The imported-league mocks (ADR 0012) through the generated client: the
 * FPL status for the commissioner and for members, seats and claim links,
 * moves that work like in any league, the commissioner's re-sync (confirm,
 * cancelled trades and claims, the note) and check against FPL.
 */
import { createApiClient, unwrap } from '@myfantasyprem/api-client';
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
const path = (leagueId: string) => ({ params: { path: { leagueId } } });
const slug = (error: unknown) => (error as { type?: string } | undefined)?.type?.split('/').pop();

beforeEach(() => resetMocks());

type Imported = Extract<data.MockLeague, { imported: true }>;
const imported = (id = data.IMPORTED_LEAGUE_ID) => data.leagues.get(id)! as Imported;

describe('imported leagues', () => {
  it('shows the commissioner where FPL stands and a clean check', async () => {
    const sync = unwrap(
      await api.GET('/v1/leagues/{leagueId}/sync', path(data.IMPORTED_LEAGUE_ID)),
    ).data;
    expect(sync).toMatchObject({
      fplLeagueId: data.FPL_LEAGUE_ID,
      pending: null,
      lastError: null,
      note: null,
    });
    expect(sync.lastSyncedAt).not.toBeNull();
    expect(sync.verification).toMatchObject({ status: 'clean', unexplained: 0, failing: [] });
    expect(sync).not.toHaveProperty('state');
    expect(sync).not.toHaveProperty('switchOptions');
  });

  it('lists seats with claim links and issues a new one', async () => {
    const seats = unwrap(
      await api.GET('/v1/leagues/{leagueId}/seats', path(data.IMPORTED_LEAGUE_ID)),
    ).data;
    expect(seats).toHaveLength(8);
    expect(seats.filter((s) => s.claimed)).toHaveLength(2);
    const open = seats.find((s) => !s.claimed)!;
    expect(open.invite).toMatchObject({ maxUses: 1, expiresAt: null, claimTeam: open.team });
    const res = await api.POST('/v1/leagues/{leagueId}/seats/{teamId}/invite', {
      params: { path: { leagueId: data.IMPORTED_LEAGUE_ID, teamId: open.team.id } },
    });
    expect(res.response.status).toBe(201);
    expect(res.data!.data.code).not.toBe(open.invite!.code);
    const claimed = seats.find((s) => s.claimed)!;
    const again = await api.POST('/v1/leagues/{leagueId}/seats/{teamId}/invite', {
      params: { path: { leagueId: data.IMPORTED_LEAGUE_ID, teamId: claimed.team.id } },
    });
    expect(slug(again.error)).toBe('seat-claimed');
  });

  it('issues a sign-in link for a claimed seat that signs its manager in once (ADR 0013)', async () => {
    const seats = unwrap(
      await api.GET('/v1/leagues/{leagueId}/seats', path(data.IMPORTED_LEAGUE_ID)),
    ).data;
    const priya = seats.find((s) => s.owner?.displayName === 'Priya N.')!;
    const issue = (teamId: string) =>
      api.POST('/v1/leagues/{leagueId}/seats/{teamId}/signin-link', {
        params: { path: { leagueId: data.IMPORTED_LEAGUE_ID, teamId } },
      });
    const first = await issue(priya.team.id);
    expect(first.response.status).toBe(201);
    const second = unwrap(await issue(priya.team.id)).data;
    expect(second).toMatchObject({ team: priya.team, owner: priya.owner });
    const code = (url: string) => url.split('/signin-link/')[1]!;
    expect(code(second.url)).toMatch(/^[A-Za-z0-9_-]{43}$/);
    const signIn = (c: string) => api.POST('/v1/auth/signin-link', { body: { code: c } });
    // The older link was replaced; the new one works once.
    expect((await signIn(code(first.data!.data.url))).response.status).toBe(401);
    const ok = unwrap(await signIn(code(second.url))).data;
    expect(ok.leagueId).toBe(data.IMPORTED_LEAGUE_ID);
    expect(ok.session.user).toMatchObject({ displayName: 'Priya N.', email: null });
    expect((await signIn(code(second.url))).response.status).toBe(401);
    // Not for an unclaimed seat or my own.
    expect(slug((await issue(seats.find((s) => !s.claimed)!.team.id)).error)).toBe(
      'seat-unclaimed',
    );
    const mine = seats.find((s) => s.owner?.id === data.DEMO_USER_ID)!;
    expect(slug((await issue(mine.team.id)).error)).toBe('has-sign-in');
  });

  it('plays like a native league: signing, waiver claims, trades and renames succeed', async () => {
    const league = path(data.IMPORTED_LEAGUE_ID);
    const l = imported();
    const me = l.viewer().team!;
    const roster = l.teamDetail(me.id)!.roster.map((r) => r.player);
    const freeOf = (position: string, status: 'free_agent' | 'waivers') =>
      data.PLAYERS.find((p) => p.position === position && l.rosterStatus(p.id) === status)!;

    const signing = roster[0]!;
    const free = freeOf(signing.position, 'free_agent');
    const signed = await api.POST('/v1/leagues/{leagueId}/free-agent-moves', {
      ...league,
      body: { addPlayerId: free.id, dropPlayerId: signing.id },
    });
    expect(signed.response.status).toBe(201);
    expect(l.ownerOf(free.id)?.id).toBe(me.id);

    const dropping = roster.find((p) => p.position !== signing.position)!;
    const waiver = freeOf(dropping.position, 'waivers');
    const claim = await api.POST('/v1/leagues/{leagueId}/waivers/claims', {
      ...league,
      body: { addPlayerId: waiver.id, dropPlayerId: dropping.id },
    });
    expect(claim.response.status).toBe(201);

    const other = l.members().find((m) => m.team!.id !== me.id)!.team!;
    const theirs = l.teamDetail(other.id)!.roster.map((r) => r.player);
    const give = l.teamDetail(me.id)!.roster.find((r) => r.player.id !== free.id)!.player;
    const get = theirs.find((p) => p.position === give.position)!;
    const trade = await api.POST('/v1/leagues/{leagueId}/trades', {
      ...league,
      body: {
        receivingTeamId: other.id,
        offeredPlayerIds: [give.id],
        requestedPlayerIds: [get.id],
      },
    });
    expect(trade.response.status).toBe(201);

    const rename = await api.PATCH('/v1/leagues/{leagueId}', { ...league, body: { name: 'New' } });
    expect(rename.response.status).toBe(200);
  });

  it('re-syncs only with confirm, then resets to FPL and cancels open trades and claims', async () => {
    const league = path(data.IMPORTED_LEAGUE_ID);
    const l = imported();
    const me = l.viewer().team!;
    const roster = l.teamDetail(me.id)!.roster.map((r) => r.player);
    const signing = roster[0]!;
    const free = data.PLAYERS.find(
      (p) => p.position === signing.position && l.rosterStatus(p.id) === 'free_agent',
    )!;
    await api.POST('/v1/leagues/{leagueId}/free-agent-moves', {
      ...league,
      body: { addPlayerId: free.id, dropPlayerId: signing.id },
    });
    const other = l.members().find((m) => m.team!.id !== me.id)!.team!;
    const give = l.teamDetail(me.id)!.roster.find((r) => r.player.id !== free.id)!.player;
    const get = l.teamDetail(other.id)!.roster.find((r) => r.player.position === give.position)!;
    const offer = unwrap(
      await api.POST('/v1/leagues/{leagueId}/trades', {
        ...league,
        body: {
          receivingTeamId: other.id,
          offeredPlayerIds: [give.id],
          requestedPlayerIds: [get.player.id],
        },
      }),
    ).data;

    const unconfirmed = await api.POST('/v1/leagues/{leagueId}/sync', {
      ...league,
      body: { confirm: false } as never,
    });
    expect(unconfirmed.response.status).toBe(422);

    const queued = await api.POST('/v1/leagues/{leagueId}/sync', {
      ...league,
      body: { confirm: true },
    });
    expect(queued.response.status).toBe(202);
    expect(queued.data!.data.pending).toMatchObject({ kind: 'resync' });
    const again = await api.POST('/v1/leagues/{leagueId}/sync', {
      ...league,
      body: { confirm: true },
    });
    expect(slug(again.error)).toBe('sync-pending');
    const checkWhileRunning = await api.POST('/v1/leagues/{leagueId}/sync/check', league);
    expect(slug(checkWhileRunning.error)).toBe('sync-pending');

    const done = unwrap(await api.GET('/v1/leagues/{leagueId}/sync', league)).data;
    expect(done.pending).toBeNull();
    expect(done.note).toMatch(/^Re-synced from FPL\. .*open trade.* (was|were) cancelled\.$/);
    expect(done.note).toMatch(/waiver claim/);

    // FPL's squads again: the signing is gone.
    expect(l.ownerOf(free.id)).toBeNull();
    expect(l.ownerOf(signing.id)?.id).toBe(me.id);
    // The open offer failed with league_resynced; nothing is pending.
    const failed = unwrap(
      await api.GET('/v1/leagues/{leagueId}/trades', {
        params: { path: { leagueId: data.IMPORTED_LEAGUE_ID }, query: { status: 'failed' } },
      }),
    ).data;
    expect(
      failed.find((t) => t.offered[0]?.id === give.id && t.requested[0]?.id === get.player.id),
    ).toMatchObject({ status: 'failed', failureReason: 'league_resynced' });
    expect(offer.status).toBe('offered');
    const open = unwrap(
      await api.GET('/v1/leagues/{leagueId}/trades', {
        params: { path: { leagueId: data.IMPORTED_LEAGUE_ID }, query: { status: 'open' } },
      }),
    ).data;
    expect(open).toEqual([]);
    const claims = unwrap(
      await api.GET('/v1/leagues/{leagueId}/waivers/claims', {
        params: { path: { leagueId: data.IMPORTED_LEAGUE_ID }, query: { status: 'pending' } },
      }),
    ).data;
    expect(claims).toEqual([]);

    // Free to play on, and to re-sync again.
    const next = await api.POST('/v1/leagues/{leagueId}/sync', {
      ...league,
      body: { confirm: true },
    });
    expect(next.response.status).toBe(202);
  });

  it('checks against FPL: a report, nothing blocked, nothing changed', async () => {
    const league = path(data.MISMATCH_LEAGUE_ID);
    const queued = await api.POST('/v1/leagues/{leagueId}/sync/check', league);
    expect(queued.response.status).toBe(202);
    expect(queued.data!.data.pending).toMatchObject({ kind: 'check' });
    const again = await api.POST('/v1/leagues/{leagueId}/sync/check', league);
    expect(slug(again.error)).toBe('sync-pending');

    const done = unwrap(await api.GET('/v1/leagues/{leagueId}/sync', league)).data;
    expect(done.pending).toBeNull();
    expect(done.note).toBeNull();
    expect(done.verification).toMatchObject({ status: 'mismatch', unexplained: 2 });
    // Informational: moves still work.
    const l = imported(data.MISMATCH_LEAGUE_ID);
    const mine = l.teamDetail(l.viewer().team!.id)!.roster[0]!.player;
    const free = data.PLAYERS.find(
      (p) => p.position === mine.position && l.rosterStatus(p.id) === 'free_agent',
    )!;
    const move = await api.POST('/v1/leagues/{leagueId}/free-agent-moves', {
      ...league,
      body: { addPlayerId: free.id, dropPlayerId: mine.id },
    });
    expect(move.response.status).toBe(201);
  });

  it('claims a seat with its link: preview, accept, then the league is mine', async () => {
    const preview = unwrap(
      await api.GET('/v1/invites/{code}', { params: { path: { code: data.CLAIM_CODE } } }),
    ).data;
    expect(preview).toMatchObject({
      leagueId: data.CLAIM_LEAGUE_ID,
      joinable: true,
      claimTeam: { teamName: data.CLAIM_TEAM_NAME },
    });
    const before = unwrap(await api.GET('/v1/me/leagues')).data;
    expect(before.some((m) => m.league.id === data.CLAIM_LEAGUE_ID)).toBe(false);
    const res = await api.POST('/v1/invites/{code}/accept', {
      params: { path: { code: data.CLAIM_CODE } },
      body: {},
    });
    expect(res.response.status).toBe(201);
    expect(res.data!.data.team.name).toBe(data.CLAIM_TEAM_NAME);
    const after = unwrap(await api.GET('/v1/me/leagues')).data;
    expect(after.some((m) => m.league.id === data.CLAIM_LEAGUE_ID)).toBe(true);
    // Single use.
    const gone = await api.GET('/v1/invites/{code}', {
      params: { path: { code: data.CLAIM_CODE } },
    });
    expect(gone.response.status).toBe(404);
  });

  it('signs a signed-out person in with a claim link and a name (ADR 0013)', async () => {
    const preview = unwrap(
      await api.GET('/v1/invites/{code}', { params: { path: { code: data.CLAIM_CODE } } }),
    ).data;
    expect(preview.claimSignIn).toBe(true);
    const bad = await api.POST('/v1/auth/claim', {
      body: { code: data.CLAIM_CODE, displayName: ' ' },
    });
    expect(bad.response.status).toBe(422);
    const claimed = unwrap(
      await api.POST('/v1/auth/claim', { body: { code: data.CLAIM_CODE, displayName: 'Jordan' } }),
    ).data;
    expect(claimed.session).toMatchObject({
      isNewUser: true,
      user: { displayName: 'Jordan', email: null, authProviders: [] },
    });
    expect(claimed.membership).toMatchObject({
      league: { id: data.CLAIM_LEAGUE_ID },
      team: { name: data.CLAIM_TEAM_NAME },
      role: 'member',
    });
    const again = await api.POST('/v1/auth/claim', {
      body: { code: data.CLAIM_CODE, displayName: 'Someone' },
    });
    expect(again.response.status).toBe(409);
    expect(slug(again.error)).toBe('seat-claimed');
  });

  it('hides the check from members and keeps re-sync to the commissioner', async () => {
    await api.POST('/v1/invites/{code}/accept', {
      params: { path: { code: data.CLAIM_CODE } },
      body: {},
    });
    const sync = unwrap(
      await api.GET('/v1/leagues/{leagueId}/sync', path(data.CLAIM_LEAGUE_ID)),
    ).data;
    expect(sync.verification).toBeNull();
    const resync = await api.POST('/v1/leagues/{leagueId}/sync', {
      ...path(data.CLAIM_LEAGUE_ID),
      body: { confirm: true },
    });
    expect(slug(resync.error)).toBe('not-commissioner');
    const check = await api.POST('/v1/leagues/{leagueId}/sync/check', path(data.CLAIM_LEAGUE_ID));
    expect(slug(check.error)).toBe('not-commissioner');
  });
});
