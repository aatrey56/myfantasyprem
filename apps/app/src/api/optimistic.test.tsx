/**
 * Optimistic mutations against the real MSW mock server: the move shows
 * before the server answers, a forced failure puts every cache back exactly
 * as it was, and a move FPL Draft's rules refuse is neither shown nor sent.
 * Expected values come from the rules (squads 2/5/5/3, same-position drops,
 * free agents vs waivers, slot 12 the backup keeper, trade shape), not from
 * the hooks.
 */
import type { Schemas } from '@myfantasyprem/api-client';
import { QueryClient, QueryClientProvider, type InfiniteData } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import { getResponse, http, type RequestHandler } from 'msw';
import type { ReactNode } from 'react';

import { send } from '@myfantasyprem/api-client';
import { api } from './client';
import { playersQuery, queryKeys, teamQuery } from './hooks';
import { useSetLineup } from './lineup-hooks';
import {
  useAddFreeAgent,
  useCancelClaim,
  useCreateClaim,
  useProposeTrade,
  useReorderClaims,
  useRespondToTrade,
} from './moves-hooks';
import { RuleError } from './optimistic';
import { useToggleWatch, watchlistKey } from './watchlist-hooks';
import { haptics } from '@/lib/haptics';
import { LEAGUE_ID } from '@/mocks/data';
import { handlers, problem, resetMocks } from '@/mocks/handlers';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));
jest.mock('expo-router', () => ({ useIsFocused: () => true }));
jest.mock('@/lib/haptics', () => ({
  haptics: { success: jest.fn(), error: jest.fn(), light: jest.fn(), selection: jest.fn() },
}));

const L = LEAGUE_ID;
type Envelope<T> = { data: T; meta: Schemas['Meta'] };
type Players = InfiniteData<Envelope<Schemas['PlayerListItem'][]>>;

// ---------------------------------------------------------------- fake network

/** Handlers placed before the mock server's own (forced failures). */
let overrides: RequestHandler[];
/** Writes wait here until released, so the optimistic state can be seen. */
let gate: { promise: Promise<void>; release: () => void } | null;
let writes: string[];

const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    if (request.method !== 'GET') {
      writes.push(`${request.method} ${new URL(request.url).pathname}`);
      if (gate) await gate.promise;
    }
    const response = await getResponse([...overrides, ...handlers], request);
    if (!response) throw new Error(`No mock handler for ${request.method} ${request.url}`);
    return response;
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});

const holdWrites = () => {
  let release!: () => void;
  const promise = new Promise<void>((r) => (release = r));
  gate = { promise, release };
};
const releaseWrites = () => {
  gate?.release();
  gate = null;
};
const fail = (method: 'put' | 'post' | 'delete', path: string, status: number, slug: string) =>
  overrides.push(
    http[method](`*/v1/leagues/:leagueId${path}`, () =>
      problem(status, 'Refused', `Forced ${slug}.`, slug),
    ),
  );

// ---------------------------------------------------------------- cache

let queryClient: QueryClient;
beforeEach(() => {
  resetMocks();
  overrides = [];
  gate = null;
  writes = [];
  jest.clearAllMocks();
  queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity, staleTime: Infinity },
      mutations: { gcTime: Infinity },
    },
  });
});
afterEach(() => {
  releaseWrites();
  queryClient.clear();
});

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);

/** Everything the league has in the cache, deep-copied, to compare after a rollback. */
const cacheCopy = () =>
  JSON.parse(
    JSON.stringify(
      queryClient
        .getQueryCache()
        .getAll()
        .map((q) => [q.queryKey, q.state.data]),
    ),
  ) as unknown;

async function seedLeague() {
  const home = await queryClient.fetchQuery({
    queryKey: queryKeys.league(L),
    queryFn: () => send(api.GET('/v1/leagues/{leagueId}', { params: { path: { leagueId: L } } })),
  });
  const me = home.data.viewer.team!;
  const team = await queryClient.fetchQuery(teamQuery(L, me.id));
  return { home: home.data, me, roster: team.data.roster };
}

async function seedPlayers(status: 'available' | 'free_agent' | 'waivers' = 'available') {
  const filter = { status, limit: 100 } as const;
  await queryClient.fetchInfiniteQuery(playersQuery(L, filter));
  return queryClient.getQueryData<Players>(queryKeys.players(L, filter))!.pages[0]!.data;
}

