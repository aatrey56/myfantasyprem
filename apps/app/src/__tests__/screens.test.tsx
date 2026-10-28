/**
 * Screens against the stateful MSW handlers (no worker: fetch is routed
 * straight to the handlers): the sign-in flow, the lineup swap with the
 * server's 422 reasons, and the draft queue controls.
 */
import type { Schemas } from '@myfantasyprem/api-client';
import { act, fireEvent, render, screen, within } from '@testing-library/react-native';
import { router } from 'expo-router';
import { renderRouter, waitFor } from 'expo-router/testing-library';
import { getResponse } from 'msw';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { persistOptions } from '@/api/persist';
import { memorySessionStore, tokens, type StoredSession } from '@/auth/session';
import { DraftQueue } from '@/components/draft/DraftQueue';
import * as data from '@/mocks/data';
import { handlers, resetMocks } from '@/mocks/handlers';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));

// The platform store, swapped per test.
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
/** Per-test rewrite of a request before the mock handlers see it. */
let rewrite: ((request: Request) => Request) | null = null;
// When set, the next magic-link verify request fails this way instead.
let failNextVerify: (() => Response) | null = null;
/** Requests whose path matches wait for `held.release()` before they are answered. */
let held: { path: RegExp; release: () => void; gate: Promise<void> } | null = null;
function hold(path: RegExp) {
  let release = () => {};
  const gate = new Promise<void>((resolve) => (release = resolve));
  held = { path, release, gate };
  return held;
}
const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    let request = new Request(input, init);
    requests.push(
      `${request.method} ${new URL(request.url).pathname} ${request.headers.get('authorization') ?? '-'}`,
    );
    if (rewrite) request = rewrite(request);
    if (held?.path.test(new URL(request.url).pathname)) await held.gate;
    if (failNextVerify && new URL(request.url).pathname === '/v1/auth/email/verify') {
      const fail = failNextVerify;
      failNextVerify = null;
      return fail();
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
  // Each test starts from a reset mock world, so also from an empty saved
  // query cache: the root layout restores it, and a previous test's pages
  // would otherwise paint (and stay fresh) in this one. removeClient also
  // drops a write still waiting on the save throttle.
  await persistOptions.persister.removeClient();
  failNextVerify = null;
  resetMocks();
  rewrite = null;
  held = null;
  requests.length = 0;
  tokens.set(null);
});

