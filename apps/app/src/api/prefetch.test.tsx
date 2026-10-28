/**
 * Prefetch triggers against the MSW handlers: what opening a league, a
 * gameweek step and a press-in put in the cache, and that gameweek steps
 * keep the last answer on screen.
 */
import { send, type Schemas } from '@myfantasyprem/api-client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import { getResponse } from 'msw';
import type { ReactNode } from 'react';

import { api } from './client';
import { leagueHomeQuery, PLAYERS_FIRST_FILTER, queryKeys, useMatchup } from './hooks';
import { useLineup } from './lineup-hooks';
import {
  IMAGE_PREFETCH_ROWS,
  prefetchLeagueTab,
  usePrefetchAdjacentGameweeks,
  usePrefetchAdjacentLineups,
  usePrefetchLeague,
  usePressPrefetch,
} from './prefetch';
import { statusKeys } from './status-hooks';
import { tokens, type StoredSession } from '@/auth/session';
import { findMyMatchup, leagueGameweek, matchupUpcoming } from '@/lib/matchup';
import * as data from '@/mocks/data';
import { handlers, resetMocks } from '@/mocks/handlers';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));
jest.mock('expo-router', () => ({ useIsFocused: () => true }));
const mockPrefetchImages = jest.fn();
jest.mock('./image-prefetch', () => ({
  prefetchImages: (urls: string[]) => mockPrefetchImages(urls),
}));

const SESSION = {
  accessToken: 'mock-access-token',
  refreshToken: 'mock-refresh-token-0001',
  expiresAt: '2099-01-01T00:00:00Z',
  refreshExpiresAt: '2099-01-01T00:00:00Z',
  user: { id: data.DEMO_USER_ID, displayName: data.DEMO_USER_NAME },
} as StoredSession;

const id = data.LEAGUE_ID;
type Home = Schemas['LeagueHome'];

/** Paths the fake server was asked for, with their query string. */
let requests: string[];
/** Requests whose path matches wait here until the test releases them. */
let held: { match: (path: string) => boolean; released: Promise<void> } | null;
let releaseHeld: (() => void) | null = null;
function hold(match: (path: string) => boolean): () => void {
  let release!: () => void;
  held = { match, released: new Promise<void>((r) => (release = r)) };
  releaseHeld = () => {
    held = null;
    releaseHeld = null;
    release();
  };
  return releaseHeld;
}
// A failed assertion skips the test's release: let the held fetch finish
// anyway so no request (or its timeout) outlives the test.
afterEach(() => releaseHeld?.());
const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    const url = new URL(request.url);
    requests.push(url.pathname + url.search);
    if (held?.match(url.pathname + url.search)) await held.released;
    const response = await getResponse(handlers, request);
    if (!response) throw new Error(`No mock handler for ${request.url}`);
    return response;
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});

let queryClient: QueryClient;
let home: Home;
beforeEach(async () => {
  resetMocks();
  tokens.set(SESSION);
  requests = [];
  held = null;
  // No gc timers: a prefetch landing after the test would keep Jest alive.
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  home = ((await queryClient.fetchQuery(leagueHomeQuery(id))) as { data: Home }).data;
  requests = [];
});
afterEach(() => {
  queryClient.clear();
  tokens.set(null);
});

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);
const cached = (key: readonly unknown[]) => queryClient.getQueryData(key) !== undefined;
const matchupsFor = (gw: number) =>
  (queryClient.getQueryData(queryKeys.matchups(id, gw)) as { data: Schemas['MatchupSummary'][] })
    .data;

describe('opening a league', () => {
  it("loads all five tabs' first data once the home is in", async () => {
    await renderHook(() => usePrefetchLeague(id), { wrapper });
    const gw = leagueGameweek(home);
    const myTeam = home.viewer.team!.id;
    await waitFor(() => {
      expect(cached(statusKeys.status(id))).toBe(true); // Status
      expect(cached(queryKeys.matchups(id, gw))).toBe(true); // Match
      expect(cached(queryKeys.team(id, myTeam))).toBe(true); // Team
      expect(cached(queryKeys.players(id, PLAYERS_FIRST_FILTER))).toBe(true); // Players
    });
    const mine = findMyMatchup(matchupsFor(gw), myTeam)!;
    await waitFor(() => expect(cached(queryKeys.matchup(id, gw, mine.id))).toBe(true));
    // League: the home, already in; not asked for again while fresh.
    expect(requests.filter((r) => r === `/v1/leagues/${id}`)).toEqual([]);
  });

  it('waits for the league home', async () => {
    const empty = new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } });
    await renderHook(() => usePrefetchLeague(id), {
      wrapper: ({ children }) => (
        <QueryClientProvider client={empty}>{children}</QueryClientProvider>
      ),
    });
    await act(async () => undefined);
    expect(requests).toEqual([]);
    empty.clear();
  });
});

