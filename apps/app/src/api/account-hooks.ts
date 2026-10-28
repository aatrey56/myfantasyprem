import { send, type Schemas } from '@myfantasyprem/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, idempotencyKey } from './client';
import { queryKeys } from './hooks';
import { useScreenFocus } from './screen-focus';
import { useAuth } from '@/auth/AuthProvider';
import { getAppleCredential, getGoogleCredential } from '@/auth/providers';
import { drainPendingRevocations, queueRevocation } from '@/auth/revocation';
import { tokens } from '@/auth/session';

// ---------------------------------------------------------------- sign-in

/** Every sign-in ends the same way: keep the session. */
function useSessionMutation<TInput>(
  request: (input: TInput) => Promise<{ data: Schemas['Session'] }>,
) {
  const { signIn } = useAuth();
  return useMutation({
    mutationFn: request,
    onSuccess: (envelope) => signIn(envelope.data),
  });
}

export function useAppleSignIn() {
  return useSessionMutation(async () =>
    send(api.POST('/v1/auth/apple', { body: await getAppleCredential() })),
  );
}

export function useGoogleSignIn() {
  return useSessionMutation(async () =>
    send(api.POST('/v1/auth/google', { body: await getGoogleCredential() })),
  );
}

/** Emails a sign-in link and code; `next` rides in the link. */
export function useStartEmailSignIn() {
  return useMutation({
    mutationFn: (body: Schemas['EmailSignInRequest']) => send(api.POST('/v1/auth/email', { body })),
  });
}

export function useVerifyEmailSignIn() {
  return useSessionMutation((token: string) =>
    send(api.POST('/v1/auth/email/verify', { body: { token } })),
  );
}

/** Signs in with the 6-digit code from the latest sign-in email. */
export function useVerifyEmailCode() {
  return useSessionMutation((body: Schemas['EmailCodeVerifyRequest']) =>
    send(api.POST('/v1/auth/email/code', { body })),
  );
}

/**
 * Signs out at once and revokes the session on the server in the
 * background: a failed logout (429, 5xx, offline) is retried with backoff,
 * then kept and retried on the next start (auth/revocation.ts), so the
 * refresh token never outlives an apparent sign-out.
 */
export function useSignOut() {
  const { signOut } = useAuth();
  return useMutation({
    mutationFn: async () => {
      const session = tokens.get();
      // Stored before the credentials go, so a crash cannot lose it.
      if (session) await queueRevocation(session).catch(() => undefined);
      await signOut();
      void drainPendingRevocations();
    },
  });
}

// ---------------------------------------------------------------- me

export function useMe() {
  const { setUser } = useAuth();
  return useQuery({
    queryKey: queryKeys.me,
    queryFn: async () => {
      const envelope = await send(api.GET('/v1/me'));
      setUser(envelope.data);
      return envelope;
    },
  });
}

export function useUpdateMe() {
  const queryClient = useQueryClient();
  const { setUser } = useAuth();
  return useMutation({
    mutationFn: (body: Schemas['UserUpdate']) => send(api.PATCH('/v1/me', { body })),
    onSuccess: (envelope) => {
      setUser(envelope.data);
      queryClient.setQueryData(queryKeys.me, envelope);
    },
  });
}

/** Deletes the account; the server revokes every session, so sign out locally after. */
export function useDeleteAccount() {
  const { signOut } = useAuth();
  return useMutation({
    mutationFn: async () => {
      const { response, error } = await api.DELETE('/v1/me');
      if (!response.ok) {
        // Reuse send()'s error mapping for the problem body.
        await send(Promise.resolve({ response, error }));
      }
    },
    onSuccess: () => signOut(),
  });
}

// ---------------------------------------------------------------- my leagues

/** The home screen's league list. Polls while a draft or live gameweek is on. */
export function useMyLeagues(enabled = true) {
  const { subscribed } = useScreenFocus(queryKeys.myLeagues);
  return useQuery({
    queryKey: queryKeys.myLeagues,
    queryFn: () => send(api.GET('/v1/me/leagues')),
    enabled,
    refetchInterval: (q) => {
      const rows = q.state.data?.data ?? [];
      if (rows.some((r) => r.league.status === 'drafting')) return 15_000;
      if (rows.some((r) => r.currentMatchup && q.state.data?.meta.scoreState === 'live'))
        return 30_000;
      return false;
    },
    subscribed,
  });
}

export function useCreateLeague() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: Schemas['LeagueCreate']) =>
      send(
        api.POST('/v1/leagues', {
          body,
          params: { header: { 'Idempotency-Key': idempotencyKey() } },
        }),
      ),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.myLeagues }),
  });
}

/** Public invite preview ("Join The Sunday Draft?"). */
export function useInvitePreview(code: string | undefined) {
  return useQuery({
    queryKey: queryKeys.invite(code ?? ''),
    queryFn: () => send(api.GET('/v1/invites/{code}', { params: { path: { code: code! } } })),
    enabled: Boolean(code),
    staleTime: 30_000,
  });
}

export function useAcceptInvite(code: string) {
  const queryClient = useQueryClient();
  return useMutation({
    /** A claim link takes no name (the seat keeps its FPL name): pass undefined. */
    mutationFn: (teamName: string | undefined) =>
      send(
        api.POST('/v1/invites/{code}/accept', {
          params: { path: { code }, header: { 'Idempotency-Key': idempotencyKey() } },
          body: teamName === undefined ? {} : { teamName },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.myLeagues });
      void queryClient.invalidateQueries({ queryKey: queryKeys.invite(code) });
    },
  });
}

/**
 * A signed-out person claims a seat with its claim link and only a name:
 * the server makes the account, gives it the seat and signs it in
 * (docs/adr/0013). The code goes in the body, never in a logged URL.
 */
export function useClaimSeatSignIn(code: string) {
  const queryClient = useQueryClient();
  const { signIn } = useAuth();
  return useMutation({
    mutationFn: (displayName: string) =>
      send(api.POST('/v1/auth/claim', { body: { code, displayName } })),
    onSuccess: async (envelope) => {
      await signIn(envelope.data.session);
      void queryClient.invalidateQueries({ queryKey: queryKeys.myLeagues });
      void queryClient.invalidateQueries({ queryKey: queryKeys.invite(code) });
    },
  });
}

/**
 * Signs in with a sign-in link the commissioner issued (docs/adr/0013):
 * the link's account gets a new session on this device. The code goes in
 * the body, never in a logged URL. Resolves to the league to open.
 */
export function useSignInWithLink(code: string) {
  const queryClient = useQueryClient();
  const { signIn } = useAuth();
  return useMutation({
    mutationFn: () => send(api.POST('/v1/auth/signin-link', { body: { code } })),
    onSuccess: async (envelope) => {
      await signIn(envelope.data.session);
      void queryClient.invalidateQueries({ queryKey: queryKeys.myLeagues });
    },
  });
}
