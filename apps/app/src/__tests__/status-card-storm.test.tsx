/**
 * Status → Top Available: opening and closing ten player cards in a row
 * (and the Sign → drop step) stays bounded: each card shows its own player,
 * the card renders a handful of times per open, each player is fetched
 * once, and the saved query cache is written a bounded number of times.
 * Against the stateful MSW handlers, as screens.test.tsx.
 */
import AsyncStorage from '@react-native-async-storage/async-storage';
import { act, fireEvent, screen, within } from '@testing-library/react-native';
import { renderRouter, waitFor } from 'expo-router/testing-library';
import { getResponse } from 'msw';

import { persistOptions } from '@/api/persist';
import { memorySessionStore, tokens, type StoredSession } from '@/auth/session';
import * as data from '@/mocks/data';
import { handlers, resetMocks } from '@/mocks/handlers';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));

let mockStore = memorySessionStore();
jest.mock('@/auth/session-store', () => ({
  get platformSessionStore() {
    return mockStore;
  },
}));

/** Renders of the player card (its useLeaguePlayer call), by player id. */
const mockCardRenders = new Map<string, number>();
jest.mock('@/api/hooks', () => {
  const actual = jest.requireActual('@/api/hooks');
  return {
    ...actual,
    useLeaguePlayer: (leagueId: string, playerId: string) => {
      mockCardRenders.set(playerId, (mockCardRenders.get(playerId) ?? 0) + 1);
      return actual.useLeaguePlayer(leagueId, playerId);
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

const requests: string[] = [];
const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    requests.push(`${request.method} ${new URL(request.url).pathname}`);
    const response = await getResponse(handlers, request);
    if (!response) throw new Error(`No mock handler for ${request.method} ${request.url}`);
    return response;
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});
beforeEach(async () => {
  await persistOptions.persister.removeClient();
  resetMocks();
  requests.length = 0;
  mockCardRenders.clear();
  tokens.set(null);
});

const CACHE_KEY = 'mfp.queryCache';
const cacheWrites = () =>
  (AsyncStorage.setItem as jest.Mock).mock.calls.filter(([k]) => k === CACHE_KEY).length;

describe('Status top available: card after card', () => {
  it('opens and closes ten cards and a drop step without a storm', async () => {
    mockStore = memorySessionStore(SESSION);
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.LEAGUE_ID}` });
    await screen.findByTestId('status-loaded', {}, { timeout: 8000 });
    // Everything the screen asks for on its own has landed.
    await act(async () => {
      jest.advanceTimersByTime(1500);
    });

    const rows = screen.getAllByTestId(/^top-plr_/).map((n) => n.props.testID.slice(4) as string);
    const ids = rows.slice(0, 10);
    expect(ids.length).toBeGreaterThanOrEqual(8);

    const before = requests.length;
    (AsyncStorage.setItem as jest.Mock).mockClear();

    for (const id of ids) {
      const row = screen.getByTestId(`top-${id}`);
      const open = within(row).getByLabelText(/^Player card for /);
      const name = (open.props.accessibilityLabel as string).replace('Player card for ', '');
      await fireEvent(open, 'pressIn');
      await fireEvent.press(open);
      const card = await screen.findByTestId('player-card');
      // The card shows the player whose row was tapped.
      const last = name
        .split(' ')
        .pop()!
        .replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      await waitFor(() =>
        expect(within(card).getAllByText(new RegExp(last)).length).toBeGreaterThan(0),
      );
      expect(card).toBeOnTheScreen();
      await fireEvent.press(screen.getByTestId('sheet-close'));
      await waitFor(() => expect(screen.queryByTestId('player-card')).toBeNull());
    }

    // Sign → the drop step, then back out.
    const signId = ids[0]!;
    await fireEvent.press(screen.getByTestId(`sign-${signId}`));
    expect(await screen.findByTestId('drop-step')).toBeOnTheScreen();
    await fireEvent.press(screen.getByTestId('sheet-close'));
    await waitFor(() => expect(screen.queryByTestId('player-card')).toBeNull());

    await act(async () => {
      jest.advanceTimersByTime(1500);
    });

    const during = requests.slice(before);
    const playerGets = during.filter((r) => /\/players\/plr_/.test(r));
    const perPlayer = new Map<string, number>();
    for (const r of playerGets) perPlayer.set(r, (perPlayer.get(r) ?? 0) + 1);
    const renders = [...mockCardRenders.values()];
    // Each player's card data is fetched once.
    expect(Math.max(...perPlayer.values())).toBe(1);
    expect(during.length).toBeLessThanOrEqual(ids.length + 6);
    // A handful of renders per open, not a loop.
    expect(Math.max(...renders)).toBeLessThanOrEqual(12);
    // The saved cache is throttled: far fewer writes than cache events.
    expect(cacheWrites()).toBeLessThanOrEqual(ids.length + 4);
  }, 60_000);
});
