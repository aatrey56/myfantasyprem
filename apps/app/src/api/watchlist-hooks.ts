import { send, type Schemas } from '@myfantasyprem/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from './client';
import { rollback, snapshot } from './optimistic';
import { useScreenFocus } from './screen-focus';
import { haptics } from '@/lib/haptics';

type Envelope = { data: Schemas['WatchlistEntry'][]; meta: Schemas['Meta'] };

/** The watchlist's query key: under the league, so a league refresh covers it. */
export const watchlistKey = (leagueId: string) => ['league', leagueId, 'watchlist'] as const;

/** My watchlist in the league, newest first. */
export function useWatchlist(leagueId: string, { enabled = true }: { enabled?: boolean } = {}) {
  const { subscribed } = useScreenFocus(watchlistKey(leagueId));
  return useQuery({
    queryKey: watchlistKey(leagueId),
    queryFn: () =>
      send(api.GET('/v1/leagues/{leagueId}/watchlist', { params: { path: { leagueId } } })),
    enabled,
    subscribed,
  });
}

/** The watched player ids, for stars on rows and cards. */
export function useWatchedIds(leagueId: string): ReadonlySet<string> {
  const q = useWatchlist(leagueId);
  return new Set(q.data?.data.map((e) => e.player.id) ?? []);
}

/**
 * Stars or unstars a player, optimistically: the star flips at once and
 * flips back if the server says no. The watchlist filter's list refreshes
 * once it settles.
 */
export function useToggleWatch(leagueId: string) {
  const queryClient = useQueryClient();
  const key = watchlistKey(leagueId);
  return useMutation({
    mutationFn: async ({ player, watch }: { player: Schemas['PlayerRef']; watch: boolean }) => {
      const params = { params: { path: { leagueId, playerId: player.id } } };
      if (watch) {
        await send(api.PUT('/v1/leagues/{leagueId}/watchlist/{playerId}', params));
        return;
      }
      const { response, error } = await api.DELETE(
        '/v1/leagues/{leagueId}/watchlist/{playerId}',
        params,
      );
      if (!response.ok) await send(Promise.resolve({ response, error }));
    },
    onMutate: async ({ player, watch }) => {
      haptics.light();
      const saved = await snapshot(queryClient, { queryKey: key, exact: true });
      const before = queryClient.getQueryData<Envelope>(key);
      if (before) {
        const rest = before.data.filter((e) => e.player.id !== player.id);
        queryClient.setQueryData<Envelope>(key, {
          ...before,
          data: watch ? [{ player, addedAt: new Date().toISOString() }, ...rest] : rest,
        });
      }
      return { saved };
    },
    onError: (_e, _v, context) => rollback(queryClient, context?.saved),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: key });
      void queryClient.invalidateQueries({
        predicate: (q) =>
          q.queryKey[0] === 'league' &&
          q.queryKey[1] === leagueId &&
          q.queryKey[2] === 'players' &&
          Boolean((q.queryKey[3] as { watchlist?: boolean } | undefined)?.watchlist),
      });
    },
  });
}
