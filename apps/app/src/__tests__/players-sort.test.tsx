/**
 * The Players tab's sort against the MSW handlers: the first request is
 * the one the league prefetches, a stored sort is read before anything is
 * asked for, and rows keep the label of the values they show.
 */
import AsyncStorage from '@react-native-async-storage/async-storage';
import type { QueryClient } from '@tanstack/react-query';
import { act, fireEvent, screen } from '@testing-library/react-native';
import { renderRouter, waitFor } from 'expo-router/testing-library';
import { getResponse } from 'msw';

import { PLAYERS_FIRST_FILTER, queryKeys } from '@/api/hooks';
import { memorySessionStore, tokens, type StoredSession } from '@/auth/session';
import { sortStorageKey } from '@/lib/playerSort';
import { viewStorageKey } from '@/lib/playerView';
import * as data from '@/mocks/data';
import { handlers, resetMocks } from '@/mocks/handlers';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));

// jest.setup.js gives FlashList a 20000px window so most lists draw every
// row. Here that window is never full: the Players list would page through
// the whole player pool, re-rendering every row for each page, before (and
// while) a test gets to what it checks. A phone-sized window pages only when
// asked, as on a device.
jest.mock('@shopify/flash-list/dist/recyclerview/utils/measureLayout', () => {
  const actual = jest.requireActual('@shopify/flash-list/dist/recyclerview/utils/measureLayout');
  const box = (width: number, height: number) => jest.fn(() => ({ x: 0, y: 0, width, height }));
  return {
    ...actual,
    measureParentSize: box(400, 900),
    measureFirstChildLayout: box(400, 900),
    measureItemLayout: box(400, 60),
  };
});

let mockStore = memorySessionStore();
jest.mock('@/auth/session-store', () => ({
  get platformSessionStore() {
    return mockStore;
  },
}));

let mockClient: QueryClient | null = null;
jest.mock('@/api/query-client', () => {
  const actual = jest.requireActual('@/api/query-client');
  return {
    ...actual,
    createQueryClient: () => {
      mockClient = actual.createQueryClient();
      return mockClient;
    },
  };
});

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

const PLAYERS = `/leagues/${data.LEAGUE_ID}/players`;
/** The query strings of every players list request, in order. */
const listRequests: URLSearchParams[] = [];
/** When set, a players request is held until this resolves. */
let hold: ((url: URL) => Promise<void> | null) | null = null;
/** When set, /clubs answers 503. */
let failClubs = false;

const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    const url = new URL(request.url);
    if (failClubs && url.pathname.endsWith('/clubs')) {
      return new Response(JSON.stringify({ title: 'Down', status: 503 }), {
        status: 503,
        headers: { 'content-type': 'application/problem+json' },
      });
    }
    if (url.pathname.endsWith('/players')) {
      listRequests.push(url.searchParams);
      const wait = hold?.(url);
      if (wait) await wait;
    }
    const response = await getResponse(handlers, request);
    if (!response) throw new Error(`No mock handler for ${request.method} ${request.url}`);
    return response;
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});
beforeEach(async () => {
  resetMocks();
  tokens.set(null);
  mockStore = memorySessionStore(SESSION);
  mockClient = null;
  hold = null;
  failClubs = false;
  listRequests.length = 0;
  await AsyncStorage.clear();
});

