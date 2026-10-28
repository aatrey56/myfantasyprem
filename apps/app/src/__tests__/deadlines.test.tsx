/**
 * The deadlines screen's clock: it counts down on the server's clock, stops
 * ticking while the screen is hidden, and asks the server again when the
 * last deadline it knows of passes instead of declaring the season over.
 */
import type { Schemas } from '@myfantasyprem/api-client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import DeadlinesScreen from '@/app/(app)/leagues/[leagueId]/activity/deadlines';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));

let mockFocused = true;
jest.mock('expo-router', () => ({
  useIsFocused: () => mockFocused,
  useLocalSearchParams: () => ({ leagueId: 'lg_01k7m2xq800000000000000001' }),
}));
jest.mock('expo-router/head', () => ({ __esModule: true, default: () => null }));
jest.mock('@/components/PressableLink', () => {
  const { Pressable } = jest.requireActual('react-native');
  return { PressableLink: Pressable };
});

const H = 3600_000;
/** Server time at the start of each test. */
const T0 = Date.parse('2026-10-17T10:00:00Z');

const gw = (n: number, deadlineAt: number, finished = false): Schemas['Gameweek'] => ({
  number: n,
  name: `Gameweek ${n}`,
  deadlineAt: new Date(deadlineAt).toISOString(),
  waiverDeadlineAt: new Date(deadlineAt - 24 * H).toISOString(),
  tradeDeadlineAt: new Date(deadlineAt - 25 * H).toISOString(),
  finished,
});

/** Device clock minus server clock. */
let skew = 0;
/** What the server answers, on its own clock. */
let status: (serverNow: number) => Schemas['GameweekStatus'];
let gets = 0;

const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async () => {
    gets += 1;
    const serverNow = Date.now() - skew;
    return new Response(
      JSON.stringify({
        data: status(serverNow),
        meta: {
          dataAsOf: new Date(serverNow).toISOString(),
          serverTime: new Date(serverNow).toISOString(),
          stale: false,
        },
      }),
      { status: 200, headers: { 'content-type': 'application/json' } },
    );
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});

let queryClient: QueryClient;
beforeEach(() => {
  mockFocused = true;
  skew = 0;
  gets = 0;
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity, staleTime: Infinity } },
  });
});
afterEach(() => {
  queryClient.clear();
  jest.useRealTimers();
});

const ui = () => (
  <SafeAreaProvider
    initialMetrics={{
      frame: { x: 0, y: 0, width: 375, height: 812 },
      insets: { top: 0, left: 0, right: 0, bottom: 0 },
    }}
  >
    <QueryClientProvider client={queryClient}>
      <DeadlinesScreen />
    </QueryClientProvider>
  </SafeAreaProvider>
);

const countdown = () => screen.getByTestId('countdown');
const advance = (ms: number) =>
  act(async () => {
    jest.advanceTimersByTime(ms);
  });

describe('deadlines screen', () => {
  it('counts down on the server clock, not the device clock', async () => {
    skew = -H; // the device runs an hour slow
    jest.useFakeTimers({ now: T0 + skew });
    status = () => ({
      current: gw(7, T0 - 48 * H, true),
      next: gw(8, T0 + 2 * H + 30 * 60_000 + 30_000),
      bonusAdded: true,
      dataChecked: true,
      waiversProcessed: true,
    });
    await render(ui());
    // Next milestone on the server's clock: lineup lock in 2h 30m.
    await waitFor(() => expect(countdown()).toHaveTextContent('2h 30m'));
  });

  it('stops ticking while the screen is hidden', async () => {
    jest.useFakeTimers({ now: T0 });
    status = () => ({
      current: gw(7, T0 - 48 * H, true),
      next: gw(8, T0 + 30 * 60_000),
      bonusAdded: true,
      dataChecked: true,
      waiversProcessed: true,
    });
    const view = await render(ui());
    await waitFor(() => expect(countdown()).toHaveTextContent('30:00'));
    mockFocused = false;
    await view.rerender(ui());
    await advance(5_000);
    expect(countdown()).toHaveTextContent('30:00');
    // Back in view it shows the right time at once.
    mockFocused = true;
    await view.rerender(ui());
    await waitFor(() => expect(countdown()).toHaveTextContent(/^29:5\d$/));
  });

  it('asks again when the last known deadline passes instead of saying the season is over', async () => {
    jest.useFakeTimers({ now: T0 });
    // GW 8 locks in 30 s; the server only reveals GW 9 once it has.
    status = (now) =>
      now < T0 + 30_000
        ? {
            current: gw(7, T0 - 48 * H),
            next: gw(8, T0 + 30_000),
            bonusAdded: false,
            dataChecked: false,
            waiversProcessed: true,
          }
        : {
            current: gw(8, T0 + 30_000),
            next: gw(9, T0 + 7 * 24 * H),
            bonusAdded: false,
            dataChecked: false,
            waiversProcessed: false,
          };
    await render(ui());
    await waitFor(() => expect(countdown()).toHaveTextContent('0:30'));
    for (let i = 0; i < 40; i++) {
      await advance(1_000);
      expect(screen.queryByTestId('deadlines-empty')).toBeNull();
    }
    await waitFor(() =>
      expect(screen.getByTestId('deadline-next')).toHaveTextContent(/GAMEWEEK 9/),
    );
    expect(gets).toBeGreaterThanOrEqual(2);
  });

  it('says the season is over only when the server has no next gameweek', async () => {
    jest.useFakeTimers({ now: T0 });
    status = () => ({
      current: gw(38, T0 - 72 * H, true),
      next: null,
      bonusAdded: true,
      dataChecked: true,
      waiversProcessed: true,
    });
    await render(ui());
    await waitFor(() => expect(screen.getByTestId('deadlines-empty')).toBeOnTheScreen());
  });
});