describe('sign-in flow', () => {
  it('emails a sign-in link', async () => {
    mockStore = memorySessionStore();
    await renderRouter('./src/app', { initialUrl: '/' });
    await fireEvent.changeText(await screen.findByTestId('sign-in-email'), 'sam@example.com');
    await fireEvent.press(screen.getByTestId('sign-in-email-submit'));
    expect(await screen.findByTestId('email-sent')).toHaveTextContent(/sam@example\.com/);
    expect(await mockStore.load()).toBeNull();
  });

  it('opening the emailed link asks first, then signs in, stores the session and opens the hub', async () => {
    mockStore = memorySessionStore();
    // renderRouter's result is a promise that also carries the router getters.
    const view = renderRouter('./src/app', { initialUrl: '/sign-in?token=a-magic-link-token-123' });
    await view;
    // A link opened by a mail scanner or someone else's click does not
    // sign in on its own, and the token leaves the URL at once.
    expect(await screen.findByTestId('magic-link-confirm')).toBeOnTheScreen();
    await waitFor(() => expect(view.getSearchParams()).toEqual({}));
    expect(requests.some((r) => r.startsWith('POST /v1/auth/email/verify'))).toBe(false);
    expect(await mockStore.load()).toBeNull();
    await fireEvent.press(screen.getByTestId('magic-link-continue'));
    expect(await screen.findByText('The Sunday Draft', {}, { timeout: 5000 })).toBeOnTheScreen();
    expect((await mockStore.load())?.accessToken).toBe('mock-access-token');
    // Signed-in requests carry the bearer token.
    await waitFor(() => expect(requests).toContain('GET /v1/me/leagues Bearer mock-access-token'));
  });

  it('signs in with the emailed code and returns to the invite it started from', async () => {
    mockStore = memorySessionStore();
    const view = renderRouter('./src/app', { initialUrl: '/sign-in?next=/join/DERBY7XQ' });
    await view;
    await fireEvent.changeText(await screen.findByTestId('sign-in-email'), 'sam@example.com');
    await fireEvent.press(screen.getByTestId('sign-in-email-submit'));
    expect(await screen.findByTestId('email-sent')).toHaveTextContent(/6-digit code/);
    // A wrong code says so and stays signed out.
    await fireEvent.changeText(screen.getByLabelText('Sign-in code'), '000000');
    expect(await screen.findByTestId('sign-in-code-error')).toHaveTextContent(
      /wrong or has expired/,
    );
    expect(await mockStore.load()).toBeNull();
    // A pasted code signs in at once.
    await fireEvent.changeText(screen.getByLabelText('Sign-in code'), '420917');
    await waitFor(() => expect(view.getPathname()).toBe('/join/DERBY7XQ'));
    expect((await mockStore.load())?.accessToken).toBe('mock-access-token');
    expect(requests.some((r) => r.startsWith('POST /v1/auth/email/code'))).toBe(true);
  });

  it('shows the reason when a magic link has expired', async () => {
    mockStore = memorySessionStore();
    await renderRouter('./src/app', { initialUrl: '/sign-in?token=expired-token-0000' });
    await fireEvent.press(await screen.findByTestId('magic-link-continue'));
    expect(await screen.findByTestId('sign-in-error')).toHaveTextContent(/expired/);
    // A used or expired link cannot work again: it is dropped.
    expect(screen.queryByTestId('magic-link-confirm')).toBeNull();
    expect(await mockStore.load()).toBeNull();
  });

  it.each([
    [
      'the network drops',
      () => {
        throw new TypeError('Network request failed');
      },
    ],
    [
      'the server is rate limiting',
      () =>
        new Response(
          JSON.stringify({ type: 'about:blank', title: 'Too many requests', status: 429 }),
          {
            status: 429,
            headers: { 'Content-Type': 'application/problem+json', 'Retry-After': '1' },
          },
        ),
    ],
    [
      'the server fails',
      () =>
        new Response(JSON.stringify({ type: 'about:blank', title: 'Unavailable', status: 503 }), {
          status: 503,
          headers: { 'Content-Type': 'application/problem+json' },
        }),
    ],
  ])('keeps the magic link to try again when %s', async (_, fail) => {
    mockStore = memorySessionStore();
    failNextVerify = fail;
    await renderRouter('./src/app', { initialUrl: '/sign-in?token=a-magic-link-token-123' });
    await fireEvent.press(await screen.findByTestId('magic-link-continue'));
    expect(await screen.findByTestId('sign-in-error')).toBeOnTheScreen();
    expect(screen.getByTestId('magic-link-confirm')).toBeOnTheScreen();
    await fireEvent.press(screen.getByTestId('magic-link-continue'));
    expect(await screen.findByText('The Sunday Draft', {}, { timeout: 5000 })).toBeOnTheScreen();
    expect((await mockStore.load())?.accessToken).toBe('mock-access-token');
  });
});