describe('the Players tab sort', () => {
  it('asks first under the key the league prefetches', async () => {
    await renderRouter('./src/app', { initialUrl: PLAYERS });
    await screen.findAllByTestId(/^player-plr_/);
    const key = queryKeys.players(data.LEAGUE_ID, PLAYERS_FIRST_FILTER);
    // The screen reads that query (it observes it), so a prefetch serves it.
    expect(
      mockClient!.getQueryCache().find({ queryKey: key, exact: true })?.getObserversCount(),
    ).toBe(1);
  });

  it('reads a stored sort before asking for any list', async () => {
    await AsyncStorage.setItem(
      sortStorageKey(data.LEAGUE_ID),
      JSON.stringify({ sort: 'goals', order: 'asc' }),
    );
    await renderRouter('./src/app', { initialUrl: PLAYERS });
    await screen.findAllByTestId(/^player-plr_/);
    // The screen's first request is the stored sort, not the default (the
    // league's idle prefetch of the default list may follow).
    expect(listRequests[0]?.get('sort')).toBe('goals');
    // An order stored by an older build is ignored: never ascending.
    expect(listRequests.every((q) => !q.has('order'))).toBe(true);
  });

  it('keeps the shown values under their own label while a new sort loads', async () => {
    await renderRouter('./src/app', { initialUrl: PLAYERS });
    const [row] = await screen.findAllByTestId(/^player-plr_/);
    const hint = () =>
      screen.getAllByTestId(/^open-card-plr_/)[0]!.props.accessibilityHint as string;
    const goals = /\bG(oals)?\b/;
    expect(row).toBeDefined();
    expect(hint()).not.toMatch(goals);
    let release = () => {};
    hold = (url) =>
      url.searchParams.get('sort') === 'goals'
        ? new Promise<void>((r) => {
            release = r;
          })
        : null;
    await act(async () => fireEvent.press(screen.getByTestId('sort-menu')));
    const option = await screen.findByTestId('sort-goals');
    await act(async () => fireEvent.press(option));
    await waitFor(() => expect(listRequests.some((q) => q.get('sort') === 'goals')).toBe(true));
    // Still the total points list: its rows are not labelled goals.
    expect(hint()).not.toMatch(goals);
    release();
    await waitFor(() => expect(hint()).toMatch(goals));
    // A screen reader hears names, not "G 3", and no direction.
    expect(hint()).toMatch(/, Goals \d+(,|$)/);
    expect(listRequests.every((q) => !q.has('order'))).toBe(true);
    await act(async () => fireEvent.press(screen.getByTestId('sort-menu')));
    expect(screen.queryByTestId('sort-order-asc')).toBeNull();
    expect(screen.queryByTestId('sort-order-desc')).toBeNull();
    expect(screen.queryByText(/ascending|descending/i)).toBeNull();
    expect(screen.getByTestId('sort-menu').props.accessibilityLabel).toBe('Sort: Goals. Change');
  });
});

