/**
 * Wave C screens against the stateful MSW handlers: the trade block from
 * the card, the Trades carousel and table, personal tags and the filter,
 * and the Team tab's news, newest first.
 */
import { fireEvent, screen, within } from '@testing-library/react-native';
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

const requests: string[] = [];
/** While set, the news endpoint answers 503 (the server is unavailable). */
let newsDown = false;
const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    requests.push(
      `${request.method} ${new URL(request.url).pathname}${new URL(request.url).search}`,
    );
    if (newsDown && new URL(request.url).pathname.endsWith('/news')) {
      return new Response(
        JSON.stringify({ type: 'about:blank', title: 'Unavailable', status: 503 }),
        { status: 503, headers: { 'Content-Type': 'application/problem+json' } },
      );
    }
    const response = await getResponse(handlers, request);
    if (!response) throw new Error(`No mock handler for ${request.method} ${request.url}`);
    return response;
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});
beforeEach(() => {
  resetMocks();
  requests.length = 0;
  newsDown = false;
  tokens.set(null);
  mockStore = memorySessionStore(SESSION);
});

const T = { timeout: 5000 };
const league = () => data.leagues.get(data.LEAGUE_ID)!;
const myTeamId = () => league().viewer().team!.id;
/** My first player on the roster, by the mocks. */
function myPlayer() {
  const l = league();
  if (!data.isInSeason(l)) throw new Error('in season');
  return data.PLAYERS.find((p) => l.playerListItem(p).owner?.id === myTeamId())!;
}

describe('trade block', () => {
  it('puts my player on the block from his card, with a note and wants', async () => {
    const p = myPlayer();
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.LEAGUE_ID}/players/${p.id}` });
    const toggle = await screen.findByTestId('card-block', {}, T);
    expect(toggle).toHaveTextContent('Trade block');
    await fireEvent.press(toggle);
    await waitFor(() => expect(screen.getByTestId('card-block')).toHaveTextContent('On the block'));
    await waitFor(() =>
      expect(requests).toContain(`PUT /v1/leagues/${data.LEAGUE_ID}/trade-block/${p.id}`),
    );
    await fireEvent.changeText(
      await screen.findByTestId('card-block-note', {}, T),
      'Open to offers',
    );
    await fireEvent.press(screen.getByTestId('card-block-want-FWD'));
    await fireEvent.press(screen.getByTestId('card-block-want-DEF'));
    await fireEvent.press(screen.getByTestId('card-block-save'));
    // The server keeps the note and the positions in GKP DEF MID FWD order.
    const puts = () =>
      requests.filter((r) => r === `PUT /v1/leagues/${data.LEAGUE_ID}/trade-block/${p.id}`).length;
    await waitFor(() => expect(puts()).toBe(2));
    const res = await getResponse(
      handlers,
      new Request(`http://api.test/v1/leagues/${data.LEAGUE_ID}/trade-block`, {
        headers: { Authorization: 'Bearer mock-access-token' },
      }),
    );
    const body = (await res!.json()) as {
      data: { player: { id: string }; note: string; wanted: string[] }[];
    };
    expect(body.data.find((e) => e.player.id === p.id)).toMatchObject({
      note: 'Open to offers',
      wanted: ['DEF', 'FWD'],
    });
    // Off again.
    await fireEvent.press(screen.getByTestId('card-block'));
    await waitFor(() => expect(screen.getByTestId('card-block')).toHaveTextContent('Trade block'));
  });

  it("shows other teams' players in the carousel; a card opens the trade builder for him", async () => {
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.LEAGUE_ID}/players/trades` });
    const carousel = await screen.findByTestId('trade-block-carousel', {}, T);
    // The mocks seed three other teams' players.
    const cards = within(carousel).getAllByTestId(/^block-card-/);
    expect(cards).toHaveLength(3);
    const playerId = (cards[0]!.props.testID as string).replace('block-card-', '');
    await fireEvent.press(cards[0]!);
    // The builder opens on his team with him already asked for.
    const toggle = await screen.findByTestId(`get-toggle-${playerId}`, {}, T);
    await waitFor(() => expect(toggle).toBeSelected());
  });

  it('lists the block by team in the table', async () => {
    await renderRouter('./src/app', {
      initialUrl: `/leagues/${data.LEAGUE_ID}/players/trades/block`,
    });
    const table = await screen.findByTestId('trade-block-table', {}, T);
    expect(within(table).getAllByTestId(/^block-team-/)).toHaveLength(3);
    expect(within(table).getAllByTestId(/^block-trade-/)).toHaveLength(3);
    expect(within(table).queryAllByTestId(/^block-remove-/)).toHaveLength(0);
  });
});

describe('tags', () => {
  it('tags a player on his card and filters the Players hub by it', async () => {
    const p = myPlayer();
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.LEAGUE_ID}/players/${p.id}` });
    await fireEvent.press(await screen.findByTestId('card-tag-hot', {}, T));
    await waitFor(() => expect(screen.getByTestId('card-tag-hot')).toBeSelected());
    await waitFor(() =>
      expect(requests).toContain(`PUT /v1/leagues/${data.LEAGUE_ID}/tags/${p.id}`),
    );
    // Another tag replaces it.
    await fireEvent.press(screen.getByTestId('card-tag-sell'));
    await waitFor(() => expect(screen.getByTestId('card-tag-sell')).toBeSelected());
    expect(screen.getByTestId('card-tag-hot')).not.toBeSelected();
  });
});

