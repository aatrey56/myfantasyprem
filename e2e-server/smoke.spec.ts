import { expect, test, type Page } from '@playwright/test';

// The demo league on the real server (server/internal/demo), signed in as
// the demo commissioner with a session from `cmd/admin session`
// (scripts/e2e-server.mjs passes it in E2E_SESSION).
const SESSION = process.env.E2E_SESSION ?? '';
const API = process.env.E2E_API ?? 'http://localhost:8091';
const LEAGUE_NAME = 'The Sunday Draft';
// Drafted mid-season, after gameweek 7's deadline (server/internal/demo/midseason).
const LATE_LEAGUE_NAME = 'Late Kickoff';

test.skip(!SESSION, 'run through npm run test:e2e:server');

async function signIn(page: Page) {
  await page.addInitScript((session) => localStorage.setItem('mfp.session', session), SESSION);
}

async function leagueId(page: Page, name = LEAGUE_NAME): Promise<string> {
  const { accessToken } = JSON.parse(SESSION) as { accessToken: string };
  const res = await page.request.get(`${API}/v1/me/leagues`, {
    headers: { Authorization: `Bearer ${accessToken}` },
  });
  expect(res.ok()).toBeTruthy();
  const body = (await res.json()) as { data: { league: { id: string; name: string } }[] };
  const league = body.data.find((l) => l.league.name === name);
  expect(league).toBeTruthy();
  return league!.league.id;
}

test.beforeEach(async ({ page }) => signIn(page));

test('the leagues hub lists the demo league', async ({ page }) => {
  await page.goto('/leagues');
  await expect(page.getByText(LEAGUE_NAME).first()).toBeVisible();
});

test('a league page loads from the server and opens the realtime channel', async ({ page }) => {
  const id = await leagueId(page);
  const socket = page.waitForEvent('websocket', (ws) => ws.url().includes('/v1/realtime?ticket='));
  await page.goto(`/leagues/${id}/league`);
  // The League tab has no page title: the name is in the bar above the tabs.
  await expect(page.locator('[data-testid="league-switcher-name"]:visible')).toHaveText(
    LEAGUE_NAME,
  );
  await expect(page.getByTestId('league-matchups-header')).toBeVisible();
  await expect(page.getByTestId('loaded')).toBeVisible();
  const ws = await socket;
  expect(ws.isClosed()).toBe(false);
});

for (const path of ['/team', '/players', '/activity', '/matchup', '/draft']) {
  test(`${path} loads without an error`, async ({ page }) => {
    const id = await leagueId(page);
    const failures: string[] = [];
    page.on('response', (r) => {
      if (r.url().startsWith(API) && r.status() >= 500) failures.push(`${r.status()} ${r.url()}`);
    });
    await page.goto(`/leagues/${id}${path}`);
    await expect(page.getByText('Something went wrong')).toHaveCount(0);
    await page.waitForLoadState('networkidle');
    expect(failures).toEqual([]);
  });
}

test('the Status tab renders from the server: status, awards and a player card', async ({
  page,
}) => {
  const id = await leagueId(page);
  const failures: string[] = [];
  page.on('response', (r) => {
    if (r.url().startsWith(API) && r.status() >= 500) failures.push(`${r.status()} ${r.url()}`);
  });
  const ok = (path: RegExp) =>
    page.waitForResponse((r) => r.url().startsWith(API) && path.test(new URL(r.url()).pathname));
  const status = ok(new RegExp(`^/v1/leagues/${id}/status$`));
  const potw = ok(/^\/v1\/awards\/players-of-the-week$/);
  const awards = ok(/^\/v1\/gameweeks\/\d+\/awards$/);
  await page.goto(`/leagues/${id}`);
  for (const res of [await status, await potw, await awards]) {
    expect(res.status(), res.url()).toBe(200);
  }
  await expect(page.getByTestId('status-loaded')).toBeVisible();
  await expect(page.getByText('Something went wrong')).toHaveCount(0);
  for (const section of ['gw-summary', 'top-available', 'potw', 'totw', 'status-injuries']) {
    await expect(page.getByTestId(section)).toBeVisible();
  }
  // The demo world's gameweek 6 Player of the Week, and a flagged player
  // the demo team owns.
  await expect(page.getByTestId('potw')).toContainText('B. Moreau');
  await expect(page.getByTestId('status-injuries')).toContainText('C. Osei');

  // A top available player's card, with per-game stats from the server.
  const detail = ok(new RegExp(`^/v1/leagues/${id}/players/plr_`));
  await page
    .getByTestId('top-available')
    .locator('[data-testid^="top-plr_"]')
    .first()
    .getByRole('button')
    .first()
    .click();
  expect((await detail).status()).toBe(200);
  const card = page.getByTestId('player-card');
  await expect(card).toBeVisible();
  await card.getByTestId('card-tab-log').click();
  await expect(card.getByTestId('card-log')).toBeVisible();
  // Gameweek 6 is final: every group has numbers, not dashes. The heat map
  // shows all the groups at once.
  const gw6 = card.getByTestId('log-row-6');
  await expect(gw6.getByTestId('log-bps')).toHaveText(/^-?\d+$/);
  await expect(gw6.getByTestId('log-ictIndex')).toHaveText(/^\d+\.\d$/);
  await expect(gw6.getByTestId('log-expectedGoals')).toHaveText(/^\d+\.\d\d$/);
  await expect(gw6.getByTestId('log-cbit')).toHaveText(/^\d+$/);
  await expect(gw6.getByTestId('log-recoveries')).toHaveText(/^\d+$/);
  await page.waitForLoadState('networkidle');
  expect(failures).toEqual([]);
});

