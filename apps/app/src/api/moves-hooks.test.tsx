import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { queryKeys } from './hooks';
import {
  useCancelClaim,
  useReorderClaims,
  useRespondToTrade,
  useTrades,
  useWaiverClaims,
} from './moves-hooks';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));
jest.mock('expo-router', () => ({ useIsFocused: () => true }));

const LEAGUE = 'lg_01k7m2xq800000000000000001';
const meta = () => ({
  dataAsOf: new Date().toISOString(),
  serverTime: new Date().toISOString(),
  stale: false,
});
const claim = (id: string, priority: number) => ({ id, priority, status: 'pending' });
const json = (status: number, body?: unknown) =>
  new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });

/** Fake server state; PUT/DELETE apply on arrival, answers can be held back. */
let claims: string[];
let trades: { id: string; status: string }[];
let tradeGets: number;
let holdWrites: boolean;
let held: (() => void)[];

const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    const { pathname } = new URL(request.url);
    const reply = (r: Response) =>
      holdWrites ? new Promise<Response>((resolve) => held.push(() => resolve(r))) : r;
    if (pathname.endsWith('/waivers/claim-order')) {
      const { claimIds } = (await request.json()) as { claimIds: string[] };
      claims = claimIds.filter((id) => claims.includes(id));
      return reply(json(200, { data: claims.map((id, i) => claim(id, i + 1)), meta: meta() }));
    }
    if (request.method === 'DELETE' && pathname.includes('/waivers/claims/')) {
      const id = pathname.split('/').pop()!;
      claims = claims.filter((c) => c !== id);
      return reply(json(204));
    }
    if (pathname.endsWith('/waivers/claims')) {
      return json(200, { data: claims.map((id, i) => claim(id, i + 1)), meta: meta() });
    }
    if (pathname.endsWith('/review')) {
      return json(200, { data: { id: 'trd_1', status: 'vetoed' }, meta: meta() });
    }
    if (pathname.endsWith('/trades')) {
      tradeGets += 1;
      return json(200, { data: trades, meta: { ...meta(), nextCursor: null } });
    }
    throw new Error(`unexpected ${request.method} ${pathname}`);
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});

let queryClient: QueryClient;
beforeEach(() => {
  claims = ['wv_1', 'wv_2', 'wv_3'];
  trades = [];
  tradeGets = 0;
  holdWrites = false;
  held = [];
  queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { gcTime: Infinity },
    },
  });
});
afterEach(() => {
  queryClient.clear();
  jest.useRealTimers();
});

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);
const tick = () =>
  act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });

describe('claims', () => {
  it('a cancel during a reorder stays cancelled, whatever order the answers land in', async () => {
    const key = queryKeys.claims(LEAGUE, 'pending');
    queryClient.setQueryData(key, {
      data: claims.map((id, i) => claim(id, i + 1)),
      meta: meta(),
    });
    const { result } = await renderHook(
      () => ({
        list: useWaiverClaims(LEAGUE),
        reorder: useReorderClaims(LEAGUE),
        cancel: useCancelClaim(LEAGUE),
      }),
      { wrapper },
    );
    holdWrites = true;
    await act(async () => {
      result.current.reorder.mutate(['wv_3', 'wv_2', 'wv_1']);
    });
    await waitFor(() => expect(held).toHaveLength(1)); // the reorder reached the server
    await act(async () => {
      result.current.cancel.mutate('wv_2');
    });
    // Release answers newest first until everything settles.
    for (let i = 0; i < 20 && (held.length || queryClient.isMutating()); i++) {
      await tick();
      held.pop()?.();
    }
    holdWrites = false;
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    await tick();
    expect(claims).toEqual(['wv_3', 'wv_1']);
    const ids = (queryClient.getQueryData(key) as { data: { id: string }[] }).data.map((c) => c.id);
    expect(ids).toEqual(['wv_3', 'wv_1']);
  });
});

describe('trades', () => {
  it('a veto refreshes the activity feed', async () => {
    const feed = [...queryKeys.transactions(LEAGUE, undefined), 20];
    queryClient.setQueryData(feed, { data: [], meta: meta() });
    const { result } = await renderHook(() => useRespondToTrade(LEAGUE), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({
        tradeId: 'trd_1',
        action: { kind: 'review', decision: 'veto' },
      });
    });
    expect(queryClient.getQueryState(feed)?.isInvalidated).toBe(true);
  });

  it('keeps checking for new offers while the inbox has nothing open', async () => {
    jest.useFakeTimers();
    trades = [{ id: 'trd_old', status: 'declined' }];
    const { result } = await renderHook(() => useTrades(LEAGUE), { wrapper });
    await waitFor(() => expect(result.current.data?.pages[0]?.data).toHaveLength(1));
    expect(tradeGets).toBe(1);
    trades = [{ id: 'trd_new', status: 'offered' }, ...trades];
    await act(async () => {
      jest.advanceTimersByTime(2 * 60_000);
    });
    await waitFor(() => expect(result.current.data?.pages[0]?.data[0]?.id).toBe('trd_new'));
  });
});
