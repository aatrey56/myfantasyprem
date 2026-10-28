import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

// The FPL Draft import end to end (docs/adr/0012): the real API, its River
// jobs and poller on a fake FPL Draft API that serves the recorded,
// anonymised league (scripts/fpl-fixture-server.mjs). scripts/e2e-server.mjs
// passes two fresh accounts' sessions. FPL itself is never called.
const IMPORTER = process.env.E2E_FPL_SESSION ?? '';
const FRIEND = process.env.E2E_FPL_FRIEND ?? '';
const API = process.env.E2E_API ?? 'http://localhost:8091';
const FPL_LEAGUE = '90001';
const LEAGUE_NAME = 'Fixture Draft League';

test.skip(!IMPORTER, 'run through npm run test:e2e:server');
test.describe.configure({ mode: 'serial' });

const token = (session: string) => (JSON.parse(session) as { accessToken: string }).accessToken;

async function signIn(page: Page, session: string) {
  await page.addInitScript((s) => localStorage.setItem('mfp.session', s), session);
}

async function get<T>(request: APIRequestContext, session: string, path: string): Promise<T> {
  const res = await request.get(`${API}${path}`, {
    headers: { Authorization: `Bearer ${token(session)}` },
  });
  expect(res.status(), path).toBe(200);
  return ((await res.json()) as { data: T }).data;
}

let leagueId = '';

test('import the league: preview, pick my team, import, land on the league', async ({ page }) => {
  // About 80 requests to the fake FPL at FPL's polite 2 a second.
  test.setTimeout(150_000);
  await signIn(page, IMPORTER);
  await page.goto('/import');
  await page
    .getByTestId('league-input')
    .fill(`https://draft.premierleague.com/league/${FPL_LEAGUE}`);
  await page.getByTestId('find-button').click();
  await expect(page.getByTestId('preview-name')).toHaveText(LEAGUE_NAME);
  await page.getByTestId('seat-option-1').click();
  await page.getByTestId('import-button').click();
  await page.waitForURL(/\/leagues\/lg_[0-9a-z]{26}/, { timeout: 120_000 });
  leagueId = /\/leagues\/(lg_[0-9a-z]{26})/.exec(page.url())![1]!;
  await expect(page.getByTestId('sync-status')).toBeVisible();
  await expect(page.getByText('Something went wrong')).toHaveCount(0);
});

test('the import verifies with zero diffs against FPL', async ({ request }) => {
  type Sync = {
    lastSyncedAt: string | null;
    pending: unknown;
    verification: {
      status: string;
      counts: { ok: number; mismatch: number; unverified: number; pending: number };
      failing: { kind: string; gameweek: number | null; status: string }[];
      pending: { kind: string; status: string }[];
    } | null;
  };
  const sync = await get<Sync>(request, IMPORTER, `/v1/leagues/${leagueId}/sync`);
  expect(sync.lastSyncedAt).not.toBeNull();
  expect(sync.pending).toBeNull();
  const v = sync.verification!;
  // Points, auto-subs and results of all six recorded gameweeks, both
  // tables, squads, ownership and the waiver order: everything agrees, and
  // no trade is pending on FPL.
  expect(v.counts).toEqual({ ok: 278, mismatch: 0, unverified: 0, pending: 0 });
  expect(v.failing).toEqual([]);
  expect(v.pending).toEqual([]);
  expect(v.status).toBe('clean');
});

test('the commissioner sees the FPL status, the check, the claim links and re-sync', async ({
  page,
}) => {
  await signIn(page, IMPORTER);
  await page.goto(`/leagues/${leagueId}/settings`);
  await expect(page.getByTestId('sync-panel')).toBeVisible();
  await expect(page.getByTestId('claim-links')).toBeVisible();
  await expect(page.getByTestId('verification-status')).toHaveText('Matches FPL');
  await expect(page.getByTestId('resync')).toBeEnabled();
  await expect(page.getByTestId('check-sync')).toBeEnabled();
});

test('the imported league loads in the app', async ({ page }) => {
  await signIn(page, IMPORTER);
  await page.goto(`/leagues/${leagueId}`);
  await expect(page.getByTestId('status-loaded')).toBeVisible();
  await expect(page.locator('[data-testid="league-switcher-name"]:visible')).toHaveText(
    LEAGUE_NAME,
  );
  await expect(page.locator('[data-testid="sync-status"]:visible')).toHaveText(
    /^Imported from FPL · last synced /,
  );
  await expect(page.locator('[data-testid="sync-verification"]:visible')).toHaveText('Matches FPL');
  await page.goto(`/leagues/${leagueId}/league`);
  await expect(
    page.locator('[data-testid="standings"]:visible [data-testid^="standing-tm_"]'),
  ).toHaveCount(12);
  await page.goto(`/leagues/${leagueId}/players`);
  await expect(page.getByText('Something went wrong')).toHaveCount(0);
});