describe('gameweek neighbours', () => {
  it('loads N-1 and N+1 on the Match tab, with the matchup each would open', async () => {
    // A played gameweek with played ones either side.
    const current = leagueGameweek(home);
    const gw = current - 1;
    expect(gw - 1).toBeGreaterThanOrEqual(home.settings.startGameweek);
    await renderHook(() => usePrefetchAdjacentGameweeks(id, gw), { wrapper });
    for (const n of [gw - 1, gw + 1]) {
      await waitFor(() => expect(cached(queryKeys.matchups(id, n))).toBe(true));
      const mine = findMyMatchup(matchupsFor(n), home.viewer.team!.id)!;
      await waitFor(() => expect(cached(queryKeys.matchup(id, n, mine.id))).toBe(true));
    }
    // Not the gameweek itself (its screen loads it) nor two away.
    expect(cached(queryKeys.matchups(id, gw - 2))).toBe(false);
  });

  it('skips a gameweek still ahead and one before the league starts', async () => {
    const first = home.settings.startGameweek;
    await renderHook(() => usePrefetchAdjacentGameweeks(id, first), { wrapper });
    const next = first + 1;
    if (!matchupUpcoming(home, next)) {
      await waitFor(() => expect(cached(queryKeys.matchups(id, next))).toBe(true));
    }
    expect(cached(queryKeys.matchups(id, first - 1))).toBe(false);

    const current = leagueGameweek(home);
    await renderHook(() => usePrefetchAdjacentGameweeks(id, current), { wrapper });
    await act(async () => undefined);
    if (matchupUpcoming(home, current + 1)) {
      expect(cached(queryKeys.matchups(id, current + 1))).toBe(false);
    }
    expect(requests.some((r) => r.includes(`/gameweeks/${first - 1}/`))).toBe(false);
  });

  it("loads the same team's lineup for N-1 and N+1 within the league's gameweeks", async () => {
    const team = home.viewer.team!.id;
    await renderHook(() => usePrefetchAdjacentLineups(id, team, 5, 4, 5), { wrapper });
    await waitFor(() => expect(cached(queryKeys.lineup(id, team, 4))).toBe(true));
    // 6 is past the last gameweek: not asked for.
    expect(requests.filter((r) => r.includes('/lineup'))).toEqual([
      `/v1/leagues/${id}/teams/${team}/lineup?gameweek=4`,
    ]);
  });
});

describe('a gameweek step keeps the last answer on screen', () => {
  it('for the matchup head-to-head', async () => {
    const gw = leagueGameweek(home) - 1;
    await queryClient.prefetchQuery({
      queryKey: queryKeys.matchups(id, gw - 1),
      queryFn: () =>
        send(
          api.GET('/v1/leagues/{leagueId}/gameweeks/{gw}/matchups', {
            params: { path: { leagueId: id, gw: gw - 1 } },
          }),
        ),
    });
    const a = findMyMatchup(home.matchups, home.viewer.team!.id)!;
    const b = findMyMatchup(matchupsFor(gw - 1), home.viewer.team!.id)!;
    const { result, rerender } = await renderHook(
      ({ g, m }: { g: number; m: string }) => useMatchup(id, g, m),
      { wrapper, initialProps: { g: leagueGameweek(home), m: a.id } },
    );
    await waitFor(() => expect(result.current.data?.data.id).toBe(a.id));

    // Hold the next answer so the stand-in can be seen before it lands.
    const release = hold((path) => path.split('?')[0]!.endsWith(`/matchups/${b.id}`));
    await rerender({ g: gw - 1, m: b.id });
    // Still the last matchup, flagged as a stand-in, until the next lands.
    expect(result.current.data?.data.id).toBe(a.id);
    expect(result.current.isPlaceholderData).toBe(true);
    release();
    await waitFor(() => expect(result.current.data?.data.id).toBe(b.id));
    expect(result.current.isPlaceholderData).toBe(false);
  });

  it('for the lineup', async () => {
    const team = home.viewer.team!.id;
    const gw = leagueGameweek(home);
    const { result, rerender } = await renderHook(
      ({ g }: { g: number }) => useLineup(id, team, g),
      {
        wrapper,
        initialProps: { g: gw },
      },
    );
    await waitFor(() => expect(result.current.data?.data.gameweek).toBe(gw));

    const release = hold((path) => path.includes(`/lineup?gameweek=${gw - 1}`));
    await rerender({ g: gw - 1 });
    expect(result.current.data?.data.gameweek).toBe(gw);
    expect(result.current.isPlaceholderData).toBe(true);
    release();
    await waitFor(() => expect(result.current.data?.data.gameweek).toBe(gw - 1));
  });
});

