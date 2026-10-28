/**
 * The FPL sync status, re-sync and checks show in league settings only, and
 * only for an imported league: never on a league tab.
 */
import { screen } from '@testing-library/react-native';
import { renderRouter, waitFor } from 'expo-router/testing-library';
import { getResponse } from 'msw';

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

const realFetch = globalThis.fetch;
let inFlight = 0;
const requested: string[] = [];
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    inFlight += 1;
    requested.push(new Request(input, init).url);
    try {
      const response = await getResponse(handlers, new Request(input, init));
      if (!response) throw new Error(`No mock handler for ${String(input)}`);
      return response;
    } finally {
      inFlight -= 1;
    }
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});
beforeEach(() => {
  resetMocks();
  requested.length = 0;
  tokens.set(null);
  mockStore = memorySessionStore(SESSION);
});
const FPL_TEXT = /Imported from FPL|last synced|Matches FPL|FPL check|Re-synced from FPL/;

const CASES = [
  ['imported', data.IMPORTED_LEAGUE_ID],
  ['imported with mismatches', data.MISMATCH_LEAGUE_ID],
  ['native', data.LEAGUE_ID],
].flatMap(([kind, id]) =>
  ['', '/matchup', '/team', '/players', '/league'].map((tab) => [kind, tab || '/', id, tab]),
);

describe('FPL sync status placement', () => {
  it.each(CASES)('a %s league shows no FPL sync status on %s', async (_, __, id, tab) => {
    await renderRouter('./src/app', { initialUrl: `/leagues/${id}${tab}` });
    expect(await screen.findByTestId('league-tabs', {}, { timeout: 10_000 })).toBeOnTheScreen();
    // Let the league's requests settle (the sync status would be one of them).
    // Long enough for the league's queries to load, the sync status among them.
    const start = Date.now();
    await waitFor(() => expect(Date.now() - start).toBeGreaterThan(1000), { timeout: 3000 });
    await waitFor(() => expect(inFlight).toBe(0));
    expect(screen.queryAllByTestId(/^sync-/)).toHaveLength(0);
    expect(screen.queryAllByText(FPL_TEXT)).toHaveLength(0);
  });

  it('shows the sync panel in settings of an imported league', async () => {
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.IMPORTED_LEAGUE_ID}/settings` });
    expect(await screen.findByTestId('sync-panel', {}, { timeout: 10_000 })).toBeOnTheScreen();
    expect(screen.getByText(/^Imported from FPL · last synced/)).toBeOnTheScreen();
    expect(screen.getByTestId('resync')).toBeOnTheScreen();
  });

  it('shows nothing about FPL in settings of a native league', async () => {
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.LEAGUE_ID}/settings` });
    expect(
      await screen.findByTestId('settings-league-name', {}, { timeout: 10_000 }),
    ).toBeOnTheScreen();
    expect(screen.queryByTestId('sync-panel')).toBeNull();
    expect(screen.queryByTestId('resync')).toBeNull();
    expect(screen.queryByText(/FPL/)).toBeNull();
  });

  it.each([
    ['imported', data.IMPORTED_LEAGUE_ID, true],
    ['native', data.LEAGUE_ID, false],
  ])(
    'a %s league tab still watches the sync status (no banner) so a re-sync refreshes it',
    async (_, id, watches) => {
      await renderRouter('./src/app', { initialUrl: `/leagues/${id}/team` });
      expect(await screen.findByTestId('league-tabs', {}, { timeout: 10_000 })).toBeOnTheScreen();
      const start = Date.now();
      await waitFor(() => expect(Date.now() - start).toBeGreaterThan(1000), { timeout: 3000 });
      await waitFor(() => expect(inFlight).toBe(0));
      const synced = requested.some((u) => new URL(u).pathname === `/v1/leagues/${id}/sync`);
      expect(synced).toBe(watches);
      expect(screen.queryAllByText(FPL_TEXT)).toHaveLength(0);
    },
  );
});