describe('the Players tab View and Available', () => {
  const label = () => screen.getByTestId('view-menu').props.accessibilityLabel as string;

  it('lists one choice at a time and labels the button with it', async () => {
    await renderRouter('./src/app', { initialUrl: PLAYERS });
    await screen.findAllByTestId(/^player-plr_/);
    expect(label()).toBe('View: All players. Change');
    // The old separate controls are gone.
    for (const id of ['position-all', 'position-DEF', 'club-filter', 'open-filters']) {
      expect(screen.queryByTestId(id)).toBeNull();
    }
    await act(async () => fireEvent.press(screen.getByTestId('view-menu')));
    const options = await screen.findAllByTestId(/^view-(all|pos-|club-)/);
    const names = options.map((o) => o.props.accessibilityLabel as string);
    expect(names.slice(0, 5)).toEqual([
      'All players',
      'Goalkeepers',
      'Defenders',
      'Midfielders',
      'Forwards',
    ]);
    const clubs = names.slice(5);
    expect(clubs.length).toBeGreaterThan(1);
    expect(clubs).toEqual([...clubs].sort((a, b) => a.localeCompare(b)));
    expect(options.filter((o) => o.props.accessibilityState?.checked)).toHaveLength(1);
    await act(async () => fireEvent.press(screen.getByTestId('view-pos-DEF')));
    await waitFor(() => expect(label()).toBe('View: Defenders. Change'));
    await waitFor(() => expect(listRequests.at(-1)?.get('position')).toBe('DEF'));
    // A club replaces the position: one choice at a time.
    await act(async () => fireEvent.press(screen.getByTestId('view-menu')));
    const club = (await screen.findAllByTestId(/^view-club-/))[0]!;
    const clubName = club.props.accessibilityLabel as string;
    await act(async () => fireEvent.press(club));
    await waitFor(() => expect(label()).toBe(`View: ${clubName}. Change`));
    await waitFor(() => expect(listRequests.at(-1)?.has('club')).toBe(true));
    expect(listRequests.at(-1)?.has('position')).toBe(false);
  });

  it('toggles Available between unowned players and everyone', async () => {
    await renderRouter('./src/app', { initialUrl: PLAYERS });
    await screen.findAllByTestId(/^player-plr_/);
    const toggle = () => screen.getByTestId('filter-available');
    expect(toggle().props.accessibilityState.selected).toBe(true);
    expect(listRequests[0]?.get('status')).toBe('available');
    await act(async () => fireEvent.press(toggle()));
    await waitFor(() => expect(listRequests.at(-1)?.get('status')).toBe('all'));
    expect(toggle().props.accessibilityState.selected).toBe(false);
    await waitFor(async () =>
      expect(JSON.parse((await AsyncStorage.getItem(viewStorageKey(data.LEAGUE_ID)))!)).toEqual({
        view: { kind: 'all' },
        available: false,
      }),
    );
  });

  it('reads the stored View and Available before asking for any list', async () => {
    await AsyncStorage.setItem(
      viewStorageKey(data.LEAGUE_ID),
      JSON.stringify({ view: { kind: 'position', position: 'MID' }, available: false }),
    );
    await renderRouter('./src/app', { initialUrl: PLAYERS });
    await screen.findAllByTestId(/^player-plr_/);
    expect(listRequests[0]?.get('position')).toBe('MID');
    expect(listRequests[0]?.get('status')).toBe('all');
    expect(label()).toBe('View: Midfielders. Change');
  });

  it('Search looks through every player, owned ones too, with Available on', async () => {
    const owned = data.PLAYERS.find((p) => data.ownerOf(p.id))!;
    await renderRouter('./src/app', { initialUrl: `${PLAYERS}?view=search` });
    const field = await screen.findByTestId('player-search');
    await act(async () => fireEvent.changeText(field, owned.displayName));
    await waitFor(() => expect(listRequests.at(-1)?.get('q')).toBe(owned.displayName), {
      timeout: 3000,
    });
    expect(listRequests.at(-1)?.get('status')).toBe('all');
    await screen.findByTestId(`player-${owned.id}`, {}, { timeout: 3000 });
    // The league's Available toggle is untouched.
    expect(
      JSON.parse((await AsyncStorage.getItem(viewStorageKey(data.LEAGUE_ID))) ?? 'null'),
    ).toBeNull();
  });

  it('ignores a stale stored view', async () => {
    await AsyncStorage.setItem(
      viewStorageKey(data.LEAGUE_ID),
      JSON.stringify({ view: { kind: 'club', code: 'ZZZ' }, available: 'maybe' }),
    );
    await renderRouter('./src/app', { initialUrl: PLAYERS });
    await screen.findAllByTestId(/^player-plr_/);
    await waitFor(() => expect(label()).toBe('View: All players. Change'));
    expect(screen.getByTestId('filter-available').props.accessibilityState.selected).toBe(true);
    // The club is checked against the club list before anything is asked for.
    expect(listRequests.some((q) => q.has('club'))).toBe(false);
  });

  it('a stored club waits for the club list; when that fails it is All players', async () => {
    failClubs = true;
    const club = data.CLUBS[0]!.code;
    await AsyncStorage.setItem(
      viewStorageKey(data.LEAGUE_ID),
      JSON.stringify({ view: { kind: 'club', code: club }, available: true }),
    );
    await renderRouter('./src/app', { initialUrl: PLAYERS });
    await screen.findAllByTestId(/^player-plr_/, {}, { timeout: 5000 });
    expect(label()).toBe('View: All players. Change');
    expect(listRequests.some((q) => q.has('club'))).toBe(false);
  });
});
