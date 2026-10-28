import { expect, test, type Page } from './fixtures';

import { overflowingElements, signIn, startWithScenario } from './helpers';

// SCORES on the mocks (apps/app/src/mocks/world.ts): gameweek 7 is live
// with four matches over, one in each half, one at half time and three to
// come; 6 is finished; 11 has a postponed fixture; 12 has none.

test.beforeEach(async ({ page }) => signIn(page));

/** Sections keep earlier screens mounted but hidden: only match what is on screen. */
const visible = (page: Page, testId: string) => page.locator(`[data-testid="${testId}"]:visible`);
const rows = (page: Page) => page.locator('[data-testid^="fixture-fx_"]:visible');
const liveRows = (page: Page) => rows(page).filter({ has: page.getByTestId('fixture-live') });

/**
 * Opens fixtures of gameweek 7, then 6, until one has a player on one of
 * my teams; returns with that fixture's box score on screen.
 */
async function openFixtureWithMine(page: Page) {
  for (const gw of [7, 6]) {
    for (let i = 0; i < 10; i++) {
      await page.goto(`/scores?gw=${gw}`);
      await expect(rows(page)).toHaveCount(10);
      await rows(page).nth(i).click();
      await expect(visible(page, 'fixture-hero')).toBeVisible();
      const detail = visible(page, 'fixture-detail');
      await expect(detail).toBeVisible();
      if ((await detail.getByTestId('owner-badge-mine').count()) > 0) return;
    }
  }
  throw new Error('no fixture has a player of mine');
}

