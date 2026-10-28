/**
 * An imported FPL Draft league (ADR 0012): its status, the commissioner's
 * re-sync and check against FPL, explanations, and the seats with their
 * claim links.
 */
import { send, type Schemas } from '@myfantasyprem/api-client';
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';

import { api, idempotencyKey } from './client';
import { queryKeys } from './hooks';

const withKey = (leagueId: string) => ({
  path: { leagueId },
  header: { 'Idempotency-Key': idempotencyKey() },
});

/** How often the status refreshes while a re-sync or check is queued or running. */
export const SYNC_POLL_MS = 2_000;

/** Everything a re-sync can change, except the sync status itself. */
const refreshLeagueData = (qc: QueryClient, leagueId: string) =>
  qc.invalidateQueries({
    queryKey: queryKeys.league(leagueId),
    predicate: (q) => q.queryKey[2] !== 'sync',
  });

/**
 * The league's FPL status; only fetched for an imported league (`enabled`).
 * Polls while a job is queued or running; when a re-sync finishes, the
 * league's data (squads, waivers, trades, lineups, standings) is refreshed.
 */
export function useLeagueSync(leagueId: string, enabled = true) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: queryKeys.sync(leagueId),
    queryFn: async () => {
      const before = qc.getQueryData<Schemas['LeagueSyncResponse']>(queryKeys.sync(leagueId));
      const envelope = await send(
        api.GET('/v1/leagues/{leagueId}/sync', { params: { path: { leagueId } } }),
      );
      if (before?.data.pending?.kind === 'resync' && !envelope.data.pending)
        void refreshLeagueData(qc, leagueId);
      return envelope;
    },
    enabled,
    refetchInterval: (q) => (q.state.data?.data.pending ? SYNC_POLL_MS : false),
  });
}

const setSync = (qc: QueryClient, leagueId: string, envelope: Schemas['LeagueSyncResponse']) =>
  qc.setQueryData(queryKeys.sync(leagueId), envelope);

/** Commissioner only: replaces the league with FPL's current state (`confirm` is always true). */
export function useResync(leagueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () =>
      send(
        api.POST('/v1/leagues/{leagueId}/sync', {
          params: withKey(leagueId),
          body: { confirm: true },
        }),
      ),
    onSuccess: (envelope) => setSync(qc, leagueId, envelope),
  });
}

/** Commissioner only: compares the league with FPL without changing anything. */
export function useCheckSync(leagueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () =>
      send(api.POST('/v1/leagues/{leagueId}/sync/check', { params: withKey(leagueId) })),
    onSuccess: (envelope) => setSync(qc, leagueId, envelope),
  });
}

export function useExplainSyncItem(leagueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Schemas['SyncExplanationInput']) =>
      send(
        api.POST('/v1/leagues/{leagueId}/sync/explanations', { params: withKey(leagueId), body }),
      ),
    onSuccess: (envelope) => setSync(qc, leagueId, envelope),
  });
}

/** Commissioner only. */
export function useSeats(leagueId: string, enabled = true) {
  return useQuery({
    queryKey: queryKeys.seats(leagueId),
    queryFn: () =>
      send(api.GET('/v1/leagues/{leagueId}/seats', { params: { path: { leagueId } } })),
    enabled,
  });
}

/** Revokes a seat's claim link and issues a new one. */
export function useResendSeatInvite(leagueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (teamId: string) =>
      send(
        api.POST('/v1/leagues/{leagueId}/seats/{teamId}/invite', {
          params: { ...withKey(leagueId), path: { leagueId, teamId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.seats(leagueId) }),
  });
}

/**
 * A single-use sign-in link (7 days) for a claimed seat's manager, who has
 * no other way back in (docs/adr/0013). The code is only in the response.
 */
export function useCreateSeatSignInLink(leagueId: string) {
  return useMutation({
    mutationFn: (teamId: string) =>
      send(
        api.POST('/v1/leagues/{leagueId}/seats/{teamId}/signin-link', {
          params: { path: { leagueId, teamId } },
        }),
      ),
  });
}
