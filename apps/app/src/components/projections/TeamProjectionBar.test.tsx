import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { TeamProjectionBar } from './TeamProjectionBar';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));
jest.mock('expo-router', () => ({ useIsFocused: () => true }));

const LEAGUE = 'lg_01k7m2xq800000000000000001';
const HOME = 'tm_01k7m2xq800000000000000001';
const AWAY = 'tm_01k7m2xq800000000000000002';

const body = {
  data: {
    gameweek: 8,
    formGameweek: 8,
    recentGameweeks: [2, 3, 4, 5, 6, 7],
    players: [],
    teams: [
      { team: { id: HOME, name: 'Home' }, expected: 52.4, lineupBasis: 'saved', starters: [] },
      { team: { id: AWAY, name: 'Away' }, expected: 47.1, lineupBasis: 'saved', starters: [] },
    ],
    matchups: [
      {
        matchupId: 'mu_01k7m2xq800000000000000001',
        homeTeamId: HOME,
        awayTeamId: AWAY,
        mode: 'pre',
        odds: { home: 0.6, draw: 0.1, away: 0.3 },
        samples: 10_000,
      },
    ],
  },
  meta: {
    dataAsOf: '2026-10-17T10:00:00Z',
    serverTime: '2026-10-17T10:00:00Z',
    stale: false,
  },
};

const realFetch = globalThis.fetch;
const realSwitch = process.env.EXPO_PUBLIC_SHOW_PROJECTIONS;
let urls: string[];
let client: QueryClient;
beforeEach(() => {
  urls = [];
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    urls.push(new Request(input, init).url);
    return new Response(JSON.stringify(body), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  }) as typeof fetch;
});
afterEach(() => {
  // Stop the live poll so Jest can exit.
  client.clear();
  globalThis.fetch = realFetch;
  if (realSwitch === undefined) delete process.env.EXPO_PUBLIC_SHOW_PROJECTIONS;
  else process.env.EXPO_PUBLIC_SHOW_PROJECTIONS = realSwitch;
});

const renderBar = () => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return render(<TeamProjectionBar leagueId={LEAGUE} teamId={HOME} gameweek={8} />, {
    wrapper,
  });
};

describe('the projections switch (EXPO_PUBLIC_SHOW_PROJECTIONS)', () => {
  it('is off by default: no Expected, no win chance, and no projections request', async () => {
    delete process.env.EXPO_PUBLIC_SHOW_PROJECTIONS;
    await renderBar();
    // Give a fetch the chance to happen if the query were enabled.
    await new Promise((r) => setTimeout(r, 20));
    expect(urls).toEqual([]);
    expect(screen.queryByTestId('team-projection')).toBeNull();
    expect(screen.queryByTestId('team-win-chance')).toBeNull();
  });

  it('when "true", fetches the gameweek and shows Expected and the win chance', async () => {
    process.env.EXPO_PUBLIC_SHOW_PROJECTIONS = 'true';
    await renderBar();
    await waitFor(() => expect(screen.getByTestId('team-projection')).toBeTruthy());
    expect(urls).toEqual([`http://api.test/v1/leagues/${LEAGUE}/gameweeks/8/projections`]);
    expect(screen.getByTestId('team-expected')).toHaveTextContent(/Exp 52\.4 · Opp 47\.1 · GW 8/);
    // Home 0.6 is the left side: 60%.
    expect(screen.getByTestId('team-win-chance-left')).toHaveTextContent('60%');
  });
});
