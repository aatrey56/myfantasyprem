/**
 * Sign-in links (docs/adr/0013) against the stateful MSW handlers: the
 * commissioner makes one for a claimed team in league settings, and
 * opening it signed out signs that manager in and opens the league.
 */
import { act, fireEvent, screen, within } from '@testing-library/react-native';
import { router } from 'expo-router';
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

describe('sign-in links', () => {
  it('the commissioner makes one for a claimed team, with copy and how it works', async () => {
    mockStore = memorySessionStore(SESSION);
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.IMPORTED_LEAGUE_ID}/settings` });
    const links = await screen.findByTestId('claim-links', {}, { timeout: 5000 });
    // Only claimed teams that are not mine: Priya's.
    const rows = within(links).getAllByTestId('signin-link');
    expect(rows).toHaveLength(1);
    expect(within(links).queryByTestId('signin-link-url')).toBeNull();
    await fireEvent.press(within(rows[0]!).getByTestId('signin-link-issue'));
    expect(await within(links).findByTestId('signin-link-help')).toHaveTextContent(
      'Send this to Priya N. to sign back in. Works once, expires in 7 days.',
    );
    const url = within(links).getByTestId('signin-link-url');
    expect(url).toHaveTextContent(/\/signin-link\/[A-Za-z0-9_-]{43}$/);
    await fireEvent.press(within(links).getByTestId('signin-link-copy'));
    const share = jest.requireMock('@/lib/share') as { copyText: jest.Mock };
    expect(share.copyText).toHaveBeenCalledWith(url.props.children);
    expect(await screen.findByText('Sign-in link copied')).toBeOnTheScreen();
  });

  it('opening a link takes the code out of the URL at once and spends it only on a tap', async () => {
    const path = await issueLink();
    mockStore = memorySessionStore();
    const view = renderRouter('./src/app', { initialUrl: path });
    // Out of the URL before anything is sent, and nothing is sent on load
    // (link scanners open links too).
    await waitFor(() => expect(view.getPathname()).toBe('/signin-link'));
    expect(redeems()).toBe(0);
    await fireEvent.press(await screen.findByTestId('signin-link-confirm', {}, { timeout: 5000 }));
    await waitFor(async () => expect((await mockStore.load())?.user.displayName).toBe('Priya N.'));
    await waitFor(() => expect(view.getPathname()).toBe(`/leagues/${data.IMPORTED_LEAGUE_ID}`));
    expect(redeems()).toBe(1);

    // A used link says so, and its code is not back in the URL.
    await act(async () => router.replace(path));
    await fireEvent.press(await screen.findByTestId('signin-link-confirm'));
    expect(await screen.findByTestId('signin-link-problem')).toHaveTextContent(
      /Ask your commissioner for a new sign-in link/,
    );
    expect(view.getPathname()).toBe('/signin-link');
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
