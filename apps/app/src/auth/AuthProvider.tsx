import type { Schemas } from '@myfantasyprem/api-client';
import { useQueryClient } from '@tanstack/react-query';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';

import {
  bindPendingRevocationStore,
  drainPendingRevocations,
  type PendingRevocationStore,
} from './revocation';
import { SessionPrefsProvider } from './SessionPrefs';
import {
  bindSessionStore,
  fromSession,
  isSuperseded,
  tokens,
  type SessionStore,
  type StoredSession,
} from './session';
import { platformPendingRevocationStore, platformSessionStore } from './session-store';
import { dropQueryCache, dropSavedQueries } from '@/api/persist';

export type AuthStatus = 'loading' | 'signedOut' | 'signedIn';

export interface AuthContextValue {
  status: AuthStatus;
  user: Schemas['User'] | null;
  /** Keep a session the server just issued. */
  signIn: (session: Schemas['Session']) => Promise<void>;
  /** Forget the session locally and drop every cached query, saved copy included. */
  signOut: () => Promise<void>;
  /** Replace the cached profile after an edit. */
  setUser: (user: Schemas['User']) => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>.');
  return ctx;
}

interface Props {
  children: ReactNode;
  /** Platform storage by default; tests pass memorySessionStore(). */
  store?: SessionStore;
  /** Sessions signed out but not yet revoked on the server; tests pass a memory one. */
  revocations?: PendingRevocationStore;
}

/**
 * Owns the session: loads it from platform storage at launch, mirrors it into
 * the token holder the API client reads, and persists every change (sign-in,
 * refresh rotation, sign-out). A failed refresh clears the tokens, which
 * lands here as a sign-out.
 */
export function AuthProvider({
  children,
  store = platformSessionStore,
  revocations = platformPendingRevocationStore,
}: Props) {
  const queryClient = useQueryClient();
  const [session, setSession] = useState<StoredSession | null>(null);
  const [status, setStatus] = useState<AuthStatus>('loading');

  // Bound first: every tokens.set from here on is written through to it, and
  // the API client re-reads it before rotating.
  useEffect(() => {
    bindSessionStore(store);
    return () => bindSessionStore(null);
  }, [store]);

  // Revocations a previous run could not finish (offline, rate limited)
  // are retried at start.
  useEffect(() => {
    bindPendingRevocationStore(revocations);
    void drainPendingRevocations();
    return () => bindPendingRevocationStore(null);
  }, [revocations]);

  useEffect(() => {
    let alive = true;
    void store.load().then((s) => {
      if (!alive) return;
      tokens.adopt(s);
      // Nobody signed in: a cache saved by a previous session is not theirs.
      if (!s) void dropSavedQueries(queryClient);
      setSession(s);
      setStatus(s ? 'signedIn' : 'signedOut');
    });
    return () => {
      alive = false;
    };
  }, [store, queryClient]);

  // Token changes from outside React (refresh rotation, refresh failure).
  // tokens.set has already persisted them.
  // Any sign-out, or a different user, drops every cached query so the next
  // user never sees the previous one's data.
  useEffect(() => {
    let userId = tokens.get()?.user.id ?? null;
    return tokens.subscribe((s) => {
      setSession(s);
      const nextUserId = s?.user.id ?? null;
      if (userId !== null && nextUserId !== userId) void dropQueryCache(queryClient);
      userId = nextUserId;
      setStatus(s ? 'signedIn' : 'signedOut');
    });
  }, [queryClient]);

  // Another tab rotated the tokens or signed out: follow it, so this tab
  // never presents a refresh token the server has already rotated.
  useEffect(
    () =>
      store.subscribe?.((s) => {
        const mine = tokens.get();
        if (s?.refreshToken === mine?.refreshToken && s?.accessToken === mine?.accessToken) return;
        // Ignore a late event for a pair this tab already moved past.
        if (s && isSuperseded(s)) return;
        tokens.adopt(s);
      }),
    [store],
  );

  const signIn = useCallback(async (s: Schemas['Session']) => {
    // Written through to the store by tokens.set.
    tokens.set(fromSession(s));
  }, []);

  const signOut = useCallback(async () => {
    tokens.set(null);
    // tokens.set clears storage; this covers a sign-out with no session.
    await dropQueryCache(queryClient);
  }, [queryClient]);

  const setUser = useCallback((user: Schemas['User']) => {
    const s = tokens.get();
    if (s) tokens.set({ ...s, user });
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({ status, user: session?.user ?? null, signIn, signOut, setUser }),
    [status, session, signIn, signOut, setUser],
  );
  return (
    <AuthContext.Provider value={value}>
      <SessionPrefsProvider userId={session?.user.id ?? null}>{children}</SessionPrefsProvider>
    </AuthContext.Provider>
  );
}
