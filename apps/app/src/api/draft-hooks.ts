import { send, type Schemas } from '@myfantasyprem/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, idempotencyKey } from './client';
import { queryKeys } from './hooks';
import { useRealtimeConnected } from './realtime';
import { useScreenFocus } from './screen-focus';

type DraftEnvelope = { data: Schemas['Draft']; meta: Schemas['Meta'] };
type QueueEnvelope = { data: Schemas['DraftQueue']; meta: Schemas['Meta'] };

/**
 * How often the draft room polls. Without a realtime socket (the mocks
 * serve none; docs/realtime.md) a running clock polls every 3 s so a pick
 * shows up almost at once, and a paused or scheduled draft every 15 s.
 * While the socket is up the draft.* events refresh the board and polling
 * is only the safety net (15 s).
 */
export const DRAFT_LIVE_POLL_MS = 3_000;
export const DRAFT_IDLE_POLL_MS = 15_000;

export function draftPollInterval(
  status: Schemas['DraftStatus'] | undefined,
  realtime = false,
): number | false {
  if (status === 'in_progress') return realtime ? DRAFT_IDLE_POLL_MS : DRAFT_LIVE_POLL_MS;
  if (status === 'paused' || status === 'scheduled') return DRAFT_IDLE_POLL_MS;
  return false;
}

const path = (leagueId: string) => ({ path: { leagueId } });

export function useDraft(leagueId: string) {
  const { subscribed, focused } = useScreenFocus(queryKeys.draft(leagueId));
  const realtime = useRealtimeConnected(leagueId);
  return useQuery({
    queryKey: queryKeys.draft(leagueId),
    queryFn: () => send(api.GET('/v1/leagues/{leagueId}/draft', { params: path(leagueId) })),
    refetchInterval: (q) =>
      focused ? draftPollInterval(q.state.data?.data.status, realtime) : false,
    subscribed,
  });
}

/** The caller's queue. Refetched with each board change so drafted players drop out. */
export function useDraftQueue(leagueId: string, enabled = true) {
  const { subscribed } = useScreenFocus(queryKeys.draftQueue(leagueId));
  return useQuery({
    queryKey: queryKeys.draftQueue(leagueId),
    queryFn: () => send(api.GET('/v1/leagues/{leagueId}/draft/queue', { params: path(leagueId) })),
    enabled,
    subscribed,
  });
}

/**
 * Replaces the queue optimistically. Each PUT replaces the whole queue, so
 * edits run one at a time (a shared mutation scope) and only the last one
 * writes the server's answer or rolls back; an earlier answer or failure
 * while a later edit is pending would otherwise show, or resend, an older
 * queue. A failure with later edits pending refetches once they settle.
 */
export function useSetDraftQueue(leagueId: string) {
  const queryClient = useQueryClient();
  const key = queryKeys.draftQueue(leagueId);
  const mutationKey = ['draftQueue', 'set', leagueId];
  const othersPending = () => queryClient.isMutating({ mutationKey }) > 1;
  return useMutation({
    mutationKey,
    scope: { id: `draft-queue:${leagueId}` },
    mutationFn: (players: Schemas['DraftQueueItem'][]) =>
      send(
        api.PUT('/v1/leagues/{leagueId}/draft/queue', {
          params: path(leagueId),
          body: { playerIds: players.map((p) => p.player.id) },
        }),
      ),
    onMutate: async (players) => {
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<QueueEnvelope>(key);
      if (previous) {
        queryClient.setQueryData<QueueEnvelope>(key, {
          ...previous,
          data: { ...previous.data, players },
        });
      }
      return { previous };
    },
    onError: (_e, _v, ctx) => {
      if (othersPending()) return; // a later edit owns the cache now
      if (ctx?.previous) queryClient.setQueryData(key, ctx.previous);
      void queryClient.invalidateQueries({ queryKey: key, exact: true });
    },
    onSuccess: (envelope) => {
      if (!othersPending()) queryClient.setQueryData(key, envelope);
    },
  });
}

/** Autopick now (`enabled`) and "autopick when my clock runs out" (`onTimeout`). */
export function useSetAutopick(leagueId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: Schemas['DraftAutopickUpdate']) =>
      send(
        api.PUT('/v1/leagues/{leagueId}/draft/autopick', {
          params: path(leagueId),
          body,
        }),
      ),
    onSuccess: (envelope) => {
      queryClient.setQueryData(queryKeys.draftQueue(leagueId), envelope);
      void queryClient.invalidateQueries({ queryKey: queryKeys.draft(leagueId), exact: true });
    },
  });
}

export function useMakePick(leagueId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (playerId: string) =>
      send(
        api.POST('/v1/leagues/{leagueId}/draft/picks', {
          params: { ...path(leagueId), header: { 'Idempotency-Key': idempotencyKey() } },
          body: { playerId },
        }),
      ),
    onSuccess: (envelope) => {
      // Put the pick on the board at once; the refetch fills in the next clock.
      const key = queryKeys.draft(leagueId);
      const previous = queryClient.getQueryData<DraftEnvelope>(key);
      if (previous && !previous.data.picks.some((p) => p.id === envelope.data.id)) {
        queryClient.setQueryData<DraftEnvelope>(key, {
          ...previous,
          data: { ...previous.data, picks: [...previous.data.picks, envelope.data] },
        });
      }
      void queryClient.invalidateQueries({ queryKey: key });
    },
  });
}

/** Commissioner: start, pause or resume. */
export function useDraftControl(leagueId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (action: 'start' | 'pause' | 'resume') => {
      const options = {
        params: { ...path(leagueId), header: { 'Idempotency-Key': idempotencyKey() } },
      };
      return send(
        action === 'start'
          ? api.POST('/v1/leagues/{leagueId}/draft/start', options)
          : action === 'pause'
            ? api.POST('/v1/leagues/{leagueId}/draft/pause', options)
            : api.POST('/v1/leagues/{leagueId}/draft/resume', options),
      );
    },
    onSuccess: (envelope) => {
      queryClient.setQueryData(queryKeys.draft(leagueId), envelope);
      void queryClient.invalidateQueries({ queryKey: queryKeys.league(leagueId), exact: true });
      void queryClient.invalidateQueries({ queryKey: queryKeys.myLeagues });
    },
  });
}

/** Commissioner, before the draft: start time, pick clock, order. */
export function useUpdateDraft(leagueId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: Schemas['DraftUpdate']) =>
      send(api.PATCH('/v1/leagues/{leagueId}/draft', { params: path(leagueId), body })),
    onSuccess: (envelope) => {
      queryClient.setQueryData(queryKeys.draft(leagueId), envelope);
      void queryClient.invalidateQueries({ queryKey: queryKeys.league(leagueId), exact: true });
    },
  });
}