describe('lineup swaps', () => {
  it('refuses an illegal swap with the rule it breaks, and keeps the lineup', async () => {
    mockStore = memorySessionStore(SESSION);
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.LEAGUE_ID}/team` });
    // The pitch is the default view.
    const xi = await screen.findByTestId('pitch-xi', {}, { timeout: 5000 });
    const players = /^lineup-player-/;
    // The XI runs forwards (top) to goalkeeper (bottom).
    const lines = within(xi).getAllByTestId(/^pitch-line-/);
    expect(lines.map((l) => l.props.testID)).toEqual([
      'pitch-line-FWD',
      'pitch-line-MID',
      'pitch-line-DEF',
      'pitch-line-GKP',
    ]);
    const keeper = within(screen.getByTestId('pitch-line-GKP')).getAllByTestId(players)[0]!;
    const keeperId = keeper.props.testID as string;
    const benchPlayers = within(screen.getByTestId('bench-row')).getAllByTestId(players);
    await fireEvent.press(keeper);
    expect(keeper).toBeSelected();
    await fireEvent.press(benchPlayers[1]!);
    const errors = await screen.findByTestId('lineup-errors');
    expect(errors).toHaveTextContent(/That lineup isn.t allowed/);
    expect(errors).toHaveTextContent(/Start at least 1 goalkeepers?\./);
    // The keeper is still in goal.
    const after = within(screen.getByTestId('pitch-line-GKP')).getAllByTestId(players);
    expect(after[0]!.props.testID).toBe(keeperId);
  });

  it('swaps from the list through the swap sheet, which offers only legal swaps', async () => {
    mockStore = memorySessionStore(SESSION);
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.LEAGUE_ID}/team` });
    // The pitch is the default; the list is one tap away.
    await fireEvent.press(await screen.findByTestId('view-list', {}, { timeout: 5000 }));
    const starters = await screen.findByTestId('list-starters', {}, { timeout: 5000 });
    expect(screen.getByTestId('swap-hint')).toHaveTextContent(
      'Tap a position to change your lineup',
    );
    const pill = /^lineup-swap-/;
    // Starters run goalkeeper first; the backup keeper is first on the bench.
    const keeperId = within(starters)
      .getAllByTestId(pill)[0]!
      .props.testID.replace('lineup-swap-', '');
    const backupId = within(screen.getByTestId('list-bench'))
      .getAllByTestId(pill)[0]!
      .props.testID.replace('lineup-swap-', '');
    const saves = () => requests.filter((r) => /^PUT .*\/lineup /.test(r)).length;
    const before = saves();

    // Cancel: the sheet closes and nothing is saved.
    await fireEvent.press(screen.getByTestId(`lineup-swap-${keeperId}`));
    const sheet = await screen.findByTestId('swap-sheet');
    expect(within(sheet).getByTestId('swap-selected')).toHaveTextContent(/GK/);
    expect(within(sheet).getByTestId('swap-prompt')).toHaveTextContent('⇅ Choose a player to swap');
    // A keeper can only swap with the other keeper.
    const offered = within(sheet).getAllByTestId(/^swap-candidate-/);
    expect(offered.map((c) => c.props.testID)).toEqual([`swap-candidate-${backupId}`]);
    await fireEvent.press(within(sheet).getByTestId('sheet-close'));
    await waitFor(() => expect(screen.queryByTestId('swap-sheet')).toBeNull());
    expect(saves()).toBe(before);
    expect(within(screen.getByTestId('list-starters')).getAllByTestId(pill)[0]!.props.testID).toBe(
      `lineup-swap-${keeperId}`,
    );

    // Pick the candidate: the same swap a second tap on the pitch makes.
    await fireEvent.press(screen.getByTestId(`lineup-swap-${keeperId}`));
    await fireEvent.press(
      within(await screen.findByTestId('swap-sheet')).getByLabelText(/^Swap with /),
    );
    await waitFor(() => expect(saves()).toBe(before + 1));
    await waitFor(() =>
      expect(
        within(screen.getByTestId('list-starters')).getAllByTestId(pill)[0]!.props.testID,
      ).toBe(`lineup-swap-${backupId}`),
    );
    expect(screen.queryByTestId('swap-sheet')).toBeNull();
  });
});

