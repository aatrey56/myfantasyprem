import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { useLineup, useSetLineup } from './lineup-hooks';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));
jest.mock('expo-router', () => ({ useIsFocused: () => true }));

const LEAGUE = 'lg_01k7m2xq800000000000000001';
const TEAM = 'tm_01k7m2xq800000000000000001';

/** The device clock runs this far behind the server. */
const SKEW_MS = 5 * 60_000;
const SERVER_START = Date.parse('2026-10-17T10:00:00Z');
/** The GW 8 deadline. Tests on the real clock move it a day ahead of now
 * (in their own describe), or the test breaks once this date has passed. */
let LOCKS_AT = new Date(SERVER_START + 60_000).toISOString();

const serverNow = () => Date.now() + SKEW_MS;
/** Ids carry the position: `MID_2` is a midfielder. */
const entry = (id: string) => ({ player: { id, position: id.split('_')[0] } });
/** A legal 4-4-2 and its bench (backup keeper first). */
const XI = [
  'GKP_1',
  'DEF_1',
  'DEF_2',
  'DEF_3',
  'DEF_4',
  'MID_1',
  'MID_2',
  'MID_3',
  'MID_4',
  'FWD_1',
  'FWD_2',
];
const BENCH = ['GKP_2', 'DEF_5', 'MID_5', 'FWD_3'];
const lineup = (
  starters: string[],
  locked = serverNow() >= Date.parse(LOCKS_AT),
  bench: string[] = BENCH,
) => ({
  data: {
    teamId: TEAM,
    gameweek: 8,
    locked,
    locksAt: LOCKS_AT,
    starters: starters.map(entry),
    bench: bench.map(entry),
  },
  meta: {
    dataAsOf: new Date(serverNow()).toISOString(),
    serverTime: new Date(serverNow()).toISOString(),
    stale: false,
  },
});
const ids = (env: unknown) =>
  (env as { data: { starters: { player: { id: string } }[] } } | undefined)?.data.starters.map(
    (s) => s.player.id,
  );

let saved: string[];
let gets: number;
/** When set, GETs never answer (a slow or failed refetch). */
let hangGets = false;

const json = (body: unknown) =>
  new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'content-type': 'application/json' },
  });

const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    if (request.method === 'PUT') {
      const body = (await request.json()) as { starters: string[]; bench: string[] };
      saved = body.starters;
      return json(lineup(saved, false, body.bench));
    }
    gets += 1;
    if (hangGets) return new Promise<Response>(() => undefined);
    return json(lineup(saved));
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});

let queryClient: QueryClient;
beforeEach(() => {
  saved = XI;
  gets = 0;
  hangGets = false;
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
  jest.restoreAllMocks();
});

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);

describe('useSetLineup', () => {
  beforeEach(() => {
    LOCKS_AT = new Date(Date.now() + 24 * 3_600_000).toISOString();
  });
  afterEach(() => {
    LOCKS_AT = new Date(SERVER_START + 60_000).toISOString();
  });

  it('shows the saved XI on the "next gameweek" lineup at once, without waiting for a refetch', async () => {
    // Ten minutes before the deadline on the device clock, whatever today is.
    const start = performance.now();
    jest
      .spyOn(Date, 'now')
      .mockImplementation(() => SERVER_START - SKEW_MS - 10 * 60_000 + (performance.now() - start));
    const { result } = await renderHook(
      () => ({ next: useLineup(LEAGUE, TEAM, undefined), save: useSetLineup(LEAGUE, TEAM) }),
      { wrapper },
    );
    await waitFor(() => expect(ids(result.current.next.data)).toEqual(XI));
    hangGets = true;
    const swapped = XI.map((id) => (id === 'MID_4' ? 'MID_5' : id));
    await act(async () => {
      await result.current.save.mutateAsync({
        gameweek: 8,
        starters: swapped,
        bench: ['GKP_2', 'DEF_5', 'MID_4', 'FWD_3'],
      });
    });
    expect(ids(result.current.next.data)).toEqual(swapped);
  });
});

describe('useLineup', () => {
  it('refetches when the deadline passes on the server clock, so the lineup shows locked', async () => {
    jest.useFakeTimers({ now: SERVER_START - SKEW_MS });
    const { result } = await renderHook(() => useLineup(LEAGUE, TEAM, undefined), { wrapper });
    await waitFor(() => expect(result.current.data?.data.locked).toBe(false));
    expect(gets).toBe(1);
    // 61 s later the server has locked the lineup; the device clock still
    // reads five minutes before the deadline.
    await act(async () => {
      jest.advanceTimersByTime(61_000);
    });
    await waitFor(() => expect(result.current.data?.data.locked).toBe(true));
    expect(gets).toBe(2);
  });
});
