/**
 * Account → Notifications and the FEED inbox, through the router against
 * the stateful MSW handlers: grouping, a switch sends a PUT for its league,
 * All off turns every switch off, and the inbox's unread state and badge.
 */
import { fireEvent, screen, within } from '@testing-library/react-native';
import { renderRouter, waitFor } from 'expo-router/testing-library';
import { getResponse } from 'msw';

import { persistOptions } from '@/api/persist';
import { memorySessionStore, tokens, type StoredSession } from '@/auth/session';
import { groupTypes } from '@/lib/notifications';
import { pushSupport } from '@/lib/web-push';
import * as data from '@/mocks/data';
import { handlers, resetMocks } from '@/mocks/handlers';
import { NOTIFICATION_TYPES } from '@/mocks/notifications';

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

const puts: unknown[] = [];
const reads: unknown[] = [];
const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    const path = new URL(request.url).pathname;
    if (request.method === 'PUT' && path === '/v1/me/notifications/preferences')
      puts.push(await request.clone().json());
    if (request.method === 'POST' && path === '/v1/me/notifications/read')
      reads.push(await request.clone().json());
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
  mockStore = memorySessionStore(SESSION);
  puts.length = 0;
  reads.length = 0;
});

const switchOn = (type: string) => screen.getByTestId(`notification-switch-${type}`).props.value;

describe('groupTypes', () => {
  it('puts Personal first, then League, then unknown groups by name', () => {
    const groups = groupTypes([
      { type: 'opponent_goal', group: 'league', label: 'a' },
      { type: 'brand_new', group: 'cup', label: 'b' },
      { type: 'player_goal', group: 'personal', label: 'c' },
    ]);
    expect(groups.map((g) => g.title)).toEqual(['Personal', 'League', 'cup']);
  });
});

describe('pushSupport', () => {
  const env = { userAgent: 'Mozilla/5.0 (X11)', standalone: false, secure: true, hasPush: true };
  it('is native off the web', () => {
    // Jest runs the iOS platform.
    expect(pushSupport(env)).toBe('native');
  });
});

describe('notification settings', () => {
  it('lists every type under Personal and League, all on by default', async () => {
    await renderRouter('./src/app', { initialUrl: '/account/notifications' });
    const personal = await screen.findByTestId(
      'notification-group-personal',
      {},
      { timeout: 5000 },
    );
    const league = screen.getByTestId('notification-group-league');
    expect(within(personal).getByText('My players: goals')).toBeOnTheScreen();
    expect(within(league).getByText("Opponent's players: goals")).toBeOnTheScreen();
    for (const t of NOTIFICATION_TYPES) expect(switchOn(t.type)).toBe(true);
    // The native app explains push comes later.
    expect(screen.getByTestId('push-unavailable')).toHaveTextContent(/iOS app/);
  });

  it('a switch sends a PUT for the picked league and stays off', async () => {
    await renderRouter('./src/app', { initialUrl: '/account/notifications' });
    await screen.findByTestId('notification-league-picker', {}, { timeout: 5000 });
    await fireEvent.press(screen.getByTestId(`notification-league-${data.OFFICE_LEAGUE_ID}`));
    await waitFor(() =>
      expect(screen.getByTestId(`notification-league-${data.OFFICE_LEAGUE_ID}`)).toBeSelected(),
    );
    await fireEvent(screen.getByTestId('notification-switch-player_goal'), 'valueChange', false);
    await waitFor(() =>
      expect(puts).toEqual([
        { leagueId: data.OFFICE_LEAGUE_ID, settings: [{ type: 'player_goal', enabled: false }] },
      ]),
    );
    await waitFor(() => expect(switchOn('player_goal')).toBe(false));
    // Other leagues keep it on.
    await fireEvent.press(screen.getByTestId(`notification-league-${data.LEAGUE_ID}`));
    await waitFor(() =>
      expect(screen.getByTestId(`notification-league-${data.LEAGUE_ID}`)).toBeSelected(),
    );
    await waitFor(() => expect(switchOn('player_goal')).toBe(true));
  });

  it('All off turns every switch off, All on back on', async () => {
    await renderRouter('./src/app', { initialUrl: '/account/notifications' });
    await fireEvent.press(
      await screen.findByTestId('notifications-all-off', {}, { timeout: 5000 }),
    );
    await waitFor(() => {
      for (const t of NOTIFICATION_TYPES) expect(switchOn(t.type)).toBe(false);
    });
    expect(puts[0]).toEqual({ leagueId: expect.stringMatching(/^lg_/), all: false });
    await fireEvent.press(screen.getByTestId('notifications-all-on'));
    await waitFor(() => expect(switchOn('opponent_news')).toBe(true));
  });

  it('opens from Account', async () => {
    const view = renderRouter('./src/app', { initialUrl: '/account' });
    await view;
    await fireEvent.press(
      await screen.findByTestId('account-notifications', {}, { timeout: 5000 }),
    );
    await waitFor(() => expect(view.getPathname()).toBe('/account/notifications'));
  });
});

describe('inbox', () => {
  it('shows unread notifications in FEED with a badge, and marks them read', async () => {
    await renderRouter('./src/app', { initialUrl: '/feed' });
    await screen.findByTestId('inbox', {}, { timeout: 5000 });
    expect(
      await screen.findByTestId('app-tab-feed-badge', { includeHiddenElements: true }),
    ).toHaveTextContent('4');
    expect(screen.getAllByTestId('inbox-unread-dot', { includeHiddenElements: true })).toHaveLength(
      4,
    );
    await fireEvent.press(screen.getByTestId('inbox-item-ntf_01k7m2xq8z0000000000000005'));
    await waitFor(() => expect(reads).toEqual([{ ids: ['ntf_01k7m2xq8z0000000000000005'] }]));
    await waitFor(() =>
      expect(
        screen.getByTestId('app-tab-feed-badge', { includeHiddenElements: true }),
      ).toHaveTextContent('3'),
    );
  });

  it('Mark all read clears the badge', async () => {
    await renderRouter('./src/app', { initialUrl: '/feed' });
    await fireEvent.press(await screen.findByTestId('inbox-mark-all-read', {}, { timeout: 5000 }));
    await waitFor(() =>
      expect(
        screen.queryByTestId('app-tab-feed-badge', { includeHiddenElements: true }),
      ).toBeNull(),
    );
    expect(
      screen.queryAllByTestId('inbox-unread-dot', { includeHiddenElements: true }),
    ).toHaveLength(0);
    expect(reads).toEqual([{ all: true }]);
  });
});
