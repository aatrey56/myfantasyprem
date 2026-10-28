import { send, type Schemas } from '@myfantasyprem/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from './client';
import { rollback, snapshot } from './optimistic';
import { useScreenFocus } from './screen-focus';
import { haptics } from '@/lib/haptics';

type Entry = Schemas['TradeBlockEntry'];
type Envelope = { data: Entry[]; meta: Schemas['Meta'] };

/** The league's trade block: under the league, so a league refresh covers it. */
export const tradeBlockKey = (leagueId: string) => ['league', leagueId, 'trade-block'] as const;

/** The league's trade block, oldest first. */
export function useTradeBlock(leagueId: string, { enabled = true }: { enabled?: boolean } = {}) {
  const { subscribed } = useScreenFocus(tradeBlockKey(leagueId));
  return useQuery({
    queryKey: tradeBlockKey(leagueId),
    queryFn: () =>
      send(api.GET('/v1/leagues/{leagueId}/trade-block', { params: { path: { leagueId } } })),
    enabled,
    subscribed,
  });
}

/** Block entries by player id, for the marks on rows and cards. */
export function useBlockByPlayer(leagueId: string): ReadonlyMap<string, Entry> {
  const q = useTradeBlock(leagueId);
  return new Map((q.data?.data ?? []).map((e) => [e.player.id, e]));
}

/**
 * Puts one of my players on the block (with a note and wanted positions)
 * or takes him off, optimistically; a refusal puts the block back.
 */
export function useSetTradeBlock(leagueId: string) {
  const queryClient = useQueryClient();
  const key = tradeBlockKey(leagueId);
  return useMutation({
    mutationFn: async (
      v:
        | {
            player: Schemas['PlayerRef'];
            team: Schemas['TeamRef'];
            on: true;
            note?: string;
            wanted?: Schemas['Position'][];
          }
        | { player: Schemas['PlayerRef']; on: false },
    ) => {
      const path = { path: { leagueId, playerId: v.player.id } };
      if (v.on) {
        return send(
          api.PUT('/v1/leagues/{leagueId}/trade-block/{playerId}', {
            params: path,
            body: { note: v.note ?? '', wanted: v.wanted ?? [] },
          }),
        );
      }
      const { response, error } = await api.DELETE(
        '/v1/leagues/{leagueId}/trade-block/{playerId}',
        {
          params: path,
        },
      );
      if (!response.ok) await send(Promise.resolve({ response, error }));
      return null;
    },
    onMutate: async (v) => {
      haptics.light();
      const saved = await snapshot(queryClient, { queryKey: key, exact: true });
      const before = queryClient.getQueryData<Envelope>(key);
      if (before) {
        const existing = before.data.find((e) => e.player.id === v.player.id);
        const rest = before.data.filter((e) => e.player.id !== v.player.id);
        queryClient.setQueryData<Envelope>(key, {
          ...before,
          data: v.on
            ? [
                ...rest,
                {
                  player: v.player,
                  team: v.team,
                  note: v.note ?? '',
                  wanted: v.wanted ?? [],
                  addedAt: existing?.addedAt ?? new Date().toISOString(),
                },
              ]
            : rest,
        });
      }
      return { saved };
    },
    onError: (_e, _v, context) => rollback(queryClient, context?.saved),
    onSettled: () => void queryClient.invalidateQueries({ queryKey: key }),
  });
}
