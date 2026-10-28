/**
 * The league's top tabs and the app's bottom bar, rendered through the
 * router against the MSW handlers: which tab is selected for a URL, what a
 * tab press does to the URL, and that each section keeps its place.
 */
import { send, type Schemas } from '@myfantasyprem/api-client';
import { QueryClient } from '@tanstack/react-query';
import { fireEvent, screen } from '@testing-library/react-native';
import { renderRouter, waitFor } from 'expo-router/testing-library';
import { getResponse } from 'msw';

import { api } from '@/api/client';
import { PLAYERS_FIRST_FILTER, queryKeys } from '@/api/hooks';
import { prefetchLeagueTab } from '@/api/prefetch';
import { statusKeys } from '@/api/status-hooks';
import { memorySessionStore, tokens, type StoredSession } from '@/auth/session';
import { findMyMatchup, leagueGameweek } from '@/lib/matchup';
import * as data from '@/mocks/data';
import { handlers, resetMocks } from '@/mocks/handlers';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));

let mockStore = memorySessionStore();
jest.mock('@/auth/session-store', () => ({
  get platformSessionStore() {
    return mockStore;
  },
}));

const SESSION: StoredSession = {
  accessToken: 'mock-access-token',
  refreshToken: 'mock-refresh-token-0001',
  expiresAt: '2099-01-01T00:00:00Z',
  refreshExpiresAt: '2099-01-01T00:00:00Z',
  user: {
    id: data.DEMO_USER_ID,
    displayName: data.DEMO_USER_NAME,
    email: 'sam@example.com',
    avatarColor: '#2f6fed',
    authProviders: ['email'],
    createdAt: '2026-07-01T09:00:00Z',
  },
};

const L = `/leagues/${data.LEAGUE_ID}`;
const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const response = await getResponse(handlers, new Request(input, init));
    if (!response) throw new Error(`No mock handler for ${String(input)}`);
    return response;
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});
beforeEach(() => {
  resetMocks();
  tokens.set(null);
  mockStore = memorySessionStore(SESSION);
});

const selected = (testID: string) =>
  screen.getByTestId(testID).props.accessibilityState?.selected === true;

/** The one top tab marked selected. */
const selectedTab = () =>
  ['index', 'matchup', 'team', 'players', 'league'].filter((t) => selected(`tab-${t}`));

describe('league top tabs', () => {
  it('opens a league on STATUS, with the tabs in pager order', async () => {
    const view = renderRouter('./src/app', { initialUrl: L });
    await view;
    expect(await screen.findByTestId('league-tabs')).toBeOnTheScreen();
    const labels = screen
      .getAllByRole('tab')
      .map((t) => t.props.accessibilityLabel)
      .filter((l) => ['Status', 'Match', 'Team', 'Players', 'League'].includes(l));
    expect(labels).toEqual(['Status', 'Match', 'Team', 'Players', 'League']);
    expect(selectedTab()).toEqual(['index']);
    expect(view.getPathname()).toBe(L);
  });

  it.each([
    ['/matchup', 'matchup'],
    ['/team', 'team'],
    ['/players', 'players'],
    ['/league', 'league'],
    ['/players/waivers', 'players'],
  ])('a deep link to %s selects its tab', async (path, tab) => {
    const view = renderRouter('./src/app', { initialUrl: `${L}${path}` });
    await view;
    await screen.findByTestId('league-tabs');
    await waitFor(() => expect(selectedTab()).toEqual([tab]));
    expect(view.getPathname()).toBe(`${L}${path}`);
  });

  it('a tab press changes the tab and the URL, and back to STATUS', async () => {
    const view = renderRouter('./src/app', { initialUrl: L });
    await view;
    await fireEvent.press(await screen.findByTestId('tab-players'));
    await waitFor(() => expect(view.getPathname()).toBe(`${L}/players`));
    expect(selectedTab()).toEqual(['players']);
    await fireEvent.press(screen.getByTestId('tab-league'));
    await waitFor(() => expect(view.getPathname()).toBe(`${L}/league`));
    expect(selectedTab()).toEqual(['league']);
    await fireEvent.press(screen.getByTestId('tab-index'));
    await waitFor(() => expect(view.getPathname()).toBe(L));
    expect(selectedTab()).toEqual(['index']);
  });

  it('Activity is not a tab: it opens from LEAGUE with a way back', async () => {
    const view = renderRouter('./src/app', { initialUrl: `${L}/league` });
    await view;
    expect(screen.queryByTestId('tab-activity')).toBeNull();
    await fireEvent.press(await screen.findByTestId('link-activity'));
    await waitFor(() => expect(view.getPathname()).toBe(`${L}/activity`));
    expect(await screen.findByTestId('back-link')).toHaveTextContent(/League/);
  });
});

describe('app bottom bar', () => {
  it('shows under a league and switches sections, keeping the league where it was', async () => {
    const view = renderRouter('./src/app', { initialUrl: `${L}/team` });
    await view;
    await screen.findByTestId('app-tabs');
    expect(selected('app-tab-leagues')).toBe(true);
    await fireEvent.press(screen.getByTestId('app-tab-scores'));
    await waitFor(() => expect(view.getPathname()).toBe('/scores'));
    expect(await screen.findByTestId('scores-list')).toBeOnTheScreen();
    expect(selected('app-tab-scores')).toBe(true);
    await fireEvent.press(screen.getByTestId('app-tab-feed'));
    await waitFor(() => expect(view.getPathname()).toBe('/feed'));
    await fireEvent.press(screen.getByTestId('app-tab-leagues'));
    await waitFor(() => expect(view.getPathname()).toBe(`${L}/team`));
    // Pressing the section you are in goes back to its first screen: the hub.
    await fireEvent.press(screen.getByTestId('app-tab-leagues'));
    await waitFor(() => expect(view.getPathname()).toBe('/leagues'));
  });

  it('opens ACCOUNT as a section', async () => {
    const view = renderRouter('./src/app', { initialUrl: '/leagues' });
    await view;
    await fireEvent.press(await screen.findByTestId('app-tab-account'));
    await waitFor(() => expect(view.getPathname()).toBe('/account'));
    expect(selected('app-tab-account')).toBe(true);
  });
});

describe('prefetching a tab', () => {
  it('loads each tab’s first query under the key its screen reads', async () => {
    tokens.set(SESSION);
    const queryClient = new QueryClient();
    const home = (await send(
      api.GET('/v1/leagues/{leagueId}', { params: { path: { leagueId: data.LEAGUE_ID } } }),
    )) as { data: Schemas['LeagueHome'] };
    const id = data.LEAGUE_ID;
    for (const tab of ['index', 'matchup', 'team', 'players', 'league'] as const) {
      await prefetchLeagueTab(queryClient, id, tab, home.data);
    }
    const gw = leagueGameweek(home.data);
    expect(queryClient.getQueryData(statusKeys.status(id))).toBeDefined();
    expect(queryClient.getQueryData(queryKeys.matchups(id, gw))).toBeDefined();
    expect(queryClient.getQueryData(queryKeys.team(id, home.data.viewer.team!.id))).toBeDefined();
    expect(queryClient.getQueryData(queryKeys.players(id, PLAYERS_FIRST_FILTER))).toBeDefined();
    const mine = findMyMatchup(home.data.matchups, home.data.viewer.team!.id)!;
    expect(queryClient.getQueryData(queryKeys.matchup(id, gw, mine.id))).toBeDefined();
    queryClient.clear();
  });
});