describe('upcoming matchups', () => {
  it('shows the pairing that was opened, not my own', async () => {
    mockStore = memorySessionStore(SESSION);
    const league = data.leagues.get(data.LEAGUE_ID) as Extract<
      data.MockLeague,
      { schedule: unknown }
    >;
    const gw = data.CURRENT_GW + 1;
    const myTeamId = league.myTeam.id;
    const other = league
      .schedule()
      .find((w) => w.gameweek === gw)!
      .matchups.find(
        (m) =>
          m.home.kind === 'team' &&
          m.away.kind === 'team' &&
          m.home.team.id !== myTeamId &&
          m.away.team.id !== myTeamId,
      )!;
    const name = (side: (typeof other)['home']) => (side.kind === 'team' ? side.team.name : '');
    await renderRouter('./src/app', {
      initialUrl: `/leagues/${data.LEAGUE_ID}/matchup/${gw}/${other.id}`,
    });
    const preview = await screen.findByTestId('matchup-preview', {}, { timeout: 5000 });
    // The preview is of the opened pairing: both its teams, not mine.
    expect(requests.some((r) => r.includes(`/matchups/${other.id}/preview`))).toBe(true);
    expect(requests.some((r) => r.includes('/preview') && !r.includes(other.id))).toBe(false);
    expect(preview).toHaveTextContent(name(other.home), { exact: false });
    expect(preview).toHaveTextContent(name(other.away), { exact: false });
    expect(preview).not.toHaveTextContent(league.myTeam.name, { exact: false });
  });

  it("a switch to a preview still loading shows the skeleton, not the last pairing's squads", async () => {
    mockStore = memorySessionStore(SESSION);
    const league = data.leagues.get(data.LEAGUE_ID) as Extract<
      data.MockLeague,
      { schedule: unknown }
    >;
    const gw = data.CURRENT_GW + 1;
    const pairings = league
      .schedule()
      .find((w) => w.gameweek === gw)!
      .matchups.filter((m) => m.home.kind === 'team' && m.away.kind === 'team');
    const [first, second] = pairings;
    const name = (side: (typeof first)['home']) => (side.kind === 'team' ? side.team.name : '');
    await renderRouter('./src/app', {
      initialUrl: `/leagues/${data.LEAGUE_ID}/matchup/${gw}/${first.id}`,
    });
    const before = await screen.findByTestId('matchup-preview', {}, { timeout: 5000 });
    expect(before).toHaveTextContent(name(first.home), { exact: false });
    const gate = hold(new RegExp(`/matchups/${second.id}/preview$`));
    await fireEvent.press(screen.getByTestId(`matchup-row-${second.id}`));
    await waitFor(() => expect(screen.queryByTestId('matchup-preview')).toBeNull());
    await act(async () => gate.release());
    const after = await screen.findByTestId('matchup-preview', {}, { timeout: 5000 });
    expect(after).toHaveTextContent(name(second.home), { exact: false });
    expect(after).toHaveTextContent(name(second.away), { exact: false });
  });
});

