import { send, type Schemas } from '@myfantasyprem/api-client';
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query';

import { api, idempotencyKey } from './client';
import { queryKeys } from './hooks';
import {
  applyRosterMove,
  cachedHome,
  cachedTeam,
  findPlayer,
  leagueQueries,
  othersPending,
  rollback,
  RuleError,
  snapshot,
  tempId,
  updateTrades,
  type Snapshot,
} from './optimistic';
import { useScreenFocus } from './screen-focus';
import { haptics } from '@/lib/haptics';
import { checkTrade, tradeDeadlinePassed } from '@/lib/trade';
import { reorderClaims, rosterMoveProblem } from '@/lib/waivers';

type ClaimsEnvelope = { data: Schemas['WaiverClaim'][]; meta: Schemas['Meta'] };

const path = (leagueId: string) => ({ path: { leagueId } });
const withKey = (leagueId: string) => ({
  ...path(leagueId),
  header: { 'Idempotency-Key': idempotencyKey() },
});

/** Throws the problem for a non-2xx response that has no body (204 endpoints). */
async function expectNoContent(
  pending: Promise<{ response: Response; error?: unknown }>,
): Promise<void> {
  const { response, error } = await pending;
  if (!response.ok) await send(Promise.resolve({ response, error }));
}

/**
 * A roster changed (free agent, trade): every view of who owns whom is
 * out of date. Matchups and past lineups are history and stay.
 */
function invalidateRosters(queryClient: QueryClient, leagueId: string) {
  const prefixes = [
    'players',
    'player',
    'team',
    'lineup',
    'matchup-preview',
    'transactions',
    'trades',
    'waivers',
    // A player who left his team came off the block; his news moved with him.
    'trade-block',
    'news',
  ];
  void queryClient.invalidateQueries({
    predicate: (q) =>
      q.queryKey[0] === 'league' &&
      q.queryKey[1] === leagueId &&
      prefixes.includes(String(q.queryKey[2])),
  });
  void queryClient.invalidateQueries({ queryKey: queryKeys.league(leagueId), exact: true });
}

// ---------------------------------------------------------------- waivers

/** Priority order and the next run. */
export function useWaiverState(leagueId: string) {
  const { subscribed } = useScreenFocus(queryKeys.waivers(leagueId));
  return useQuery({
    queryKey: queryKeys.waivers(leagueId),
    queryFn: () => send(api.GET('/v1/leagues/{leagueId}/waivers', { params: path(leagueId) })),
    subscribed,
  });
}

/** My claims: pending (in my preference order) or processed (newest first). */
export function useWaiverClaims(leagueId: string, status: 'pending' | 'processed' = 'pending') {
  const { subscribed } = useScreenFocus(queryKeys.claims(leagueId, status));
  return useQuery({
    queryKey: queryKeys.claims(leagueId, status),
    queryFn: () =>
      send(
        api.GET('/v1/leagues/{leagueId}/waivers/claims', {
          params: { ...path(leagueId), query: { status } },
        }),
      ),
    subscribed,
  });
}

/**
 * Place, cancel and reorder all rewrite my pending claims, so they share a
 * mutation scope (they run one at a time, in order) and only the last one
 * to settle writes the cache; it then refetches, so a reorder answer that
 * still lists a claim cancelled after it can never bring that claim back.
 */
const claimsMutation = (leagueId: string) => ({
  mutationKey: ['claims', 'edit', leagueId],
  scope: { id: `claims:${leagueId}` },
});

/** A claim shown before the server has created it: it cannot be moved or cancelled yet. */
export const isOptimisticClaim = (claimId: string) => claimId.includes('_optimistic_');

/**
 * Places a waiver claim, optimistically: it joins the end of my pending
 * claims at once. The rules (same position, a player on waivers, not twice)
 * are checked first; a refusal rolls the list back.
 */