async function seedLineup(teamId: string) {
  const key = queryKeys.lineup(L, teamId, undefined);
  await queryClient.fetchQuery({
    queryKey: key,
    queryFn: () =>
      send(
        api.GET('/v1/leagues/{leagueId}/teams/{teamId}/lineup', {
          params: { path: { leagueId: L, teamId } },
        }),
      ),
  });
  return { key, lineup: queryClient.getQueryData<Envelope<Schemas['Lineup']>>(key)!.data };
}

/** Lets the mutation's async onMutate run while its request is held. */
const settle = () => waitFor(() => expect(queryClient.isMutating()).toBe(0));

// ---------------------------------------------------------------- lineup

describe('lineup swap', () => {
  async function setup() {
    const { me } = await seedLeague();
    const { key, lineup } = await seedLineup(me.id);
    const { result } = await renderHook(() => useSetLineup(L, me.id), { wrapper });
    const ids = (e: Schemas['LineupEntry'][]) => e.map((x) => x.player.id);
    // Slot 1 is the keeper, slot 12 (bench first) the backup keeper.
    const keeper = lineup.starters.find((e) => e.player.position === 'GKP')!.player.id;
    const backup = lineup.bench[0]!.player.id;
    const keeperSwap = {
      gameweek: lineup.gameweek,
      starters: ids(lineup.starters).map((id) => (id === keeper ? backup : id)),
      bench: ids(lineup.bench).map((id) => (id === backup ? keeper : id)),
    };
    const read = () => queryClient.getQueryData<Envelope<Schemas['Lineup']>>(key)!.data;
    return { key, lineup, result, keeper, backup, keeperSwap, read, ids };
  }

  it('shows the swapped keepers before the server answers', async () => {
    const { result, keeper, backup, keeperSwap, read, ids } = await setup();
    holdWrites();
    await act(async () => {
      result.current.mutate(keeperSwap);
    });
    await waitFor(() => expect(ids(read().starters)).toContain(backup));
    expect(ids(read().bench)[0]).toBe(keeper);
    expect(read().starters.map((e) => e.slot)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11]);
    expect(read().bench.map((e) => e.slot)).toEqual([12, 13, 14, 15]);
    expect(writes).toHaveLength(1);
    releaseWrites();
    await settle();
    expect(ids(read().starters)).toContain(backup);
    expect(haptics.error).not.toHaveBeenCalled();
  });

  it('a refused save restores exactly the previous cache and taps an error', async () => {
    const { result, keeperSwap } = await setup();
    const before = cacheCopy();
    fail('put', '/teams/:teamId/lineup', 422, 'invalid-lineup');
    await act(async () => {
      await result.current.mutateAsync(keeperSwap).catch(() => undefined);
    });
    expect(cacheCopy()).toEqual(before);
    expect(haptics.error).toHaveBeenCalledTimes(1);
  });

  it('a keeper for an outfield player (no keeper in the XI) is never shown or sent', async () => {
    const { result, lineup, keeper, ids } = await setup();
    const outfield = lineup.bench.find((e) => e.player.position !== 'GKP')!.player.id;
    const before = cacheCopy();
    let error: unknown;
    await act(async () => {
      await result.current
        .mutateAsync({
          gameweek: lineup.gameweek,
          starters: ids(lineup.starters).map((id) => (id === keeper ? outfield : id)),
          bench: ids(lineup.bench).map((id) => (id === outfield ? keeper : id)),
        })
        .catch((e: unknown) => (error = e));
    });
    expect(error).toBeInstanceOf(RuleError);
    expect((error as RuleError).problems.join(' ')).toMatch(/goalkeeper/i);
    expect(writes).toEqual([]);
    expect(cacheCopy()).toEqual(before);
  });

  it('a locked gameweek is refused before anything is shown or sent', async () => {
    const { result, key, keeperSwap } = await setup();
    queryClient.setQueryData<Envelope<Schemas['Lineup']>>(key, (d) => ({
      ...d!,
      data: { ...d!.data, locked: true },
    }));
    const before = cacheCopy();
    let error: unknown;
    await act(async () => {
      await result.current.mutateAsync(keeperSwap).catch((e: unknown) => (error = e));
    });
    expect(error).toBeInstanceOf(RuleError);
    expect(String((error as Error).message)).toMatch(/locked/);
    expect(writes).toEqual([]);
    expect(cacheCopy()).toEqual(before);
  });
});

// ---------------------------------------------------------------- free agents