describe('Match tab carousel', () => {
  it("a jump to a matchup still loading shows the skeleton, not the last matchup's lineups", async () => {
    mockStore = memorySessionStore(SESSION);
    const league = data.leagues.get(data.LEAGUE_ID) as Extract<
      data.MockLeague,
      { myTeam: unknown }
    >;
    const myTeamId = league.myTeam!.id;
    const summaries = data.matchupSummaries(data.CURRENT_GW);
    const mine = summaries.find(
      (m) =>
        (m.home.kind === 'team' && m.home.team.id === myTeamId) ||
        (m.away.kind === 'team' && m.away.team.id === myTeamId),
    )!;
    // Page 0 is mine; page 3 is not a neighbour of 0, so it is not prefetched.
    const target = summaries.filter((m) => m.id !== mine.id)[2]!;
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.LEAGUE_ID}/matchup` });
    expect(await screen.findByTestId('matchup-detail', {}, { timeout: 5000 })).toBeOnTheScreen();
    const gate = hold(new RegExp(`/matchups/${target.id}$`));
    await fireEvent.press(screen.getByTestId('carousel-dot-3'));
    await waitFor(() => expect(screen.queryByTestId('matchup-detail')).toBeNull());
    expect(screen.getAllByTestId('loading').length).toBeGreaterThan(0);
    await act(async () => gate.release());
    expect(await screen.findByTestId('matchup-detail', {}, { timeout: 5000 })).toBeOnTheScreen();
  });
});

describe('a league drafted mid-season', () => {
  // Late Kickoff drafted after gameweek 7's deadline: its first gameweek is
  // 8 and has not started.
  const first = data.CURRENT_GW + 1;
  const late = () =>
    data.leagues.get(data.LATE_LEAGUE_ID) as Extract<data.MockLeague, { schedule: unknown }>;

  it('opens the Matchup tab on its first gameweek, as a pairing preview', async () => {
    mockStore = memorySessionStore(SESSION);
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.LATE_LEAGUE_ID}/matchup` });
    expect(await screen.findByTestId('matchup-preview', {}, { timeout: 5000 })).toBeOnTheScreen();
    expect(screen.getByTestId('gw-label')).toHaveTextContent(`GW ${first}`);
    expect(requests.some((r) => /\/gameweeks\/([1-7])\/matchups\//.test(r))).toBe(false);
    expect(screen.getByTestId('gw-prev')).toBeDisabled();
    expect(screen.queryByText('Something went wrong')).toBeNull();
    // Another team's side has no locked lineup yet (none before the first
    // gameweek; the first one's is private until its deadline).
    const week = late()
      .schedule()
      .find((w) => w.gameweek === first)!;
    for (const m of week.matchups) {
      const res = await fetch(
        `http://api.test/v1/leagues/${data.LATE_LEAGUE_ID}/gameweeks/${first}/matchups/${m.id}/preview`,
        { headers: { authorization: 'Bearer mock-access-token' } },
      );
      const body = (await res.json()) as { data: Schemas['MatchupPreview'] };
      for (const side of [body.data.home, body.data.away]) {
        if (side.kind !== 'team' || side.team?.id === late().myTeam.id) continue;
        expect(side.starters).toHaveLength(11);
        expect(side.lineupGameweek).toBeUndefined();
      }
    }
  });

  it("shows another team's lineup from the league's first gameweek, never before", async () => {
    mockStore = memorySessionStore(SESSION);
    const other = late().TEAMS.find((t) => t.id !== late().myTeam.id)!;
    // Open the league first: its Status tab loads the season's gameweek
    // (7), which the team page then has in its cache.
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.LATE_LEAGUE_ID}` });
    await screen.findByTestId('status-loaded', {}, { timeout: 5000 });
    await act(() => router.push(`/leagues/${data.LATE_LEAGUE_ID}/team/${other.id}`));
    const label = await screen.findByTestId('gw-label', {}, { timeout: 5000 });
    await waitFor(() => expect(label).toHaveTextContent(`GW ${first}`));
    expect(screen.getByTestId('gw-prev')).toBeDisabled();
    expect(requests.some((r) => /\/lineup\?gameweek=([1-7])\b/.test(r))).toBe(false);
  });
});

describe('draft room start', () => {
  it('enables Start with at least 2 managers and explains the open seats', async () => {
    mockStore = memorySessionStore(SESSION);
    // Five-a-side Friends: 5 of 8 managers, Sam is commissioner.
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.PREDRAFT_LEAGUE_ID}/draft` });
    const start = await screen.findByTestId('draft-start', {}, { timeout: 5000 });
    expect(start).not.toBeDisabled();
    expect(screen.getByTestId('draft-start-reason')).toHaveTextContent(
      '5 of 8 teams have a manager. The 3 open seats are autopicked; anyone who joins later takes one over.',
    );
    await fireEvent.press(start);
    await waitFor(() =>
      expect(requests.some((r) => r.startsWith('POST') && r.includes('/draft/start'))).toBe(true),
    );
  });
});

