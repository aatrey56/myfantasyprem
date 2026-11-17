import { send, type Schemas } from '@myfantasyprem/api-client';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, idempotencyKey } from './client';
import { queryKeys } from './hooks';
import { useScreenFocus } from './screen-focus';

type Summary = Schemas['CommissionerMovesSummaryResponse'];

const path = (leagueId: string) => ({ path: { leagueId } });

/**
 * What the Status banner needs: how many commissioner moves the signed-in
 * member has not seen and the newest of them. The marker lives on the
 * server, so the banner follows them across devices (docs/adr/0017).
 */
export function useCommissionerMovesSummary(leagueId: string, enabled = true) {
  const { subscribed } = useScreenFocus(queryKeys.commissionerMovesSummary(leagueId));
  return useQuery({
    queryKey: queryKeys.commissionerMovesSummary(leagueId),
    queryFn: () =>
      send(
        api.GET('/v1/leagues/{leagueId}/commissioner-moves/summary', { params: path(leagueId) }),
      ),
    enabled,
    subscribed,
  });
}

/** Every commissioner move, newest first, a page at a time. */
export function useCommissionerMoves(leagueId: string) {
  const { subscribed } = useScreenFocus(queryKeys.commissionerMoves(leagueId));
  return useInfiniteQuery({
    queryKey: queryKeys.commissionerMoves(leagueId),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      send(
        api.GET('/v1/leagues/{leagueId}/commissioner-moves', {
          params: { ...path(leagueId), query: { cursor: pageParam, limit: 25 } },
        }),
      ),
    getNextPageParam: (last) => last.meta.nextCursor ?? undefined,
    subscribed,
  });
}

/**
 * Marks the commissioner's moves seen, up to `throughId` or all of them.
 * The banner goes at once; a refusal puts it back.
 */
export function useMarkCommissionerMovesSeen(leagueId: string) {
  const queryClient = useQueryClient();
  const key = queryKeys.commissionerMovesSummary(leagueId);
  return useMutation({
    mutationFn: (throughId?: string) =>
      send(
        api.POST('/v1/leagues/{leagueId}/commissioner-moves/seen', {
          params: { ...path(leagueId), header: { 'Idempotency-Key': idempotencyKey() } },
          body: throughId ? { throughId } : {},
        }),
      ),
    onMutate: async () => {
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<Summary>(key);
      if (previous) {
        queryClient.setQueryData<Summary>(key, {
          ...previous,
          data: { unseenCount: 0, latest: null },
        });
      }
      return { previous };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.previous) queryClient.setQueryData(key, ctx.previous);
    },
    onSuccess: (envelope) => {
      queryClient.setQueryData<Summary>(key, envelope);
      void queryClient.invalidateQueries({
        queryKey: queryKeys.commissionerMoves(leagueId),
        exact: true,
      });
    },
  });
}
