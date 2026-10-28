import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { useMatchupPreview } from './hooks';
import { useSetLineup } from './lineup-hooks';
import { useAddFreeAgent } from './moves-hooks';
import { invalidationsFor } from './realtime';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));
let mockFocused = true;
jest.mock('expo-router', () => ({ useIsFocused: () => mockFocused }));

const LEAGUE = 'lg_01k7m2xq800000000000000001';
const TEAM = 'tm_01k7m2xq800000000000000001';
const MATCHUP = 'mu_01k7m2xq800000000000000001';
const meta = () => ({
  dataAsOf: new Date().toISOString(),
  serverTime: new Date().toISOString(),
  stale: false,
});
const json = (status: number, body?: unknown) =>
  new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });

let previewGets: number;
const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    const { pathname } = new URL(request.url);
    if (pathname.endsWith('/preview')) {
      previewGets += 1;
      return json(200, { data: { id: MATCHUP, gameweek: 9 }, meta: meta() });
    }
    if (request.method === 'PUT') {
      const body = (await request.json()) as { starters: string[]; bench: string[] };
      const entry = (id: string) => ({ player: { id, position: 'MID' } });
      return json(200, {
        data: {
          teamId: TEAM,
          gameweek: 9,
          locked: false,
          locksAt: new Date(Date.now() + 86_400_000).toISOString(),
          starters: body.starters.map(entry),
          bench: body.bench.map(entry),
        },
        meta: meta(),
      });
    }
    if (request.method === 'POST') return json(200, { data: {}, meta: meta() });
    return json(404, { title: 'not found', status: 404 });
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});

let queryClient: QueryClient;
beforeEach(() => {
  previewGets = 0;
  mockFocused = true;
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
});
afterEach(() => queryClient.clear());

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);

async function renderPreview<T>(extra: () => T) {
  const hook = await renderHook(
    () => ({ preview: useMatchupPreview(LEAGUE, 9, MATCHUP), extra: extra() }),
    { wrapper },
  );
  await waitFor(() => expect(hook.result.current.preview.data?.data.id).toBe(MATCHUP));
  expect(previewGets).toBe(1);
  return hook;
}

describe('useMatchupPreview', () => {
  it('refetches after a lineup save', async () => {
    const { result } = await renderPreview(() => useSetLineup(LEAGUE, TEAM));
    await act(async () => {
      await result.current.extra.mutateAsync({ gameweek: 9, starters: ['MID_1'], bench: [] });
    });
    await waitFor(() => expect(previewGets).toBe(2));
  });

  it('refetches after a roster move', async () => {
    const { result } = await renderPreview(() => useAddFreeAgent(LEAGUE));
    await act(async () => {
      await result.current.extra.mutateAsync({ addPlayerId: 'plr_1', dropPlayerId: 'plr_2' });
    });
    await waitFor(() => expect(previewGets).toBe(2));
  });

  it('refetches on a re-sync of the league and on realtime lineup and roster events', async () => {
    await renderPreview(() => null);
    const events = ['lineup.locked', 'waivers.processed', 'trade.updated', 'matchups.updated'];
    for (const [i, type] of events.entries()) {
      for (const queryKey of invalidationsFor(LEAGUE, type))
        await act(() => queryClient.invalidateQueries({ queryKey }));
      await waitFor(() => expect(previewGets).toBe(i + 2));
    }
  });

  it('is one of the keys the Match screen refreshes when it comes back into view', async () => {
    const { rerender } = await renderPreview(() => null);
    // Past its staleTime: the screen's focus refresh refetches it.
    jest.spyOn(Date, 'now').mockReturnValue(Date.now() + 5 * 60_000);
    mockFocused = false;
    await rerender({});
    mockFocused = true;
    await rerender({});
    await waitFor(() => expect(previewGets).toBe(2));
    jest.restoreAllMocks();
  });
});
