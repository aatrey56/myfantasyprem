/**
 * The import preview asks FPL live through a rate-paced client, so it can
 * take well over the usual request timeout. It waits longer, and a slow
 * answer is not retried (each retry would queue another FPL call).
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { REQUEST_TIMEOUT } from './client';
import { bindSessionStore, memorySessionStore, tokens, type StoredSession } from '@/auth/session';
import { IMPORT_PREVIEW_TIMEOUT, useImportPreview } from './hooks';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));

const realFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = realFetch;
  jest.useRealTimers();
  tokens.set(null);
  bindSessionStore(null);
});

const wrapperFor = (client: QueryClient) =>
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };

it('waits past the usual timeout, then fails once without a retry', async () => {
  jest.useFakeTimers();
  const hang = jest.fn(
    (_input: RequestInfo | URL, init?: RequestInit) =>
      new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () =>
          reject(Object.assign(new Error('aborted'), { name: 'AbortError' })),
        );
      }),
  );
  globalThis.fetch = hang as typeof fetch;
  // The app's own default retries (3); the hook must override it.
  const client = new QueryClient({ defaultOptions: { queries: { retry: 3, retryDelay: 1 } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const { result } = await renderHook(() => useImportPreview({ fplLeagueId: 90001 }), { wrapper });

  await jest.advanceTimersByTimeAsync(REQUEST_TIMEOUT.ms + 1);
  expect(result.current.isError).toBe(false);
  expect(hang).toHaveBeenCalledTimes(1);

  await jest.advanceTimersByTimeAsync(IMPORT_PREVIEW_TIMEOUT.ms - REQUEST_TIMEOUT.ms + 1000);
  expect(result.current.isError).toBe(true);
  expect(result.current.error).toBeInstanceOf(TypeError);
  expect(hang).toHaveBeenCalledTimes(1);
  expect(IMPORT_PREVIEW_TIMEOUT.ms).toBeGreaterThanOrEqual(40_000);
});

it('keeps the long wait when the token expired and the preview is retried', async () => {
  jest.useFakeTimers();
  bindSessionStore(memorySessionStore());
  const session = (n: number) => ({
    accessToken: `a${n}`,
    refreshToken: `r${n}`,
    expiresAt: '2099-01-01T00:00:00Z',
    refreshExpiresAt: '2099-01-01T00:00:00Z',
    user: {
      id: 'usr_01k7m2xq8y0000000000000001',
      displayName: 'Sam',
      email: 'sam@example.com',
      avatarColor: '#2f6fed',
      authProviders: ['email'],
      createdAt: '2026-07-01T09:00:00Z',
    } as StoredSession['user'],
  });
  tokens.set(session(1));
  const meta = {
    dataAsOf: '2026-10-12T00:00:00Z',
    serverTime: '2026-10-12T00:00:00Z',
    stale: false,
  };
  const json = (status: number, body: unknown) =>
    new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    if (new URL(request.url).pathname === '/v1/auth/refresh')
      return json(200, { data: session(2), meta });
    if (request.headers.get('authorization') !== 'Bearer a2') {
      return json(401, { type: 'about:blank', title: 'Unauthorized', status: 401 });
    }
    // The refreshed preview takes 20 s: past the usual 15 s, inside 45 s.
    return new Promise<Response>((resolve, reject) => {
      const timer = setTimeout(() => resolve(json(200, { data: { league: 'ok' }, meta })), 20_000);
      init?.signal?.addEventListener('abort', () => {
        clearTimeout(timer);
        reject(Object.assign(new Error('aborted'), { name: 'AbortError' }));
      });
    });
  }) as typeof fetch;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { result } = await renderHook(() => useImportPreview({ fplLeagueId: 90001 }), {
    wrapper: wrapperFor(client),
  });

  await jest.advanceTimersByTimeAsync(20_000 + 1000);
  expect(result.current.error).toBeNull();
  expect(result.current.data?.data).toEqual({ league: 'ok' });
});
