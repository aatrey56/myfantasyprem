/**
 * Sign-in links (docs/adr/0013) opened by someone already signed in. Its
 * own file: a second sign-in link render in one file keeps the previous
 * render's router state (expo-router's store is module-level).
 */
import { fireEvent, screen, within } from '@testing-library/react-native';
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

jest.mock('@/lib/share', () => ({
  canShare: () => false,
  copyText: jest.fn(async () => true),
  shareLink: jest.fn(),
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
let redeemed = 0;
/** How many times the app sent a sign-in link's code. */
const redeems = () => redeemed;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    if (request.method === 'POST' && request.url.endsWith('/v1/auth/signin-link')) redeemed++;
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
  tokens.set(null);
  redeemed = 0;
});

describe('sign-in links, signed in', () => {
  it('asks before replacing someone signed in', async () => {
    const path = await issueLink();
    const stored = memorySessionStore(SESSION);
    mockStore = stored;
    const view = renderRouter('./src/app', { initialUrl: path });
    await waitFor(() => expect(view.getPathname()).toBe('/signin-link'), { timeout: 5000 });
    expect(
      within(await screen.findByTestId('signin-link-signed-in', {}, { timeout: 5000 })).getByText(
        `You're signed in as ${data.DEMO_USER_NAME} Sign out and use this link?`,
      ),
    ).toBeOnTheScreen();
    expect(screen.queryByTestId('signin-link-ready')).toBeNull();
    expect(redeems()).toBe(0);
    await fireEvent.press(screen.getByTestId('signin-link-confirm'));
    await waitFor(async () => expect((await stored.load())?.user.displayName).toBe('Priya N.'));
    expect(redeems()).toBe(1);
  });
});

/** Issues a sign-in link for Priya's team as the commissioner (mocks). */
async function issueLink(): Promise<string> {
  const issued = await getResponse(
    handlers,
    new Request(
      `http://api.test/v1/leagues/${data.IMPORTED_LEAGUE_ID}/seats/${await priyaTeam()}/signin-link`,
      { method: 'POST', headers: { Authorization: 'Bearer mock-access-token' } },
    ),
  );
  const url: string = ((await issued!.json()) as { data: { url: string } }).data.url;
  return url.slice(url.indexOf('/signin-link/'));
}

async function priyaTeam(): Promise<string> {
  const res = await getResponse(
    handlers,
    new Request(`http://api.test/v1/leagues/${data.IMPORTED_LEAGUE_ID}/seats`, {
      headers: { Authorization: 'Bearer mock-access-token' },
    }),
  );
  const seats = (
    (await res!.json()) as {
      data: { team: { id: string }; owner: { displayName: string } | null }[];
    }
  ).data;
  return seats.find((s) => s.owner?.displayName === 'Priya N.')!.team.id;
}
