import { send, type Schemas } from '@myfantasyprem/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from './client';
import { rollback, snapshot } from './optimistic';
import { useScreenFocus } from './screen-focus';
import { haptics } from '@/lib/haptics';
import { tagsByPlayer, type PlayerTag, type TagMeta } from '@/lib/tags';

type Envelope = { data: Schemas['PlayerTagEntry'][]; meta: Schemas['Meta'] };

/** My tags in the league: under the league, so a league refresh covers it. */
export const tagsKey = (leagueId: string) => ['league', leagueId, 'tags'] as const;

/** My personal tags in the league, newest first. */
export function usePlayerTags(leagueId: string, { enabled = true }: { enabled?: boolean } = {}) {
  const { subscribed } = useScreenFocus(tagsKey(leagueId));
  return useQuery({
    queryKey: tagsKey(leagueId),
    queryFn: () => send(api.GET('/v1/leagues/{leagueId}/tags', { params: { path: { leagueId } } })),
    enabled,
    subscribed,
  });
}

/** My tags by player id, for rows and cards. */
export function useTagByPlayer(leagueId: string): ReadonlyMap<string, TagMeta> {
  return tagsByPlayer(usePlayerTags(leagueId).data?.data);
}

/**
 * Tags a player (or clears his tag with `tag: null`), optimistically. The
 * Players hub's tag filter refreshes once it settles.
 */
export function useSetTag(leagueId: string) {
  const queryClient = useQueryClient();
  const key = tagsKey(leagueId);
  return useMutation({
    mutationFn: async ({
      player,
      tag,
    }: {
      player: Schemas['PlayerRef'];
      tag: PlayerTag | null;
    }) => {
      const params = { params: { path: { leagueId, playerId: player.id } } };
      if (tag) {
        await send(api.PUT('/v1/leagues/{leagueId}/tags/{playerId}', { ...params, body: { tag } }));
        return;
      }
      const { response, error } = await api.DELETE(
        '/v1/leagues/{leagueId}/tags/{playerId}',
        params,
      );
      if (!response.ok) await send(Promise.resolve({ response, error }));
    },
    onMutate: async ({ player, tag }) => {
      haptics.light();
      const saved = await snapshot(queryClient, { queryKey: key, exact: true });
      const before = queryClient.getQueryData<Envelope>(key);
      if (before) {
        const rest = before.data.filter((e) => e.player.id !== player.id);
        queryClient.setQueryData<Envelope>(key, {
          ...before,
          data: tag ? [{ player, tag, updatedAt: new Date().toISOString() }, ...rest] : rest,
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
          Boolean((q.queryKey[3] as { tag?: string } | undefined)?.tag),
      });
    },
  });
}