describe('press-in', () => {
  it('a league row loads the league home and, once drafted, its Status tab', async () => {
    queryClient.clear();
    const { result } = await renderHook(() => usePressPrefetch(), { wrapper });
    await act(async () => result.current.league(id, 'in_season'));
    await waitFor(() => {
      expect(cached(queryKeys.league(id))).toBe(true);
      expect(cached(statusKeys.status(id))).toBe(true);
    });
  });

  it('a league before its draft loads only the home', async () => {
    queryClient.clear();
    const { result } = await renderHook(() => usePressPrefetch(), { wrapper });
    await act(async () => result.current.league(id, 'pre_draft'));
    await waitFor(() => expect(cached(queryKeys.league(id))).toBe(true));
    expect(requests.some((r) => r.endsWith('/status'))).toBe(false);
  });

  it('a player row loads the card; a matchup card its head-to-head', async () => {
    const { result } = await renderHook(() => usePressPrefetch(), { wrapper });
    const players = await send(
      api.GET('/v1/leagues/{leagueId}/players', {
        params: { path: { leagueId: id }, query: { limit: 1 } },
      }),
    );
    const player = players.data[0]!.player.id;
    const mine = findMyMatchup(home.matchups, home.viewer.team!.id)!;
    const gw = leagueGameweek(home);
    await act(async () => result.current.matchup(id, gw, mine.id));
    await waitFor(() => expect(cached(queryKeys.matchup(id, gw, mine.id))).toBe(true));
    await act(async () => result.current.player(id, player));
    await waitFor(() => expect(cached(queryKeys.player(id, player))).toBe(true));
  });

  it('does nothing outside a QueryClientProvider', async () => {
    const { result } = await renderHook(() => usePressPrefetch());
    expect(() => result.current.league(id, 'in_season')).not.toThrow();
    expect(requests).toEqual([]);
  });
});

describe('images for the first rows', () => {
  beforeEach(() => mockPrefetchImages.mockClear());

  it('warms the Players tab photos and crests when official images are on', async () => {
    queryClient.setQueryData(['config'], { data: { brandedAssets: true }, meta: {} });
    await prefetchLeagueTab(queryClient, id, 'players', home);
    expect(mockPrefetchImages).toHaveBeenCalledTimes(1);
    const urls = mockPrefetchImages.mock.calls[0]![0] as string[];
    const photos = urls.filter((u) => u.includes('/v1/assets/players/'));
    expect(photos).toHaveLength(IMAGE_PREFETCH_ROWS);
    expect(urls.every((u) => u.startsWith('http://api.test/v1/assets/'))).toBe(true);
    expect(urls.some((u) => u.endsWith('/crest'))).toBe(true);
  });

  it("warms the user's squad on the Team tab", async () => {
    queryClient.setQueryData(['config'], { data: { brandedAssets: true }, meta: {} });
    await prefetchLeagueTab(queryClient, id, 'team', home);
    const urls = mockPrefetchImages.mock.calls[0]![0] as string[];
    expect(urls.filter((u) => u.endsWith('/photo')).length).toBeGreaterThan(0);
  });

  it('requests no image while the switch is off or unknown', async () => {
    await prefetchLeagueTab(queryClient, id, 'players', home);
    queryClient.setQueryData(['config'], { data: { brandedAssets: false }, meta: {} });
    await prefetchLeagueTab(queryClient, id, 'team', home);
    expect(mockPrefetchImages).not.toHaveBeenCalled();
  });
});
