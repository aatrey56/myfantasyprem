import { send, type Schemas } from '@myfantasyprem/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, idempotencyKey } from './client';
import { queryKeys } from './hooks';

const leaguePath = (leagueId: string) => ({ path: { leagueId } });

/** Throws the problem for a non-2xx response that has no body (204 endpoints). */
async function expectNoContent(
  pending: Promise<{ response: Response; error?: unknown }>,
): Promise<void> {
  const { response, error } = await pending;
  if (!response.ok) await send(Promise.resolve({ response, error }));
}

export function useInvites(leagueId: string, enabled = true) {
  return useQuery({
    queryKey: queryKeys.invites(leagueId),
    queryFn: () =>
      send(api.GET('/v1/leagues/{leagueId}/invites', { params: leaguePath(leagueId) })),
    enabled,
  });
}

export function useCreateInvite(leagueId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: Schemas['InviteCreate'] = {}) =>
      send(
        api.POST('/v1/leagues/{leagueId}/invites', {
          params: { ...leaguePath(leagueId), header: { 'Idempotency-Key': idempotencyKey() } },
          body,
        }),
      ),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.invites(leagueId) }),
  });
}

export function useRevokeInvite(leagueId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (inviteId: string) =>
      expectNoContent(
        api.DELETE('/v1/leagues/{leagueId}/invites/{inviteId}', {
          params: { path: { leagueId, inviteId } },
        }),
      ),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.invites(leagueId) }),
  });
}

export function useMembers(leagueId: string) {
  return useQuery({
    queryKey: queryKeys.members(leagueId),
    queryFn: () =>
      send(api.GET('/v1/leagues/{leagueId}/members', { params: leaguePath(leagueId) })),
  });
}

export function useRemoveMember(leagueId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) =>
      expectNoContent(
        api.DELETE('/v1/leagues/{leagueId}/members/{userId}', {
          params: { path: { leagueId, userId } },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.members(leagueId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.league(leagueId) });
    },
  });
}

/** Commissioner only. The caller becomes a plain member. */
export function useTransferCommissioner(leagueId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) =>
      send(
        api.POST('/v1/leagues/{leagueId}/commissioner', {
          params: leaguePath(leagueId),
          body: { userId },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.members(leagueId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.league(leagueId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.myLeagues });
    },
  });
}

export function useLeaveLeague(leagueId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () =>
      expectNoContent(
        api.POST('/v1/leagues/{leagueId}/leave', {
          params: { ...leaguePath(leagueId), header: { 'Idempotency-Key': idempotencyKey() } },
        }),
      ),
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: ['league', leagueId] });
      void queryClient.invalidateQueries({ queryKey: queryKeys.myLeagues });
    },
  });
}

/** Commissioner settings. 409 `setting-locked` once the draft has started. */
export function useUpdateLeague(leagueId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: Schemas['LeagueUpdate']) =>
      send(api.PATCH('/v1/leagues/{leagueId}', { params: leaguePath(leagueId), body })),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.league(leagueId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.myLeagues });
    },
  });
}
