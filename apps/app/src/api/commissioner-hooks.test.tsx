import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { SUMMARY_REFRESH_MS, useCommissionerMovesSummary } from './commissioner-hooks';
import { queryKeys } from './hooks';
import { invalidationsFor } from './realtime';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));
let mockFocused = true;
jest.mock('expo-router', () => ({ useIsFocused: () => mockFocused }));

const LEAGUE = 'lg_01k7m2xq800000000000000001';
const meta = () => ({
  dataAsOf: new Date().toISOString(),
  serverTime: new Date().toISOString(),
  stale: false,
});

/** The server's unseen count; each GET of the summary is counted. */
let unseen: number;
let gets: number;

const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const { pathname } = new URL(new Request(input, init).url);
    if (pathname.endsWith('/commissioner-moves/summary')) {
      gets += 1;
      return new Response(
        JSON.stringify({ data: { unseenCount: unseen, latest: null }, meta: meta() }),
        { status: 200, headers: { 'content-type': 'application/json' } },
      );
    }
    throw new Error(`unexpected ${pathname}`);
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});

let queryClient: QueryClient;
beforeEach(() => {
  unseen = 0;
  gets = 0;
  mockFocused = true;
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
});
afterEach(() => {
  queryClient.clear();
  jest.useRealTimers();
});

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);

describe('useCommissionerMovesSummary', () => {
  it('refetches while Status is in view, so a missed event still shows the banner', async () => {
    jest.useFakeTimers();
    const { result } = await renderHook(() => useCommissionerMovesSummary(LEAGUE), { wrapper });
    await waitFor(() => expect(result.current.data?.data.unseenCount).toBe(0));
    unseen = 1; // the commissioner created an invite; the event never arrived
    await act(async () => {
      jest.advanceTimersByTime(SUMMARY_REFRESH_MS);
    });
    await waitFor(() => expect(result.current.data?.data.unseenCount).toBe(1));
  });

  it('does not poll a screen out of view', async () => {
    mockFocused = false;
    jest.useFakeTimers();
    await renderHook(() => useCommissionerMovesSummary(LEAGUE), { wrapper });
    await act(async () => {
      jest.advanceTimersByTime(SUMMARY_REFRESH_MS * 3);
    });
    expect(gets).toBe(0);
  });

  it('refetches on league.updated, the event an invite or a sign-in link sends', async () => {
    const { result } = await renderHook(() => useCommissionerMovesSummary(LEAGUE), { wrapper });
    await waitFor(() => expect(result.current.data?.data.unseenCount).toBe(0));
    unseen = 2;
    await act(async () => {
      for (const queryKey of invalidationsFor(LEAGUE, 'league.updated')) {
        await queryClient.invalidateQueries({ queryKey });
      }
    });
    await waitFor(() => expect(result.current.data?.data.unseenCount).toBe(2));
    expect(
      queryClient.getQueryState(queryKeys.commissionerMovesSummary(LEAGUE))?.isInvalidated,
    ).toBe(false);
  });
});