describe('free-agent sign (+ drop)', () => {
  async function setup() {
    const { me, roster } = await seedLeague();
    const { key: lineupKey } = await seedLineup(me.id);
    const available = await seedPlayers();
    const free = available.find(
      (p) =>
        p.rosterStatus === 'free_agent' &&
        roster.some((r) => r.player.position === p.player.position),
    )!;
    const drop = roster.find((r) => r.player.position === free.player.position)!;
    const { result } = await renderHook(() => useAddFreeAgent(L), { wrapper });
    const body = { addPlayerId: free.player.id, dropPlayerId: drop.player.id };
    return { me, roster, free, drop, result, body, lineupKey, available };
  }
  const rosterIds = (me: Schemas['TeamRef']) =>
    queryClient
      .getQueryData<Envelope<Schemas['TeamDetail']>>(queryKeys.team(L, me.id))!
      .data.roster.map((r) => r.player.id);
  const listed = (id: string) =>
    queryClient
      .getQueryData<Players>(queryKeys.players(L, { status: 'available', limit: 100 }))!
      .pages[0]!.data.find((p) => p.player.id === id)!;

  it('the squad, the open lineup and the lists show the move at once', async () => {
    const { me, free, drop, result, body, lineupKey } = await setup();
    holdWrites();
    await act(async () => {
      result.current.mutate(body);
    });
    await waitFor(() => expect(rosterIds(me)).toContain(free.player.id));
    expect(rosterIds(me)).not.toContain(drop.player.id);
    expect(rosterIds(me)).toHaveLength(15);
    const entry = queryClient
      .getQueryData<Envelope<Schemas['TeamDetail']>>(queryKeys.team(L, me.id))!
      .data.roster.find((r) => r.player.id === free.player.id)!;
    expect(entry.slot).toBe(drop.slot);
    expect(entry.acquiredVia).toBe('free_agent');
    // The arrival takes the departed player's lineup slot.
    const lineup = queryClient.getQueryData<Envelope<Schemas['Lineup']>>(lineupKey)!.data;
    const all = [...lineup.starters, ...lineup.bench];
    expect(all.map((e) => e.player.id)).toContain(free.player.id);
    expect(all.map((e) => e.player.id)).not.toContain(drop.player.id);
    expect(listed(free.player.id)).toMatchObject({ rosterStatus: 'rostered', owner: me });
    expect(haptics.success).not.toHaveBeenCalled();
    releaseWrites();
    await settle();
    expect(haptics.success).toHaveBeenCalledTimes(1);
    expect(haptics.error).not.toHaveBeenCalled();
  });

  it("the arrival's lineup entry has only that gameweek's fixtures", async () => {
    const { free, result, body, lineupKey } = await setup();
    // The list row carries every remaining fixture (several gameweeks).
    expect(new Set(free.nextFixtures.map((f) => f.gameweek)).size).toBeGreaterThan(1);
    holdWrites();
    await act(async () => {
      result.current.mutate(body);
    });
    const lineup = () => queryClient.getQueryData<Envelope<Schemas['Lineup']>>(lineupKey)!.data;
    await waitFor(() =>
      expect([...lineup().starters, ...lineup().bench].map((e) => e.player.id)).toContain(
        free.player.id,
      ),
    );
    const { gameweek, starters, bench } = lineup();
    const entry = [...starters, ...bench].find((e) => e.player.id === free.player.id)!;
    const expected = free.nextFixtures.filter((f) => f.gameweek === gameweek);
    expect(entry.fixtures.map((f) => f.fixtureId)).toEqual(expected.map((f) => f.fixtureId));
    // The lineup keeps the full shape: each opponent a club, not a code.
    for (const f of entry.fixtures) expect(f.opponent.code).toEqual(expect.any(String));
    releaseWrites();
    await settle();
  });

  it('a refused signing restores every cache exactly and taps an error', async () => {
    const { result, body } = await setup();
    const before = cacheCopy();
    fail('post', '/free-agent-moves', 409, 'player-taken');
    let error: unknown;
    await act(async () => {
      await result.current.mutateAsync(body).catch((e: unknown) => (error = e));
    });
    expect(String((error as Error).message)).toBe('Forced player-taken.');
    expect(cacheCopy()).toEqual(before);
    expect(haptics.error).toHaveBeenCalledTimes(1);
    expect(haptics.success).not.toHaveBeenCalled();
  });

  it('dropping a player of another position is refused before it shows', async () => {
    const { roster, free, result } = await setup();
    const other = roster.find((r) => r.player.position !== free.player.position)!;
    const before = cacheCopy();
    let error: unknown;
    await act(async () => {
      await result.current
        .mutateAsync({ addPlayerId: free.player.id, dropPlayerId: other.player.id })
        .catch((e: unknown) => (error = e));
    });
    expect(error).toBeInstanceOf(RuleError);
    expect(writes).toEqual([]);
    expect(cacheCopy()).toEqual(before);
  });

  it('a player on waivers cannot be added straight away', async () => {
    const { roster, result, available } = await setup();
    const onWaivers = available.find(
      (p) =>
        p.rosterStatus === 'waivers' && roster.some((r) => r.player.position === p.player.position),
    );
    if (!onWaivers) throw new Error('The mock league no longer has the data this test needs.');
    const drop = roster.find((r) => r.player.position === onWaivers.player.position)!;
    let error: unknown;
    await act(async () => {
      await result.current
        .mutateAsync({ addPlayerId: onWaivers.player.id, dropPlayerId: drop.player.id })
        .catch((e: unknown) => (error = e));
    });
    expect(error).toBeInstanceOf(RuleError);
    expect(writes).toEqual([]);
  });
});

