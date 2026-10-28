import { expect, test, type Page } from './fixtures';

import { LEAGUE, overflowingElements, signIn, wheel } from './helpers';

/** The Office League: offers both ways, a trade in a league vote. */
const OFFICE = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd0';

test.beforeEach(async ({ page }) => signIn(page));

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);
const visibleStarting = (page: Page, prefix: string) =>
  page.locator(`[data-testid^="${prefix}"]:visible:not([inert] *)`);
const rows = (page: Page) => visibleStarting(page, 'player-plr_');

/** The numbers in the rows' boxes, top to bottom. */
async function boxValues(page: Page): Promise<number[]> {
  const texts = await page
    .locator('[data-testid="player-list"]:visible:not([inert] *) [data-testid="player-points"]')
    .allTextContents();
  return texts.map((t) => Number(t.replace('+', '')));
}

/** Picks a View (All players, a position or a club); the dialog closes on the pick. */
async function pickView(page: Page, id: string) {
  await visible(page, 'view-menu').click();
  await visible(page, `view-${id}`).click();
  await expect(visible(page, 'view-sheet')).toHaveCount(0);
}

/** Turns the Available toggle on or off. */
async function setAvailable(page: Page, on: boolean) {
  const toggle = visible(page, 'filter-available');
  if ((await toggle.getAttribute('aria-pressed')) !== String(on)) await toggle.click();
  await expect(toggle).toHaveAttribute('aria-pressed', String(on));
}

/** The position in each row, top to bottom. */
async function rowPositions(page: Page): Promise<string[]> {
  const texts = await rows(page).allTextContents();
  return texts.map((t) => /(GKP|GK|DEF|MID|FWD)•/.exec(t)?.[1] ?? '?');
}