describe('draft room after the last pick', () => {
  it('stops asking for the queue once the draft completes', async () => {
    mockStore = memorySessionStore(SESSION);
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.DRAFT_LEAGUE_ID}/draft` });
    await screen.findByTestId('draft-clock', {}, { timeout: 5000 });
    await waitFor(() => expect(requests.some((r) => r.includes('/draft/queue'))).toBe(true));
    const mark = requests.length;
    // Every remaining pick is made (as a day later); the next poll of the
    // board brings the last one and the completed draft.
    const league = data.leagues.get(data.DRAFT_LEAGUE_ID) as Extract<
      data.MockLeague,
      { advance: unknown }
    >;
    for (let h = 1; h <= 200 && league.status === 'drafting'; h++) {
      league.advance(Date.now() + h * data.HOUR);
    }
    expect(league.status).toBe('in_season');
    await screen.findByText('Draft complete', {}, { timeout: 8000 });
    // The room's effects have run: a refetch would already be on record.
    const asked = requests.slice(mark).filter((r) => r.startsWith('GET /v1/leagues/'));
    expect(asked.filter((r) => r.includes('/draft/queue'))).toEqual([]);
    expect(screen.queryByText('Something went wrong')).toBeNull();
  });
});

describe('missed pick clocks', () => {
  const derby = () =>
    data.leagues.get(data.DRAFT_LEAGUE_ID) as Extract<data.MockLeague, { advance: unknown }>;
  const lastMissed = () =>
    derby()
      .draft()
      .picks.filter((p) => p.missedClock)
      .at(-1)!;

  it('says in the draft room who was autopicked, and with which player', async () => {
    mockStore = memorySessionStore(SESSION);
    const missed = lastMissed();
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.DRAFT_LEAGUE_ID}/draft` });
    const banner = await screen.findByTestId('missed-clock-banner', {}, { timeout: 5000 });
    expect(banner).toHaveTextContent(
      `${missed.team.name}'s clock ran out: autopicked ${missed.player.displayName}`,
      { exact: false },
    );
    await fireEvent.press(within(banner).getByLabelText('Dismiss'));
    expect(screen.queryByTestId('missed-clock-banner')).toBeNull();
  });

  it('turns "autopick when my clock runs out" off from the queue', async () => {
    mockStore = memorySessionStore(SESSION);
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.DRAFT_LEAGUE_ID}/draft` });
    await fireEvent.press(await screen.findByTestId('pane-queue', {}, { timeout: 5000 }));
    const toggle = await screen.findByTestId('autopick-timeout-switch');
    expect(toggle.props.value).toBe(true);
    await fireEvent(toggle, 'valueChange', false);
    await waitFor(() => expect(derby().queueView().autopickOnTimeout).toBe(false));
  });

  it('lists a missed-clock autopick in the activity feed while drafting', async () => {
    mockStore = memorySessionStore(SESSION);
    const missed = lastMissed();
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.DRAFT_LEAGUE_ID}/activity` });
    // On native the feed is the screen's list: it is there before its rows.
    const feed = await screen.findByTestId('activity-feed', {}, { timeout: 5000 });
    await waitFor(() =>
      expect(feed).toHaveTextContent(missed.player.displayName, { exact: false }),
    );
    expect(feed).toHaveTextContent('Clock ran out', { exact: false });
  });
});

