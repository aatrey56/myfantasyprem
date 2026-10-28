import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { useSignOut } from './account-hooks';
import { AuthProvider, useAuth } from '@/auth/AuthProvider';
import { memoryPendingRevocationStore, type PendingRevocationStore } from '@/auth/revocation';
import { memorySessionStore, tokens, type StoredSession } from '@/auth/session';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));
jest.mock('expo-router', () => ({ useIsFocused: () => true }));
jest.mock('@/auth/providers', () => ({}));

const session = (n: number): StoredSession => ({
  accessToken: `a${n}`,
  refreshToken: `r${n}`,
  expiresAt: '2099-01-01T00:00:00Z',
  refreshExpiresAt: '2099-01-01T00:00:00Z',
  user: {
    id: 'usr_a',
    displayName: 'Sam',
    email: 'sam@example.com',
    avatarColor: '#2f6fed',
    authProviders: ['email'],
    createdAt: '2026-07-01T09:00:00Z',
  } as StoredSession['user'],
});

const problem = (status: number, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify({ type: 'about:blank', title: 'No', status }), {
    status,
    headers: { 'content-type': 'application/problem+json', ...headers },
  });

/** Each call to the fake server: method, path, bearer token and when (fake ms). */
let calls: { path: string; bearer: string | null; at: number }[];
/** Answers the next logout / refresh. */
let onLogout: (bearer: string | null) => Response | Promise<Response>;
let onRefresh: (refreshToken: string) => Response | Promise<Response>;

const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    const { pathname } = new URL(request.url);
    const bearer = request.headers.get('Authorization')?.replace(/^Bearer /, '') ?? null;
    calls.push({ path: pathname, bearer, at: Date.now() });
    if (pathname === '/v1/auth/logout') return onLogout(bearer);
    if (pathname === '/v1/auth/refresh') {
      const { refreshToken } = (await request.json()) as { refreshToken: string };
      return onRefresh(refreshToken);
    }
    throw new Error(`unexpected ${request.method} ${pathname}`);
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});

beforeEach(() => {
  jest.useFakeTimers();
  calls = [];
  onRefresh = () => problem(401);
  tokens.set(null);
});
afterEach(() => {
  jest.useRealTimers();
});

const logouts = () => calls.filter((c) => c.path === '/v1/auth/logout');

function mount(store: ReturnType<typeof memorySessionStore>, pending: PendingRevocationStore) {
  const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <AuthProvider store={store} revocations={pending}>
        {children}
      </AuthProvider>
    </QueryClientProvider>
  );
  return renderHook(() => ({ auth: useAuth(), signOut: useSignOut() }), { wrapper });
}

describe('useSignOut', () => {
  it('signs out at once and retries the revocation after a 429, honouring Retry-After', async () => {
    let n = 0;
    onLogout = () =>
      ++n === 1 ? problem(429, { 'Retry-After': '7' }) : new Response(null, { status: 204 });
    const pending = memoryPendingRevocationStore();
    const { result } = await mount(memorySessionStore(session(1)), pending);
    await waitFor(() => expect(result.current.auth.status).toBe('signedIn'));

    await act(async () => {
      result.current.signOut.mutate();
    });
    await waitFor(() => expect(result.current.auth.status).toBe('signedOut'));
    await waitFor(() => expect(logouts()).toHaveLength(1));

    // Not before Retry-After has passed.
    await jest.advanceTimersByTimeAsync(6_000);
    expect(logouts()).toHaveLength(1);
    await jest.advanceTimersByTimeAsync(2_000);
    await waitFor(() => expect(logouts()).toHaveLength(2));
    const [first, second] = logouts();
    expect(second.bearer).toBe('a1');
    expect(second.at - first.at).toBeGreaterThanOrEqual(7_000);
    // Revoked: nothing left pending.
    await waitFor(() =>
      expect((pending as ReturnType<typeof memoryPendingRevocationStore>).entries()).toEqual([]),
    );
  });

  it('keeps the token pending when revocation keeps failing and retries it on next start', async () => {
    onLogout = () => problem(503);
    const pending = memoryPendingRevocationStore();
    const first = await mount(memorySessionStore(session(1)), pending);
    await waitFor(() => expect(first.result.current.auth.status).toBe('signedIn'));

    await act(async () => {
      first.result.current.signOut.mutate();
    });
    await waitFor(() => expect(first.result.current.auth.status).toBe('signedOut'));
    // Retries with backoff for a bounded time, then gives up for this run.
    await jest.advanceTimersByTimeAsync(10 * 60_000);
    const tries = logouts().length;
    expect(tries).toBeGreaterThan(2);
    await jest.advanceTimersByTimeAsync(10 * 60_000);
    expect(logouts()).toHaveLength(tries);
    expect(pending.entries()).toEqual([
      { accessToken: 'a1', refreshToken: 'r1', refreshExpiresAt: '2099-01-01T00:00:00Z' },
    ]);
    await first.unmount();

    // Next start: the access token has expired, so the pending refresh
    // token is rotated and the new session revoked.
    calls = [];
    onLogout = (bearer) => (bearer === 'a2' ? new Response(null, { status: 204 }) : problem(401));
    onRefresh = (refreshToken) =>
      refreshToken === 'r1'
        ? new Response(
            JSON.stringify({
              data: { ...session(2), tokenType: 'Bearer', isNewUser: false },
              meta: { serverTime: new Date().toISOString() },
            }),
            { status: 200, headers: { 'content-type': 'application/json' } },
          )
        : problem(401);
    const second = await mount(memorySessionStore(null), pending);
    await waitFor(() => expect(second.result.current.auth.status).toBe('signedOut'));
    await waitFor(() => expect(pending.entries()).toEqual([]));
    expect(calls.map((c) => `${c.path} ${c.bearer ?? '-'}`)).toEqual([
      '/v1/auth/logout a1',
      '/v1/auth/refresh -',
      '/v1/auth/logout a2',
    ]);
    // The signed-out app never picked the rotated session up.
    expect(tokens.get()).toBeNull();
  });
});