test('an invite link from the server opens the join screen on this build', async ({ page }) => {
  const { accessToken } = JSON.parse(SESSION) as { accessToken: string };
  const headers = { Authorization: `Bearer ${accessToken}` };
  const name = `Invite smoke ${Date.now()}`;
  const created = await page.request.post(`${API}/v1/leagues`, {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: { name, teamCount: 4, teamName: 'Smoke FC' },
  });
  expect(created.status()).toBe(201);
  const league = ((await created.json()) as { data: { league: { id: string } } }).data.league;
  const res = await page.request.post(`${API}/v1/leagues/${league.id}/invites`, {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: {},
  });
  expect(res.status()).toBe(201);
  const { url } = ((await res.json()) as { data: { url: string } }).data;
  // APP_URL is this build (scripts/e2e-server.mjs).
  expect(url).toMatch(
    new RegExp(`^http://localhost:${process.env.PW_PORT ?? 4174}/join/[0-9A-Z]{10}$`),
  );
  await page.goto(url);
  await expect(page.getByTestId('invite-league-name')).toHaveText(name);
});

test('a league drafted mid-season starts at its first gameweek, with its missed clocks', async ({
  page,
}) => {
  const id = await leagueId(page, LATE_LEAGUE_NAME);
  const failures: string[] = [];
  page.on('response', (r) => {
    if (r.url().startsWith(API) && r.status() >= 400) failures.push(`${r.status()} ${r.url()}`);
  });
  const noEarlyGameweeks = async () => {
    await expect(page.locator('body')).not.toContainText(/\bGW ?[0-7]\b/);
    await expect(page.getByText('Something went wrong')).toHaveCount(0);
  };

  await page.goto(`/leagues/${id}/league`);
  await expect(page.getByTestId('loaded')).toBeVisible();
  const matchups = page.getByTestId('league-matchups-header');
  await expect(matchups.getByTestId('gw-label')).toHaveText('GW 8');
  await expect(matchups.getByTestId('gw-prev')).toBeDisabled();
  await noEarlyGameweeks();

  await page.goto(`/leagues/${id}/matchup`);
  await expect(page.getByTestId('gw-label')).toHaveText('GW 8');
  await expect(page.getByTestId('gw-prev')).toBeDisabled();
  await noEarlyGameweeks();

  await page.goto(`/leagues/${id}/draft`);
  await expect(page.getByText('Draft complete')).toBeVisible();

  await page.goto(`/leagues/${id}/activity`);
  const feed = page.getByTestId('activity-feed');
  await expect(feed).toContainText('Clock ran out');
  await expect(feed).toContainText('Draft completed');
  await page.waitForLoadState('networkidle');
  expect(failures).toEqual([]);
});

test('the watchlist round-trips through the server: star on the card, ★ on the Players tab', async ({
  page,
}) => {
  const id = await leagueId(page);
  const { accessToken } = JSON.parse(SESSION) as { accessToken: string };
  const headers = { Authorization: `Bearer ${accessToken}` };
  await page.goto(`/leagues/${id}/players`);
  const row = page.locator('[data-testid^="player-plr_"]:visible').first();
  await expect(row).toBeVisible();
  const playerId = (await row.getAttribute('data-testid'))!.replace('player-', '');
  await row.click();
  const card = page.locator('[data-testid="player-card"]:visible');
  const watched = page.waitForResponse(
    (r) => r.request().method() === 'PUT' && r.url().endsWith(`/watchlist/${playerId}`),
  );
  await card.getByTestId('card-watch').click();
  expect((await watched).status()).toBe(200);
  const list = await page.request.get(`${API}/v1/leagues/${id}/watchlist`, { headers });
  expect(list.ok()).toBeTruthy();
  const entries = ((await list.json()) as { data: { player: { id: string } }[] }).data;
  expect(entries.map((e) => e.player.id)).toContain(playerId);
  await page.keyboard.press('Escape');
  await page.locator('[data-testid="filter-watchlist"]:visible').click();
  await expect(page.locator(`[data-testid="player-${playerId}"]:visible`)).toBeVisible();
  // Leave the demo user's list as it was.
  const removed = await page.request.delete(`${API}/v1/leagues/${id}/watchlist/${playerId}`, {
    headers,
  });
  expect(removed.status()).toBe(204);
});