test('a friend claims a seat with its link', async ({ page, request }) => {
  type Seat = {
    team: { id: string; name: string };
    claimed: boolean;
    invite: { code: string } | null;
  };
  const seats = await get<Seat[]>(request, IMPORTER, `/v1/leagues/${leagueId}/seats`);
  expect(seats).toHaveLength(12);
  expect(seats.filter((s) => s.claimed)).toHaveLength(1);
  const open = seats.find((s) => s.invite)!;
  await signIn(page, FRIEND);
  await page.goto(`/join/${open.invite!.code}`);
  await page.getByTestId('claim-seat-button').click();
  await page.waitForURL(new RegExp(`/leagues/${leagueId}`));
  const after = await get<Seat[]>(request, IMPORTER, `/v1/leagues/${leagueId}/seats`);
  expect(after.find((s) => s.team.id === open.team.id)?.claimed).toBe(true);
});

// Testing phase (docs/adr/0013): a friend with no account claims a seat
// with its link alone, in a fresh browser (no session), giving only a name.
test('a signed-out friend claims a seat with its link and a name, no email', async ({
  browser,
  request,
}) => {
  type Seat = {
    team: { id: string; name: string };
    claimed: boolean;
    invite: { code: string } | null;
  };
  const seats = await get<Seat[]>(request, IMPORTER, `/v1/leagues/${leagueId}/seats`);
  const open = seats.find((s) => s.invite)!;
  const context = await browser.newContext({ baseURL: test.info().project.use.baseURL });
  const page = await context.newPage();
  await page.goto(`/join/${open.invite!.code}`);
  await expect(page.getByTestId('claim-heading')).toHaveText(
    `Claim ${open.team.name} in ${LEAGUE_NAME}`,
  );
  await page.getByTestId('claim-name').fill('New Friend');
  await page.getByTestId('claim-sign-in-button').click();
  await page.waitForURL(new RegExp(`/leagues/${leagueId}/team$`));
  await expect(page.getByRole('heading', { level: 1, name: open.team.name })).toBeVisible();
  // Signed in for real: the stored session works against the API.
  const stored = await page.evaluate(() => localStorage.getItem('mfp.session'));
  const me = await request.get(`${API}/v1/me`, {
    headers: { Authorization: `Bearer ${token(stored!)}` },
  });
  expect(me.status()).toBe(200);
  expect(await me.json()).toMatchObject({ data: { displayName: 'New Friend', authProviders: [] } });
  await context.close();

  const after = await get<Seat[]>(request, IMPORTER, `/v1/leagues/${leagueId}/seats`);
  expect(after.find((s) => s.team.id === open.team.id)?.claimed).toBe(true);
  // The link is used up.
  const again = await request.post(`${API}/v1/auth/claim`, {
    data: { code: open.invite!.code, displayName: 'Someone Else' },
  });
  expect(again.status()).toBe(409);
});

test('the imported league is playable, and a re-sync puts FPL’s state back', async ({
  page,
  request,
}) => {
  test.setTimeout(90_000);
  const res = await request.patch(`${API}/v1/leagues/${leagueId}`, {
    headers: { Authorization: `Bearer ${token(IMPORTER)}` },
    data: { name: 'Renamed Here' },
  });
  expect(res.status()).toBe(200);

  await signIn(page, IMPORTER);
  await page.goto(`/leagues/${leagueId}/settings`);
  await page.getByTestId('resync').click();
  const dialog = page.getByTestId('resync-confirm');
  await expect(dialog).toContainText('This replaces all moves made here since');
  await dialog.getByTestId('confirm-ok').click();
  // The River job re-fetches the fake FPL (finished gameweeks are cached).
  await expect(page.getByTestId('sync-panel-note')).toHaveText(/^Re-synced from FPL\./, {
    timeout: 60_000,
  });
  type League = { league: { name: string } };
  const after = await get<League>(request, IMPORTER, `/v1/leagues/${leagueId}`);
  expect(after.league.name).toBe(LEAGUE_NAME);
  // The claimed seat stays claimed.
  type Seat = { claimed: boolean };
  const seats = await get<Seat[]>(request, IMPORTER, `/v1/leagues/${leagueId}/seats`);
  expect(seats.filter((s) => s.claimed)).toHaveLength(3);
});

test('branded images come through the API proxy, from the fake image host', async ({
  page,
  request,
}) => {
  const config = await request.get(`${API}/v1/config`);
  expect(((await config.json()) as { data: { brandedAssets: boolean } }).data.brandedAssets).toBe(
    true,
  );
  type Row = { player: { id: string; club: { code: string } } };
  const [row] = await get<Row[]>(request, IMPORTER, `/v1/leagues/${leagueId}/players?limit=1`);
  for (const path of [
    `/v1/assets/players/${row!.player.id}/photo`,
    `/v1/assets/clubs/${row!.player.club.code}/crest`,
    `/v1/assets/clubs/${row!.player.club.code}/shirt?gk=true`,
  ]) {
    // Public: images load without a token.
    const res = await request.get(`${API}${path}`);
    expect(res.status(), path).toBe(200);
    expect(res.headers()['content-type']).toBe('image/png');
    expect(res.headers()['cache-control']).toContain('max-age=');
  }
  // The pitch wears the proxied shirts.
  await signIn(page, IMPORTER);
  await page.goto(`/leagues/${leagueId}/team`);
  await page.getByTestId('view-pitch').click();
  const shirts = page.locator(
    '[data-testid="pitch-xi"]:visible [data-testid="branded-image"][data-kind="shirt"]',
  );
  await expect(shirts.first()).toHaveAttribute('data-state', 'loaded');
});