export function useCreateClaim(leagueId: string) {
  const queryClient = useQueryClient();
  const key = queryKeys.claims(leagueId, 'pending');
  const { mutationKey } = claimsMutation(leagueId);
  return useMutation({
    ...claimsMutation(leagueId),
    mutationFn: (body: Schemas['WaiverClaimCreate']) =>
      send(api.POST('/v1/leagues/{leagueId}/waivers/claims', { params: withKey(leagueId), body })),
    onMutate: async (body) => {
      const saved = await snapshot(queryClient, { queryKey: key, exact: true });
      const home = cachedHome(queryClient, leagueId);
      const team = home?.viewer.team;
      const roster = team && cachedTeam(queryClient, leagueId, team.id)?.roster;
      const add = findPlayer(queryClient, leagueId, body.addPlayerId);
      const list = queryClient.getQueryData<ClaimsEnvelope>(key);
      if (!home || !team || !roster || !add) return { saved };
      const problem = rosterMoveProblem('claim', roster, add, body.dropPlayerId, list?.data);
      if (problem) throw new RuleError([problem]);
      const drop = roster.find((r) => r.player.id === body.dropPlayerId)!.player;
      const waivers = queryClient.getQueryData<{ data: Schemas['WaiverState'] }>(
        queryKeys.waivers(leagueId),
      )?.data;
      if (list) {
        const now = new Date().toISOString();
        queryClient.setQueryData<ClaimsEnvelope>(key, {
          ...list,
          data: [
            ...list.data,
            {
              id: tempId('wc'),
              team,
              add: add.player,
              drop,
              order: list.data.length + 1,
              status: 'pending',
              failureReason: null,
              gameweek: waivers?.nextRunGameweek ?? home.currentGameweek.number + 1,
              processAt: waivers?.nextRunAt ?? now,
              processedAt: null,
              createdAt: now,
            },
          ],
        });
      }
      return { saved };
    },
    onError: (_e, _v, ctx) => {
      if (othersPending(queryClient, mutationKey)) haptics.error();
      else rollback(queryClient, ctx?.saved);
    },
    onSuccess: (envelope) => {
      haptics.success();
      if (othersPending(queryClient, mutationKey)) return;
      queryClient.setQueryData<ClaimsEnvelope>(key, (d) =>
        d
          ? {
              ...d,
              data: [
                ...d.data.filter((c) => !isOptimisticClaim(c.id) && c.id !== envelope.data.id),
                envelope.data,
              ],
            }
          : d,
      );
    },
    onSettled: () => {
      if (othersPending(queryClient, mutationKey)) return;
      void queryClient.invalidateQueries({ queryKey: key });
    },
  });
}

/** Cancels a pending claim: it leaves the list at once and comes back if the server refuses. */
export function useCancelClaim(leagueId: string) {
  const queryClient = useQueryClient();
  const key = queryKeys.claims(leagueId, 'pending');
  const { mutationKey } = claimsMutation(leagueId);
  return useMutation({
    ...claimsMutation(leagueId),
    mutationFn: (claimId: string) =>
      expectNoContent(
        api.DELETE('/v1/leagues/{leagueId}/waivers/claims/{claimId}', {
          params: { path: { leagueId, claimId } },
        }),
      ),
    onMutate: async (claimId) => {
      if (isOptimisticClaim(claimId)) throw new RuleError(['That claim is still being placed.']);
      const saved = await snapshot(queryClient, { queryKey: key, exact: true });
      const previous = queryClient.getQueryData<ClaimsEnvelope>(key);
      if (previous) {
        const rest = previous.data.filter((c) => c.id !== claimId);
        queryClient.setQueryData<ClaimsEnvelope>(key, {
          ...previous,
          data: reorderClaims(
            rest,
            rest.map((c) => c.id),
          ),
        });
      }
      return { saved };
    },
    onError: (_e, _v, ctx) => {
      if (othersPending(queryClient, mutationKey)) haptics.error();
      else rollback(queryClient, ctx?.saved);
    },
    onSettled: () => {
      if (othersPending(queryClient, mutationKey)) return;
      void queryClient.invalidateQueries({ queryKey: key });
    },
  });
}

/** Reorders my pending claims at once and rolls back if the server refuses. */
export function useReorderClaims(leagueId: string) {
  const queryClient = useQueryClient();
  const key = queryKeys.claims(leagueId, 'pending');
  const { mutationKey } = claimsMutation(leagueId);
  return useMutation({
    ...claimsMutation(leagueId),
    mutationFn: (claimIds: string[]) =>
      send(
        api.PUT('/v1/leagues/{leagueId}/waivers/claim-order', {
          params: path(leagueId),
          body: { claimIds },
        }),
      ),
    onMutate: async (claimIds) => {
      const saved = await snapshot(queryClient, { queryKey: key, exact: true });
      const previous = queryClient.getQueryData<ClaimsEnvelope>(key);
      if (previous) {
        // The server wants every pending claim, once each.
        const ids = previous.data.map((c) => c.id);
        const same =
          claimIds.length === ids.length &&
          new Set(claimIds).size === ids.length &&
          ids.every((id) => claimIds.includes(id));
        if (!same || ids.some(isOptimisticClaim)) {
          throw new RuleError(['Your claims changed: try again once the list has refreshed.']);
        }
        queryClient.setQueryData<ClaimsEnvelope>(key, {
          ...previous,
          data: reorderClaims(previous.data, claimIds),
        });
      }
      return { saved };
    },
    onError: (_e, _v, ctx) => {
      if (othersPending(queryClient, mutationKey)) haptics.error();
      else rollback(queryClient, ctx?.saved);
    },
    onSuccess: (envelope) => {
      if (othersPending(queryClient, mutationKey)) return;
      queryClient.setQueryData(key, envelope);
    },
    onSettled: (_d, error) => {
      if (othersPending(queryClient, mutationKey)) return;
      if (error) void queryClient.invalidateQueries({ queryKey: key });
    },
  });
}