describe('League tab', () => {
  it('opens on the matchup cards with a GW stepper, then standings and quiet links', async () => {
    mockStore = memorySessionStore(SESSION);
    const league = data.leagues.get(data.LEAGUE_ID) as Extract<
      data.MockLeague,
      { myTeam: unknown }
    >;
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.LEAGUE_ID}/league` });
    const header = await screen.findByTestId('league-matchups-header', {}, { timeout: 5000 });
    expect(within(header).getByTestId('gw-label')).toHaveTextContent(`GW ${data.CURRENT_GW}`);
    const now = data.matchupSummaries(data.CURRENT_GW);
    const cards = within(screen.getByTestId('league-matchups')).getAllByTestId(/^matchup-row-mu_/);
    expect(cards).toHaveLength(now.length);
    // Mine first.
    const mine = now.find((m) =>
      [m.home, m.away].some((s) => s.kind === 'team' && s.team.id === league.myTeam!.id),
    )!;
    expect(cards[0]!.props.testID).toBe(`matchup-row-${mine.id}`);
    expect(screen.getByTestId('standings-header')).toBeOnTheScreen();
    // No page title and no button row; Activity and the draft results stay reachable.
    expect(screen.queryByTestId('league-name')).toBeNull();
    for (const id of ['link-trades', 'link-waivers', 'link-deadlines', 'section-schedule']) {
      expect(screen.queryByTestId(id)).toBeNull();
    }
    expect(screen.getByTestId('link-activity')).toBeOnTheScreen();
    expect(screen.getByTestId('link-draft-results')).toBeOnTheScreen();
    // The stepper: last gameweek's results, then next gameweek's pairings.
    await fireEvent.press(within(header).getByTestId('gw-prev'));
    const before = data.matchupSummaries(data.CURRENT_GW - 1);
    expect(
      await screen.findByTestId(`matchup-row-${before[0]!.id}`, {}, { timeout: 5000 }),
    ).toBeOnTheScreen();
    expect(within(header).getByTestId('gw-label')).toHaveTextContent(`GW ${data.CURRENT_GW - 1}`);
    await fireEvent.press(within(header).getByTestId('gw-next'));
    await fireEvent.press(within(header).getByTestId('gw-next'));
    const next = (league as unknown as { schedule: () => Schemas['ScheduleWeek'][] })
      .schedule()
      .find((w) => w.gameweek === data.CURRENT_GW + 1)!;
    expect(
      await screen.findByTestId(`matchup-row-${next.matchups[0]!.id}`, {}, { timeout: 5000 }),
    ).toBeOnTheScreen();
  });
});

describe('league home standings', () => {
  it('names the last gameweek played, counting from the league start', async () => {
    mockStore = memorySessionStore(SESSION);
    const league = data.leagues.get(data.LEAGUE_ID) as Extract<
      data.MockLeague,
      { settings: unknown }
    >;
    // A league that joined the season at GW 3.
    league.settings.startGameweek = 3;
    const played = data.standings()[0]!.played;
    expect(played).toBeGreaterThan(0);
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.LEAGUE_ID}/league` });
    expect(
      await screen.findByText(`after GW ${3 + played - 1}`, {}, { timeout: 5000 }),
    ).toBeOnTheScreen();
  });
});