for (const vp of [
  { name: 'phone 390', width: 390, height: 844 },
  { name: 'desktop 1280', width: 1280, height: 800 },
] as const) {
  test.describe(`scores (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('opens on the live gameweek by day, live matches in the accent with their clock', async ({
      page,
    }) => {
      await page.goto('/scores');
      await expect(visible(page, 'gw-label')).toHaveText('GW 7');
      await expect(page.getByText('Gameweek 7 · 3 live')).toBeVisible();
      // Grouped by day: a heading per day, every fixture under one.
      const days = visible(page, 'scores-day');
      expect(await days.count()).toBeGreaterThanOrEqual(2);
      await expect(days.first()).toHaveText(/^(Mon|Tue|Wed|Thu|Fri|Sat|Sun) \d{1,2} [A-Z][a-z]{2}/);
      await expect(rows(page)).toHaveCount(10);

      await expect(liveRows(page)).toHaveCount(3);
      const clocks = await liveRows(page).getByTestId('fixture-clock').allTextContents();
      expect(clocks.sort()).toEqual(["38'", "71'", 'HT']);
      // Live rows take the accent border; a finished one keeps the plain border.
      const border = (testId: string) =>
        rows(page)
          .filter({ has: page.getByTestId(testId) })
          .first()
          .evaluate((el) => getComputedStyle(el).borderTopColor);
      const finished = rows(page).filter({
        has: page.getByTestId('fixture-status').getByText('FT', { exact: true }),
      });
      await expect(finished).toHaveCount(4);
      expect(await border('fixture-live')).not.toBe(
        await finished.first().evaluate((el) => getComputedStyle(el).borderTopColor),
      );
      // Live scores are numbers; matches to come show their kickoff time.
      for (const score of await liveRows(page).getByTestId('fixture-score-home').all()) {
        await expect(score).toHaveText(/^\d+$/);
      }
      const upcoming = visible(page, 'fixture-status').filter({ hasText: /^\d\d:\d\d$/ });
      await expect(upcoming).toHaveCount(3);
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('steps to a finished, a postponed and a blank gameweek', async ({ page }) => {
      await page.goto('/scores');
      await expect(visible(page, 'gw-label')).toHaveText('GW 7');
      await visible(page, 'gw-prev').click();
      await expect(visible(page, 'gw-label')).toHaveText('GW 6');
      await expect(page).toHaveURL(/\/scores\?gw=6$/);
      await expect(liveRows(page)).toHaveCount(0);
      await expect(visible(page, 'fixture-status')).toHaveText(Array(10).fill('FT'));

      await page.goto('/scores?gw=11');
      await expect(visible(page, 'gw-label')).toHaveText('GW 11');
      const postponed = visible(page, 'scores-day-postponed');
      await expect(postponed).toBeVisible();
      await expect(postponed.getByTestId('scores-day')).toContainText('Postponed');
      await expect(postponed.locator('[data-testid^="fixture-fx_"]')).toHaveCount(1);
      await expect(postponed.getByTestId('fixture-status')).toHaveText('PP');
      // The rest are on their days, before it.
      await expect(rows(page)).toHaveCount(10);

      await visible(page, 'gw-next').click();
      await expect(visible(page, 'gw-label')).toHaveText('GW 12');
      await expect(visible(page, 'scores-empty')).toBeVisible();
      await expect(visible(page, 'scores-empty')).toContainText('No fixtures');
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('a live fixture opens its box score with owners, mine emphasised', async ({ page }) => {
      await page.goto('/scores');
      await liveRows(page).first().click();
      await expect(page).toHaveURL(/\/scores\/fx_[0-9a-z]{26}$/);
      const hero = visible(page, 'fixture-hero');
      await expect(hero).toBeVisible();
      await expect(hero.getByTestId('fixture-hero-status')).toHaveText(/^(\d+'|HT)$/);
      await expect(hero.getByTestId('fixture-hero-score')).toHaveText(/^\d+–\d+$/);
      const detail = visible(page, 'fixture-detail');
      await expect(detail.getByTestId('fixture-side-home')).toBeVisible();
      await expect(detail.getByTestId('fixture-side-away')).toBeVisible();
      const players = detail.locator('[data-testid^="fx-player-plr_"]');
      expect(await players.count()).toBeGreaterThan(10);
      const points = (await players.getByTestId('fx-player-points').allTextContents()).map(Number);
      expect(points.every(Number.isInteger)).toBe(true);
      // Highest first: the first row of a side outscores its last.
      expect(points[0]).toBeGreaterThanOrEqual(points.at(-1)!);
      // Players owned in my leagues carry team initials.
      await expect(detail.getByTestId('owner-badges').first()).toBeVisible();
      await expect(detail.getByTestId('owner-badge').first()).toHaveText(/^[A-Z0-9]{1,2}$/);
      expect(await overflowingElements(page)).toEqual([]);

      // Back to the same gameweek's list.
      await visible(page, 'back-link').click();
      await expect(page).toHaveURL(/\/scores\?gw=7$/);
      await expect(visible(page, 'scores-list')).toBeVisible();
    });

    test('my players stand out and open the player card', async ({ page }) => {
      await openFixtureWithMine(page);
      const detail = visible(page, 'fixture-detail');
      const mine = detail
        .locator('[data-testid^="fx-player-plr_"]')
        .filter({ has: page.getByTestId('owner-badge-mine') });
      const count = await mine.count();
      expect(count).toBeGreaterThan(0);
      await expect(visible(page, 'fixture-mine-count')).toHaveText(
        count === 1 ? '1 of your players' : `${count} of your players`,
      );
      // My badge first, then the other leagues' owners.
      const first = mine.first();
      await expect(
        first.locator('[data-testid^="owner-badge-"], [data-testid="owner-badge"]').first(),
      ).toHaveAttribute('data-testid', 'owner-badge-mine');
      // Emphasised: tinted, unlike an unowned or not-mine row.
      const bg = (await first.evaluate((el) => getComputedStyle(el).backgroundColor)) as string;
      const plain = detail
        .locator('[data-testid^="fx-player-plr_"]')
        .filter({ hasNot: page.getByTestId('owner-badge-mine') })
        .first();
      expect(bg).not.toBe(await plain.evaluate((el) => getComputedStyle(el).backgroundColor));

      const label = (await first.getAttribute('aria-label'))!;
      expect(label).toMatch(/^Player card for .+, -?\d+ points, /);
      expect(label).toContain('(your team)');
      const name = label.replace(/^Player card for /, '').split(', ')[0]!;
      await first.click();
      const card = visible(page, 'player-card');
      await expect(card).toBeVisible();
      await expect(card.getByTestId('card-name')).toContainText(name.split(' ').at(-1)!);
      await page.keyboard.press('Escape');
      await expect(visible(page, 'player-card')).toHaveCount(0);
      await expect(visible(page, 'fixture-detail')).toBeVisible();
    });

    test('a match to come lists only owned players; a postponed one says so', async ({ page }) => {
      await page.goto('/scores?gw=9');
      await rows(page).first().click();
      const detail = visible(page, 'fixture-detail');
      await expect(detail).toBeVisible();
      await expect(visible(page, 'fixture-hero').getByTestId('fixture-hero-score')).toHaveCount(0);
      const players = detail.locator('[data-testid^="fx-player-plr_"]');
      expect(await players.count()).toBeGreaterThan(0);
      for (const p of await players.all()) {
        await expect(p.getByTestId('owner-badges')).toBeVisible();
        await expect(p.getByTestId('fx-player-points')).toHaveText('0');
      }

      await page.goto('/scores?gw=11');
      await visible(page, 'scores-day-postponed').locator('[data-testid^="fixture-fx_"]').click();
      await expect(visible(page, 'fixture-hero-status')).toHaveText('Postponed');
      await expect(visible(page, 'fixture-postponed')).toBeVisible();
    });
  });
}

test.describe('scores crests', () => {
  test('crests show with branded images on, club names only with them off', async ({ page }) => {
    await page.goto('/scores');
    await expect(rows(page)).toHaveCount(10);
    await expect(
      rows(page).first().locator('[data-testid="branded-image"][data-kind="crest"]'),
    ).toHaveCount(2);
  });

  test('no crest requests with branded images off', async ({ page }) => {
    await startWithScenario(page, 'branded-off');
    const assets: string[] = [];
    page.on('request', (r) => {
      if (r.url().includes('/v1/assets/')) assets.push(r.url());
    });
    await page.goto('/scores');
    await expect(rows(page)).toHaveCount(10);
    await expect(rows(page).first()).toContainText(/\w+/);
    await expect(page.locator('[data-testid="branded-image"]')).toHaveCount(0);
    expect(assets).toEqual([]);
  });
});