for (const vp of [
  { name: 'phone', width: 390, height: 844 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`players hub (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('the nav card sits under the tabs; rows show the next fixture and position stats', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/players`);
      const nav = visible(page, 'players-nav');
      await expect(nav).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Players', exact: true })).toHaveCount(0);
      for (const [id, label] of [
        ['open-search', 'Search'],
        ['open-trend', 'Trend'],
        ['open-available', 'Available'],
        ['open-leaders', 'Leaders'],
        ['open-trades', 'Trades'],
      ]) {
        await expect(nav.getByTestId(id)).toContainText(label);
      }
      await expect(visible(page, 'open-available')).toHaveAttribute('aria-current', 'page');
      // Chips: View · Available | ★ · tags. No position chips, club picker or filter sheet.
      const chips = visible(page, 'player-filters');
      for (const id of ['view-menu', 'filter-available', 'filter-watchlist']) {
        await expect(chips.getByTestId(id)).toBeVisible();
      }
      await expect(visible(page, 'view-menu')).toHaveText('View: All players');
      for (const id of ['open-filters', 'status-free_agent', 'position-all', 'club-filter']) {
        await expect(page.getByTestId(id)).toHaveCount(0);
      }

      await expect(visible(page, 'player-list')).toBeVisible();
      // No "Free agent" text: the fixture line says when they play next.
      await expect(visible(page, 'player-list')).not.toContainText('Free agent');
      const first = rows(page).first();
      await expect(first.getByTestId('next-fixtures')).toHaveText(
        /^(No fixture|.*(v|@) [A-Z]{3}.*)$/,
      );
      // Stat strips by position.
      await pickView(page, 'pos-DEF');
      await expect(visibleStarting(page, 'player-stats-').first()).toContainText('CS');
      await expect(visibleStarting(page, 'player-stats-').first()).toContainText('DC');
      await pickView(page, 'pos-GKP');
      await expect(visibleStarting(page, 'player-stats-').first()).toContainText('SV');
      await pickView(page, 'pos-MID');
      await expect(visibleStarting(page, 'player-stats-').first()).toContainText('xGI');
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('players on waivers carry a W badge with the waiver run day', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      const link = visible(page, 'open-waivers');
      await expect(link).toContainText(/next run (Mon|Tue|Wed|Thu|Fri|Sat|Sun) /);
      const day = /next run (\w{3}) /.exec((await link.textContent())!)![1]!;
      await expect(visible(page, 'players-scope-title')).toHaveText('Available');
      const claim = page
        .locator('[data-testid^="move-"]:visible:not([inert] *)[aria-label^="Claim "]')
        .first();
      await expect(claim).toHaveAccessibleName(new RegExp(`^Claim .+, waivers run ${day}$`));
      await expect(claim.getByTestId('waiver-day')).toHaveText(`(${day})`);
      expect(await overflowingElements(page)).toEqual([]);
    });

    test("⇄ on another team's player opens a trade with its owner", async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      await setAvailable(page, false);
      await expect(visible(page, 'players-scope-title')).toHaveText('All players');
      const trade = visibleStarting(page, 'trade-for-').first();
      await expect(trade).toHaveAccessibleName(/^Trade for /);
      const playerId = (await trade.getAttribute('data-testid'))!.replace('trade-for-', '');
      // My own players have no action.
      const mine = rows(page).filter({ hasText: 'Your team' }).first();
      const mineId = (await mine.getAttribute('data-testid'))!.replace('player-', '');
      await expect(visible(page, `trade-for-${mineId}`)).toHaveCount(0);
      await trade.click();
      await expect(page).toHaveURL(
        new RegExp(`/players/trades/new\\?teamId=tm_[0-9a-z]{26}&playerId=${playerId}$`),
      );
    });

    test('Trend lists 2-GW form risers, and fallers with the toggle', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      await visible(page, 'open-trend').click();
      await expect(visible(page, 'players-scope-title')).toHaveText('Trending');
      await expect(visible(page, 'trend-up')).toHaveAttribute('aria-checked', 'true');
      await expect(visible(page, 'player-list')).toContainText('Form ±');
      const up = await boxValues(page);
      expect(up.length).toBeGreaterThan(5);
      expect(up).toEqual([...up].sort((a, b) => b - a));
      expect(up[0]).toBeGreaterThan(0);
      await visible(page, 'trend-down').click();
      await expect.poll(async () => (await boxValues(page))[0] ?? 0).toBeLessThan(0);
      const down = await boxValues(page);
      expect(down).toEqual([...down].sort((a, b) => a - b));
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('Leaders lists top scorers for the season or the gameweek, by position', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/players`);
      await visible(page, 'open-leaders').click();
      await expect(visible(page, 'players-scope-title')).toHaveText('Leaders');
      const season = await boxValues(page);
      expect(season).toEqual([...season].sort((a, b) => b - a));
      await pickView(page, 'pos-FWD');
      await expect(rows(page).first()).toContainText('FWD');
      await visible(page, 'leaders-gw').click();
      await expect(visible(page, 'player-list')).toContainText('GW');
      const gw = await boxValues(page);
      expect(gw).toEqual([...gw].sort((a, b) => b - a));
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('sorts by GW points, total or form', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      await visible(page, 'sort-menu').click();
      const sheet = visible(page, 'sort-sheet');
      await expect(sheet.getByTestId('sort-gameweek_points')).toContainText('GW points');
      await expect(sheet.getByTestId('sort-total_points')).toContainText('Total points');
      await expect(sheet.getByTestId('sort-form')).toContainText('Form');
      await sheet.getByTestId('sort-gameweek_points').click();
      await expect(visible(page, 'sort-menu')).toContainText('Sort: GW');
      const gw = await boxValues(page);
      expect(gw).toEqual([...gw].sort((a, b) => b - a));
    });

    test('a starred player shows under the watchlist chip, and unstarring empties it', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/players`);
      const row = rows(page).first();
      const label = await row.getByTestId(/^open-card-/).getAttribute('aria-label');
      const name = label!.replace('Player card for ', '');
      await row.click();
      const card = visible(page, 'player-card');
      const star = card.getByTestId('card-watch');
      await expect(star).toHaveAttribute('aria-pressed', 'false');
      await star.click();
      await expect(page.getByTestId('toast')).toContainText('to your watchlist');
      await expect(star).toHaveAttribute('aria-pressed', 'true');
      await page.keyboard.press('Escape');
      await expect(visible(page, 'player-card')).toHaveCount(0);

      await visible(page, 'filter-watchlist').click();
      await expect(visible(page, 'players-scope-title')).toHaveText('Watchlist');
      await expect(rows(page)).toHaveCount(1);
      await expect(rows(page).first()).toContainText(name);

      await rows(page).first().click();
      await visible(page, 'player-card').getByTestId('card-watch').click();
      await expect(page.getByTestId('toast')).toContainText('from your watchlist');
      await page.keyboard.press('Escape');
      await expect(visible(page, 'players-empty')).toContainText('Your watchlist is empty');
    });

    test('DROP on my own player picks a same-position replacement', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      await setAvailable(page, false);
      await rows(page).filter({ hasText: 'Your team' }).first().click();
      const card = visible(page, 'player-card');
      await expect(card.getByTestId('card-drop')).toHaveText('Drop');
      await expect(card.getByTestId('card-watch')).toBeVisible();
      await card.getByTestId('card-drop').click();
      await expect(visible(page, 'replace-step')).toBeVisible();
      await expect(visible(page, 'replace-confirm')).toBeDisabled();
      // Pick a free agent (not one on waivers) so the move is immediate.
      await visibleStarting(page, 'replace-plr_').filter({ hasNotText: 'Waivers' }).first().click();
      await expect(visible(page, 'replace-confirm')).toHaveText('Add and drop');
      await visible(page, 'replace-confirm').click();
      await expect(page.getByTestId('toast')).toContainText(/^Added .+, dropped .+/);
      expect(await overflowingElements(page)).toEqual([]);
    });
  });

  test.describe(`View and Available (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('Defenders shows only DEF rows', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      await expect(rows(page).first()).toBeVisible();
      await pickView(page, 'pos-DEF');
      await expect(visible(page, 'view-menu')).toHaveText('View: Defenders');
      await expect.poll(async () => new Set(await rowPositions(page))).toEqual(new Set(['DEF']));
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('a club shows only that club, one choice at a time', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      await expect(rows(page).first()).toBeVisible();
      await visible(page, 'view-menu').click();
      const options = visible(page, 'view-sheet').locator('[data-testid^="view-"][role="radio"]');
      const names = await options.evaluateAll((els) =>
        els.map((e) => e.getAttribute('aria-label')),
      );
      expect(names.slice(0, 5)).toEqual([
        'All players',
        'Goalkeepers',
        'Defenders',
        'Midfielders',
        'Forwards',
      ]);
      const clubs = names.slice(5) as string[];
      expect(clubs.length).toBeGreaterThanOrEqual(20);
      expect(clubs).toEqual([...clubs].sort((a, b) => a.localeCompare(b)));
      await expect(options.and(page.locator('[aria-checked="true"]'))).toHaveCount(1);
      const club = visible(page, 'view-sheet').locator('[data-testid^="view-club-"]').first();
      const code = (await club.getAttribute('data-testid'))!.replace('view-club-', '');
      const name = (await club.getAttribute('aria-label'))!;
      await club.click();
      await expect(visible(page, 'view-sheet')).toHaveCount(0);
      await expect(visible(page, 'view-menu')).toHaveText(`View: ${name}`);
      await expect(rows(page).first()).toBeVisible();
      await expect
        .poll(async () => (await rows(page).allTextContents()).every((t) => t.includes(`•${code}`)))
        .toBe(true);
    });

    test('Available on lists only unowned players; off brings owned ones back', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/players`);
      await setAvailable(page, false);
      await expect(rows(page).filter({ hasText: 'Your team' }).first()).toBeVisible();
      expect(await visibleStarting(page, 'trade-for-').count()).toBeGreaterThan(0);
      await setAvailable(page, true);
      await expect(visible(page, 'players-scope-title')).toHaveText('Available');
      await expect(visibleStarting(page, 'trade-for-')).toHaveCount(0);
      await expect(rows(page).filter({ hasText: 'Your team' })).toHaveCount(0);
      expect(await rows(page).count()).toBeGreaterThan(0);
    });

    test('Search finds an owned player with Available on', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      await setAvailable(page, false);
      const owned = rows(page).filter({ hasText: 'Your team' }).first();
      await expect(owned).toBeVisible();
      const id = (await owned.getAttribute('data-testid'))!;
      const name = (
        (await owned.locator('[data-testid^="open-card-"]').getAttribute('aria-label')) ?? ''
      ).replace('Player card for ', '');
      expect(name.length).toBeGreaterThan(1);
      await setAvailable(page, true);
      await visible(page, 'open-search').click();
      await visible(page, 'player-search').fill(name);
      await expect(visible(page, id)).toBeVisible();
      // Search leaves the league's Available toggle as it was.
      await visible(page, 'open-available').click();
      await expect(visible(page, 'filter-available')).toHaveAttribute('aria-pressed', 'true');
    });

    test('Sort works together with View', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      await pickView(page, 'pos-MID');
      await visible(page, 'sort-menu').click();
      await visible(page, 'sort-sheet').getByTestId('sort-gameweek_points').click();
      await expect(visible(page, 'sort-menu')).toContainText('Sort: GW');
      await expect(visible(page, 'view-menu')).toHaveText('View: Midfielders');
      await expect.poll(async () => new Set(await rowPositions(page))).toEqual(new Set(['MID']));
      const gw = await boxValues(page);
      expect(gw.length).toBeGreaterThan(1);
      expect(gw).toEqual([...gw].sort((a, b) => b - a));
      // Both persist for the league.
      await page.reload();
      await expect(visible(page, 'view-menu')).toHaveText('View: Midfielders');
      await expect(visible(page, 'sort-menu')).toContainText('Sort: GW');
    });
  });

  test.describe(`trades view (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('one Active list, newest first, tagged incoming or outgoing; Past apart', async ({
      page,
    }) => {
      await page.goto(`${OFFICE}/players/trades`);
      await expect(visible(page, 'players-nav')).toBeVisible();
      await expect(visible(page, 'back-link')).toHaveCount(0);
      await expect(visible(page, 'trades-title')).toHaveText('Active Trades');
      const cards = visible(page, 'trade-list').locator('[data-testid^="trade-trd_"]');
      await expect(cards.first()).toBeVisible();
      expect(await cards.count()).toBeGreaterThanOrEqual(3);
      const directions = await visible(page, 'trade-list')
        .getByTestId('trade-direction')
        .allTextContents();
      expect(directions.map((d) => d.toUpperCase())).toEqual(
        expect.arrayContaining(['INCOMING', 'OUTGOING']),
      );
      // Newest first: "3h ago" before "6h ago" before older days.
      const ago = await visible(page, 'trade-list')
        .getByTestId('trade-offered-at')
        .allTextContents();
      const hours = ago.map((t) => (/^(\d+)h ago$/.exec(t) ? Number(/^(\d+)/.exec(t)![1]) : 1e6));
      expect(hours).toEqual([...hours].sort((a, b) => a - b));
      await expect(visible(page, 'bucket-history')).toHaveText('Past');
      await visible(page, 'bucket-history').click();
      await expect(visible(page, 'trades-title')).toHaveText('Past Trades');
      await expect(visible(page, 'trade-list')).toHaveCount(0);
      expect(await overflowingElements(page)).toEqual([]);
    });
  });
}

test.describe('players list (phone 390)', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('scrolls, loads the next page at the end, and filters', async ({ page }) => {
    await page.goto(`${LEAGUE}/players`);
    await expect(rows(page).first()).toBeVisible();
    const before = await rows(page).count();
    // Scroll the screen towards its end, as a finger or wheel would: the next page loads.
    const scroller = visible(page, 'screen-scroll');
    const box = (await scroller.boundingBox())!;
    for (let i = 0; i < 8 && (await rows(page).count()) === before; i++) {
      await wheel(page, box.x + box.width / 2, box.y + box.height / 2, 0, 2000);
      await page.waitForTimeout(200);
    }
    await expect.poll(() => rows(page).count()).toBeGreaterThan(before);
    expect(await scroller.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
    // A position view keeps only that position.
    await pickView(page, 'pos-DEF');
    await expect(rows(page).first()).toContainText('DEF');
    for (const text of await rows(page).allTextContents()) expect(text).toContain('DEF');
  });
});

test.describe('players row fixtures by keyboard (desktop)', () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test('a row reads its next fixture and how many follow; the card lists them all', async ({
    page,
  }) => {
    await page.goto(`${LEAGUE}/players`);
    const row = rows(page).first();
    await expect(row).toBeVisible();
    const card = row.locator('[data-testid^="open-card-"]');
    // The sideways strip is a picture of what the row already says: it
    // offers nothing to focus inside the row's button.
    const strip = row.locator('[data-testid="next-fixtures"]');
    await expect(strip).toHaveAttribute('aria-hidden', 'true');
    expect(
      await strip.evaluate(
        (el) => el.querySelectorAll('[tabindex]:not([tabindex="-1"]), button, a').length,
      ),
    ).toBe(0);
    await expect(card).toHaveAccessibleDescription(/next .+, then \d+ more fixtures/);
    // Reach the row by keyboard and open the card: its Fixtures tab lists every one.
    await card.focus();
    await expect(card).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(visible(page, 'card-tab-fixtures')).toBeVisible();
  });
});