/**
 * Adds a free agent and drops a same-position player of mine,
 * optimistically: my squad, my open lineups and every player list show the
 * move at once (the arrival takes the departed player's slot). FPL Draft's
 * rules are checked first; a refusal puts every cache back as it was.
 */
export function useAddFreeAgent(leagueId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: Schemas['FreeAgentMoveRequest']) =>
      send(
        api.POST('/v1/leagues/{leagueId}/free-agent-moves', { params: withKey(leagueId), body }),
      ),
    onMutate: async (body): Promise<{ saved: Snapshot }> => {
      const saved = await snapshot(
        queryClient,
        leagueQueries(leagueId, 'team', 'lineup', 'players', 'player'),
      );
      const team = cachedHome(queryClient, leagueId)?.viewer.team;
      const roster = team && cachedTeam(queryClient, leagueId, team.id)?.roster;
      const add = findPlayer(queryClient, leagueId, body.addPlayerId);
      if (!team || !roster || !add) return { saved };
      const problem = rosterMoveProblem('add', roster, add, body.dropPlayerId);
      if (problem) throw new RuleError([problem]);
      applyRosterMove(queryClient, leagueId, team, add, body.dropPlayerId);
      return { saved };
    },
    onError: (_e, _v, ctx) => rollback(queryClient, ctx?.saved),
    onSuccess: () => haptics.success(),
    onSettled: () => invalidateRosters(queryClient, leagueId),
  });
}

// ---------------------------------------------------------------- trades

export const TRADES_OPEN_POLL_MS = 30_000;
export const TRADES_IDLE_POLL_MS = 60_000;

/** Every trade I can see, newest first (the inbox splits them into buckets). */
export function useTrades(leagueId: string) {
  const { subscribed, focused } = useScreenFocus(queryKeys.trades(leagueId));
  return useInfiniteQuery({
    queryKey: queryKeys.trades(leagueId),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      send(
        api.GET('/v1/leagues/{leagueId}/trades', {
          params: { ...path(leagueId), query: { cursor: pageParam, limit: 50 } },
        }),
      ),
    getNextPageParam: (last) => last.meta.nextCursor ?? undefined,
    // Review windows close and offers get answered: refresh often while
    // something is open; otherwise still check now and then, so a new
    // offer shows up without a reload (only while the screen is focused).
    refetchInterval: (q) =>
      !focused
        ? false
        : q.state.data?.pages.some((p) =>
              p.data.some((t) => ['offered', 'accepted', 'in_review'].includes(t.status)),
            )
          ? TRADES_OPEN_POLL_MS
          : TRADES_IDLE_POLL_MS,
    subscribed,
  });
}

/**
 * Sends a trade offer, optimistically: it heads my trade list as offered at
 * once. FPL Draft's trade shape (same number and positions each way) and
 * the league's trade deadline are checked first.
 */
export function useProposeTrade(leagueId: string) {
  const queryClient = useQueryClient();
  const key = queryKeys.trades(leagueId);
  return useMutation({
    mutationFn: (body: Schemas['TradeCreate']) =>
      send(api.POST('/v1/leagues/{leagueId}/trades', { params: withKey(leagueId), body })),
    onMutate: async (body) => {
      const saved = await snapshot(queryClient, { queryKey: key, exact: true });
      const home = cachedHome(queryClient, leagueId);
      if (!home) return { saved };
      if (tradeDeadlinePassed(home.settings.trades.deadlineGameweek, home.currentGameweek.number)) {
        throw new RuleError(['The trade deadline has passed.']);
      }
      const refs = (ids: string[]) =>
        ids.map((id) => findPlayer(queryClient, leagueId, id)?.player);
      const offered = refs(body.offeredPlayerIds);
      const requested = refs(body.requestedPlayerIds);
      const known = <T>(xs: (T | undefined)[]): xs is T[] => xs.every(Boolean);
      if (!known(offered) || !known(requested)) return { saved };
      const check = checkTrade(offered, requested, body.message ?? '');
      if (!check.ok) throw new RuleError(check.problems);
      const offeringTeam = home.viewer.team;
      const receivingTeam = home.standings.find((r) => r.team.id === body.receivingTeamId)?.team;
      if (!offeringTeam || !receivingTeam) return { saved };
      const now = new Date().toISOString();
      updateTrades(queryClient, leagueId, (trades, page) =>
        page > 0
          ? trades
          : [
              {
                id: tempId('trd'),
                status: 'offered',
                offeringTeam,
                receivingTeam,
                offered,
                requested,
                message: body.message ?? null,
                createdAt: now,
                expiresAt: now,
                respondedAt: null,
                review: null,
                processedAt: null,
                effectiveGameweek: null,
                failureReason: null,
                viewerActions: [],
              },
              ...trades,
            ],
      );
      return { saved };
    },
    onError: (_e, _v, ctx) => rollback(queryClient, ctx?.saved),
    onSuccess: () => haptics.success(),
    onSettled: () => queryClient.invalidateQueries({ queryKey: key }),
  });
}