// ---------------------------------------------------------------- waivers

describe('waiver claims', () => {
  const key = queryKeys.claims(L, 'pending');
  async function setup() {
    const { me, roster } = await seedLeague();
    await seedPlayers();
    await queryClient.fetchQuery({
      queryKey: key,
      queryFn: () =>
        send(
          api.GET('/v1/leagues/{leagueId}/waivers/claims', {
            params: { path: { leagueId: L }, query: { status: 'pending' } },
          }),
        ),
    });
    const claims = () => queryClient.getQueryData<Envelope<Schemas['WaiverClaim'][]>>(key)!.data;
    const hooks = await renderHook(
      () => ({
        place: useCreateClaim(L),
        cancel: useCancelClaim(L),
        reorder: useReorderClaims(L),
      }),
      { wrapper },
    );
    return { me, roster, claims, result: hooks.result };
  }

  /** A waiver player my squad can take, with a same-position drop not already claimed. */
  function waiverMove(roster: Schemas['RosterEntry'][], claims: Schemas['WaiverClaim'][]) {
    const list = queryClient.getQueryData<Players>(
      queryKeys.players(L, { status: 'available', limit: 100 }),
    )!.pages[0]!.data;
    for (const p of list.filter((x) => x.rosterStatus === 'waivers')) {
      const drop = roster.find(
        (r) =>
          r.player.position === p.player.position &&
          !claims.some((c) => c.add.id === p.player.id && c.drop.id === r.player.id),
      );
      if (drop) return { add: p, drop };
    }
    return null;
  }

  it('a new claim joins the end of my list at once and is confirmed', async () => {
    const { roster, claims, result } = await setup();
    const move = waiverMove(roster, claims());
    if (!move) throw new Error('The mock league no longer has the data this test needs.');
    const n = claims().length;
    holdWrites();
    await act(async () => {
      result.current.place.mutate({
        addPlayerId: move.add.player.id,
        dropPlayerId: move.drop.player.id,
      });
    });
    await waitFor(() => expect(claims()).toHaveLength(n + 1));
    expect(claims()[n]).toMatchObject({
      add: { id: move.add.player.id },
      drop: { id: move.drop.player.id },
      order: n + 1,
      status: 'pending',
    });
    releaseWrites();
    await settle();
    expect(haptics.success).toHaveBeenCalledTimes(1);
  });

  it('a refused claim restores the list exactly', async () => {
    const { roster, claims, result } = await setup();
    const move = waiverMove(roster, claims());
    if (!move) throw new Error('The mock league no longer has the data this test needs.');
    const before = cacheCopy();
    fail('post', '/waivers/claims', 409, 'duplicate-claim');
    await act(async () => {
      await result.current.place
        .mutateAsync({ addPlayerId: move.add.player.id, dropPlayerId: move.drop.player.id })
        .catch(() => undefined);
    });
    expect(cacheCopy()).toEqual(before);
    expect(haptics.error).toHaveBeenCalledTimes(1);
  });

  it('a free agent is added, not claimed: the claim never shows', async () => {
    const { roster, result } = await setup();
    const list = queryClient.getQueryData<Players>(
      queryKeys.players(L, { status: 'available', limit: 100 }),
    )!.pages[0]!.data;
    const free = list.find((p) => p.rosterStatus === 'free_agent')!;
    const drop = roster.find((r) => r.player.position === free.player.position)!;
    const before = cacheCopy();
    let error: unknown;
    await act(async () => {
      await result.current.place
        .mutateAsync({ addPlayerId: free.player.id, dropPlayerId: drop.player.id })
        .catch((e: unknown) => (error = e));
    });
    expect(error).toBeInstanceOf(RuleError);
    expect(writes).toEqual([]);
    expect(cacheCopy()).toEqual(before);
  });

  it('cancel removes the claim at once; a refusal brings it back exactly', async () => {
    const { claims, result } = await setup();
    if (claims().length < 2)
      throw new Error('The mock league no longer has the data this test needs.');
    const [first, second] = claims();
    holdWrites();
    const before = cacheCopy();
    fail('delete', '/waivers/claims/:claimId', 500, 'internal');
    await act(async () => {
      result.current.cancel.mutate(first!.id);
    });
    await waitFor(() => expect(claims().map((c) => c.id)).not.toContain(first!.id));
    // The next claim moves up to first preference.
    expect(claims()[0]).toMatchObject({ id: second!.id, order: 1 });
    releaseWrites();
    await settle();
    expect(cacheCopy()).toEqual(before);
    expect(haptics.error).toHaveBeenCalledTimes(1);
  });

  it('reorder shows at once; a refusal restores the order exactly', async () => {
    const { claims, result } = await setup();
    if (claims().length < 2)
      throw new Error('The mock league no longer has the data this test needs.');
    const ids = claims().map((c) => c.id);
    const reversed = [...ids].reverse();
    const before = cacheCopy();
    fail('put', '/waivers/claim-order', 422, 'claim-order');
    holdWrites();
    await act(async () => {
      result.current.reorder.mutate(reversed);
    });
    await waitFor(() => expect(claims().map((c) => c.id)).toEqual(reversed));
    releaseWrites();
    await settle();
    expect(cacheCopy()).toEqual(before);
  });
});