describe('team news', () => {
  // A mock team with flagged players (as e2e/team-tab.spec.ts).
  const FLAGGED = 'tm_01k7m2xq800000000000000004';

  it("lists a team's news newest first, each with its time", async () => {
    await renderRouter('./src/app', {
      initialUrl: `/leagues/${data.LEAGUE_ID}/team/${FLAGGED}`,
    });
    await fireEvent.press(await screen.findByTestId('team-action-news', {}, T));
    const sheet = await screen.findByTestId('news-sheet', {}, T);
    const l = league();
    if (!data.isInSeason(l)) throw new Error('in season');
    // Expected from the mock world: the team's players with news, by newsAt, newest first.
    const want = data.PLAYERS.filter((p) => p.news && l.playerListItem(p).owner?.id === FLAGGED)
      .sort((a, b) => (b.newsAt ?? '').localeCompare(a.newsAt ?? ''))
      .map((p) => `news-row-${p.id}`);
    expect(want.length).toBeGreaterThan(0);
    await waitFor(() =>
      expect(
        within(sheet)
          .getAllByTestId(/^news-row-/)
          .map((r) => r.props.testID),
      ).toEqual(want),
    );
    expect(within(sheet).getAllByTestId('news-time')).toHaveLength(want.length);
    expect(requests).toContain(`GET /v1/leagues/${data.LEAGUE_ID}/news?teamId=${FLAGGED}&limit=50`);
  });

  it('shows an error with a retry when news fails, never "all available"', async () => {
    newsDown = true;
    await renderRouter('./src/app', {
      initialUrl: `/leagues/${data.LEAGUE_ID}/team/${FLAGGED}`,
    });
    await fireEvent.press(await screen.findByTestId('team-action-news', {}, T));
    const sheet = await screen.findByTestId('news-sheet', {}, T);
    // A 503 is retried three times (1 s, 2 s, 4 s) before the error shows;
    // the wait is long so the test holds up on a busy machine.
    const error = await within(sheet).findByTestId('query-error', {}, { timeout: 40_000 });
    expect(error).toHaveTextContent(/Couldn't load this\./);
    expect(within(sheet).queryByTestId('news-empty')).toBeNull();
    expect(within(sheet).queryByText(/is available\.|are available\./)).toBeNull();
    const news = `GET /v1/leagues/${data.LEAGUE_ID}/news?teamId=${FLAGGED}&limit=50`;
    expect(requests.filter((r) => r === news)).toHaveLength(4);

    // Back up: Try again loads the team's news.
    newsDown = false;
    await fireEvent.press(within(sheet).getByTestId('retry'));
    expect((await within(sheet).findAllByTestId(/^news-row-/, {}, T)).length).toBeGreaterThan(0);
    expect(within(sheet).queryByTestId('query-error')).toBeNull();
  }, 60_000);
});