/** What an answer makes of a trade before the server confirms it. */
const OPTIMISTIC_STATUS: Partial<Record<TradeResponse['kind'], Schemas['TradeStatus']>> = {
  accept: 'accepted',
  decline: 'declined',
  withdraw: 'withdrawn',
};

export type TradeResponse =
  | { kind: 'accept' | 'decline' | 'withdraw' }
  | { kind: 'vote'; vote: Schemas['TradeVote'] }
  | { kind: 'review'; decision: 'approve' | 'veto' };

/**
 * Accept, decline, withdraw, vote, or review (commissioner) a trade. The
 * first three show at once (the trade's new status, no more actions) and
 * only when the trade still offers that action; a refusal rolls back.
 */
export function useRespondToTrade(leagueId: string) {
  const queryClient = useQueryClient();
  const key = queryKeys.trades(leagueId);
  return useMutation({
    mutationFn: ({ tradeId, action }: { tradeId: string; action: TradeResponse }) => {
      const params = {
        path: { leagueId, tradeId },
        header: { 'Idempotency-Key': idempotencyKey() },
      };
      switch (action.kind) {
        case 'accept':
          return send(api.POST('/v1/leagues/{leagueId}/trades/{tradeId}/accept', { params }));
        case 'decline':
          return send(api.POST('/v1/leagues/{leagueId}/trades/{tradeId}/decline', { params }));
        case 'withdraw':
          return send(api.POST('/v1/leagues/{leagueId}/trades/{tradeId}/withdraw', { params }));
        case 'vote':
          return send(
            api.POST('/v1/leagues/{leagueId}/trades/{tradeId}/votes', {
              params,
              body: { vote: action.vote },
            }),
          );
        case 'review':
          return send(
            api.POST('/v1/leagues/{leagueId}/trades/{tradeId}/review', {
              params,
              body: { decision: action.decision },
            }),
          );
      }
    },
    onMutate: async ({ tradeId, action }) => {
      const status = OPTIMISTIC_STATUS[action.kind];
      if (!status) return { saved: [] as Snapshot };
      const saved = await snapshot(queryClient, { queryKey: key, exact: true });
      const trade = queryClient
        .getQueryData<{ pages: { data: Schemas['Trade'][] }[] }>(key)
        ?.pages.flatMap((p) => p.data)
        .find((t) => t.id === tradeId);
      if (!trade) return { saved };
      if (!trade.viewerActions.includes(action.kind)) {
        throw new RuleError([`You can't ${action.kind} this trade any more.`]);
      }
      const now = new Date().toISOString();
      updateTrades(queryClient, leagueId, (trades) =>
        trades.map((t) =>
          t.id === tradeId ? { ...t, status, respondedAt: now, viewerActions: [] } : t,
        ),
      );
      return { saved };
    },
    onError: (_e, _v, ctx) => {
      rollback(queryClient, ctx?.saved);
      void queryClient.invalidateQueries({ queryKey: key });
    },
    onSuccess: (envelope) => {
      haptics.success();
      updateTrades(queryClient, leagueId, (trades) =>
        trades.map((t) => (t.id === envelope.data.id ? envelope.data : t)),
      );
      if (envelope.data.status === 'processed') invalidateRosters(queryClient, leagueId);
      else {
        void queryClient.invalidateQueries({ queryKey: queryKeys.trades(leagueId) });
        // A veto is league activity too.
        if (envelope.data.status === 'vetoed') {
          void queryClient.invalidateQueries({
            queryKey: ['league', leagueId, 'transactions'],
          });
        }
      }
    },
  });
}