// ---------------------------------------------------------------- trades

describe('trades', () => {
  const key = queryKeys.trades(L);
  type Trades = InfiniteData<Envelope<Schemas['Trade'][]>>;
  const trades = () =>
    queryClient.getQueryData<Trades>(key)!.pages.flatMap((p) => p.data) as Schemas['Trade'][];

  async function setup() {
    const { home, me, roster } = await seedLeague();
    await queryClient.fetchInfiniteQuery({
      queryKey: key,
      initialPageParam: undefined as string | undefined,
      queryFn: () =>
        send(api.GET('/v1/leagues/{leagueId}/trades', { params: { path: { leagueId: L } } })),
      getNextPageParam: () => undefined,
    });
    const other = home.standings.find((r) => r.team.id !== me.id)!.team;
    const theirs = (await queryClient.fetchQuery(teamQuery(L, other.id))).data.roster;
    const { result } = await renderHook(
      () => ({ propose: useProposeTrade(L), respond: useRespondToTrade(L) }),
      { wrapper },
    );
    // One midfielder for one midfielder: a legal 1-for-1.
    const give = roster.find((r) => r.player.position === 'MID')!.player;
    const get = theirs.find((r) => r.player.position === 'MID')!.player;
    const body = {
      receivingTeamId: other.id,
      offeredPlayerIds: [give.id],
      requestedPlayerIds: [get.id],
    };
    return { home, me, other, roster, theirs, result, body, give, get };
  }

  it('a proposal heads the list as offered before the server answers', async () => {
    const { me, other, result, body, give, get } = await setup();
    holdWrites();
    await act(async () => {
      result.current.propose.mutate(body);
    });
    await waitFor(() => expect(trades()[0]?.status).toBe('offered'));
    expect(trades()[0]).toMatchObject({
      offeringTeam: { id: me.id },
      receivingTeam: { id: other.id },
      offered: [{ id: give.id }],
      requested: [{ id: get.id }],
    });
    releaseWrites();
    await settle();
    expect(haptics.success).toHaveBeenCalledTimes(1);
  });

  it('a refused proposal restores the list exactly', async () => {
    const { result, body } = await setup();
    const before = cacheCopy();
    fail('post', '/trades', 422, 'trade-shape');
    await act(async () => {
      await result.current.propose.mutateAsync(body).catch(() => undefined);
    });
    expect(cacheCopy()).toEqual(before);
    expect(haptics.error).toHaveBeenCalledTimes(1);
  });

  it('a defender for a midfielder breaks the trade shape and is never shown or sent', async () => {
    const { result, body, roster } = await setup();
    const def = roster.find((r) => r.player.position === 'DEF')!.player;
    const before = cacheCopy();
    let error: unknown;
    await act(async () => {
      await result.current.propose
        .mutateAsync({ ...body, offeredPlayerIds: [def.id] })
        .catch((e: unknown) => (error = e));
    });
    expect(error).toBeInstanceOf(RuleError);
    expect((error as RuleError).problems.join(' ')).toMatch(/Positions must match/);
    expect(writes).toEqual([]);
    expect(cacheCopy()).toEqual(before);
  });

  it('two for one is refused: both sides move the same number', async () => {
    const { result, body, roster } = await setup();
    const mids = roster.filter((r) => r.player.position === 'MID').map((r) => r.player.id);
    let error: unknown;
    await act(async () => {
      await result.current.propose
        .mutateAsync({ ...body, offeredPlayerIds: mids.slice(0, 2) })
        .catch((e: unknown) => (error = e));
    });
    expect(error).toBeInstanceOf(RuleError);
    expect(writes).toEqual([]);
  });

  it('past the trade deadline nothing is shown or sent', async () => {
    const { home, result, body } = await setup();
    queryClient.setQueryData(queryKeys.league(L), (d: Envelope<Schemas['LeagueHome']>) => ({
      ...d,
      data: {
        ...d.data,
        settings: {
          ...d.data.settings,
          trades: { ...d.data.settings.trades, deadlineGameweek: home.currentGameweek.number - 1 },
        },
      },
    }));
    let error: unknown;
    await act(async () => {
      await result.current.propose.mutateAsync(body).catch((e: unknown) => (error = e));
    });
    expect(error).toBeInstanceOf(RuleError);
    expect(writes).toEqual([]);
  });

  for (const [kind, status] of [
    ['accept', 'accepted'],
    ['decline', 'declined'],
    ['withdraw', 'withdrawn'],
  ] as const) {
    it(`${kind} shows the trade ${status} at once; a refusal restores it exactly`, async () => {
      const { result } = await setup();
      const trade = trades().find((t) => t.viewerActions.includes(kind));
      if (!trade) throw new Error('The mock league no longer has the data this test needs.');
      const before = cacheCopy();
      fail('post', `/trades/:tradeId/${kind}`, 409, 'trade-not-open');
      holdWrites();
      await act(async () => {
        result.current.respond.mutate({ tradeId: trade.id, action: { kind } });
      });
      await waitFor(() =>
        expect(trades().find((t) => t.id === trade.id)).toMatchObject({
          status,
          viewerActions: [],
        }),
      );
      releaseWrites();
      await settle();
      expect(cacheCopy()).toEqual(before);
      expect(haptics.error).toHaveBeenCalledTimes(1);
    });
  }

  it('an action the trade no longer offers is refused before it shows', async () => {
    const { result } = await setup();
    const trade = trades().find((t) => !t.viewerActions.includes('accept'));
    if (!trade) throw new Error('The mock league no longer has the data this test needs.');
    let error: unknown;
    await act(async () => {
      await result.current.respond
        .mutateAsync({ tradeId: trade.id, action: { kind: 'accept' } })
        .catch((e: unknown) => (error = e));
    });
    expect(error).toBeInstanceOf(RuleError);
    expect(writes).toEqual([]);
  });
});

// ---------------------------------------------------------------- watchlist

describe('watchlist star', () => {
  it('stars at once with a light tap; a refusal restores the list exactly', async () => {
    await seedLeague();
    const [player] = await seedPlayers();
    await queryClient.fetchQuery({
      queryKey: watchlistKey(L),
      queryFn: () =>
        send(api.GET('/v1/leagues/{leagueId}/watchlist', { params: { path: { leagueId: L } } })),
    });
    const watched = () =>
      queryClient
        .getQueryData<Envelope<Schemas['WatchlistEntry'][]>>(watchlistKey(L))!
        .data.map((e) => e.player.id);
    const { result } = await renderHook(() => useToggleWatch(L), { wrapper });
    const before = cacheCopy();
    fail('put', '/watchlist/:playerId', 500, 'internal');
    holdWrites();
    await act(async () => {
      result.current.mutate({ player: player!.player, watch: true });
    });
    await waitFor(() => expect(watched()[0]).toBe(player!.player.id));
    expect(haptics.light).toHaveBeenCalledTimes(1);
    releaseWrites();
    await settle();
    expect(cacheCopy()).toEqual(before);
    expect(haptics.error).toHaveBeenCalledTimes(1);
  });
});