describe('paged lists', () => {
  /**
   * Offset cursors: something added at the top between two page loads
   * shifts every item down, so the next page repeats the last one seen.
   */
  const shiftNextPage = (pathEnd: string, by: number) => (request: Request) => {
    const url = new URL(request.url);
    const cursor = url.searchParams.get('cursor');
    if (!url.pathname.endsWith(pathEnd) || !cursor) return request;
    url.searchParams.set('cursor', btoa(String(Number(atob(cursor)) - by)));
    return new Request(url, request);
  };

  it('lists a player once when the next page overlaps', async () => {
    mockStore = memorySessionStore(SESSION);
    rewrite = shiftNextPage('/players', 1);
    await renderRouter('./src/app', { initialUrl: `/leagues/${data.LEAGUE_ID}/players` });
    const rows = () => screen.getAllByTestId(/^player-plr_/);
    const pages = () => requests.filter((r) => /\/players /.test(r)).length;
    // On native the list (FlashList) loads the next pages as its end comes
    // into view; Jest's window shows every row, so they all load.
    await waitFor(() => expect(pages()).toBeGreaterThan(1), { timeout: 5000 });
    await waitFor(() => expect(screen.queryByTestId('players-more')).toBeNull());
    const ids = rows().map((r) => r.props.testID as string);
    expect(ids.length).toBeGreaterThan(0);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it('lists a trade once when the next page overlaps', async () => {
    mockStore = memorySessionStore(SESSION);
    // Pages of two, the second shifted back by one.
    const shift = shiftNextPage('/trades', 1);
    rewrite = (request) => {
      const url = new URL(request.url);
      if (!url.pathname.endsWith('/trades') || request.method !== 'GET') return request;
      url.searchParams.set('limit', '2');
      return shift(new Request(url, request));
    };
    await renderRouter('./src/app', {
      initialUrl: `/leagues/${data.LEAGUE_ID}/players/trades`,
    });
    await fireEvent.press(await screen.findByText('Show older trades', {}, { timeout: 5000 }));
    await waitFor(() => expect(requests.filter((r) => r.includes('/trades ')).length).toBe(2));
    const seen: string[] = [];
    for (const bucket of ['active', 'history']) {
      await fireEvent.press(screen.getByTestId(`bucket-${bucket}`));
      seen.push(...screen.queryAllByTestId(/^trade-trd_/).map((c) => c.props.testID as string));
    }
    expect(seen.length).toBe(3); // two pages of two, one repeated
    expect(new Set(seen).size).toBe(seen.length);
  });
});

describe('draft queue', () => {
  const item = (n: number, position: Schemas['Position'] = 'MID'): Schemas['DraftQueueItem'] => ({
    player: {
      id: `plr_${String(n).padStart(26, '0')}`,
      displayName: `Player ${n}`,
      club: { code: 'NOR', name: 'Northgate' },
      position,
      availability: 'available',
    },
    draftRank: n,
  });

  async function renderQueue(overrides: Partial<Parameters<typeof DraftQueue>[0]> = {}) {
    const props = {
      queue: [item(1), item(2, 'GKP'), item(3)],
      autopick: false,
      autopickOnTimeout: true,
      autopickPending: false,
      onAutopick: jest.fn(),
      onAutopickOnTimeout: jest.fn(),
      onMove: jest.fn(),
      onRemove: jest.fn(),
      myCounts: { GKP: 2, DEF: 0, MID: 0, FWD: 0 },
      canPick: true,
      pickingId: null,
      onPick: jest.fn(),
      ...overrides,
    };
    await render(
      <SafeAreaProvider
        initialMetrics={{
          frame: { x: 0, y: 0, width: 375, height: 812 },
          insets: { top: 0, left: 0, right: 0, bottom: 0 },
        }}
      >
        <DraftQueue {...props} />
      </SafeAreaProvider>,
    );
    return props;
  }

  it('reorders, removes and drafts the top player', async () => {
    const props = await renderQueue();
    const id2 = item(2).player.id;
    expect(screen.getByLabelText('Move Player 1 up')).toBeDisabled();
    expect(screen.getByLabelText('Move Player 3 down')).toBeDisabled();
    await fireEvent.press(screen.getByLabelText('Move Player 2 up'));
    expect(props.onMove).toHaveBeenCalledWith(id2, -1);
    await fireEvent.press(screen.getByLabelText('Remove Player 3 from queue'));
    expect(props.onRemove).toHaveBeenCalledWith(item(3).player.id);
    // Only the top of the queue gets a Draft button while on the clock.
    expect(screen.getAllByLabelText(/^Draft /)).toHaveLength(1);
    await fireEvent.press(screen.getByLabelText('Draft Player 1'));
    expect(props.onPick).toHaveBeenCalledWith(item(1).player);
  });

  it('marks a queued player whose position is full', async () => {
    await renderQueue();
    expect(screen.getByTestId(`queue-item-${item(2).player.id}`)).toHaveTextContent(
      /position full/,
    );
  });

  it('toggles autopick and shows an empty queue', async () => {
    const props = await renderQueue({ queue: [] });
    expect(screen.getByTestId('queue-empty')).toBeOnTheScreen();
    await fireEvent(screen.getByTestId('autopick-switch'), 'valueChange', true);
    expect(props.onAutopick).toHaveBeenCalledWith(true);
  });
});
