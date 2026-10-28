import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn } from './helpers';

// Match tab header: names left-aligned and whole, the season strip with
// average points against, the points-share bar, and the GW badge as a
// gameweek picker (past scores, future previews with fixtures).

test.beforeEach(async ({ page }) => signIn(page));

async function namesWhole(page: Page) {
  for (const id of ['score-left-name', 'score-right-name']) {
    const el = page.getByTestId('score-header').getByTestId(id);
    await expect(el).toBeVisible();
    const cut = await el.evaluate(
      (e) => e.scrollWidth > e.clientWidth + 1 || getComputedStyle(e).textAlign !== 'left',
    );
    expect(cut, `${id} cut short or not left-aligned`).toBe(false);
  }
}

async function pick(page: Page, gw: number) {
  await page.getByTestId('score-header').getByTestId('gw-picker').click();
  await page.getByTestId(`gw-option-${gw}`).click();
  await expect(page.getByTestId(`gw-option-${gw}`)).toHaveCount(0);
}

for (const [name, viewport] of [
  ['phone', { width: 375, height: 812 }],
  ['desktop', { width: 1280, height: 900 }],
] as const) {
  test.describe(name, () => {
    test.use({ viewport });

    test('header: whole names, avg pts against, points-share bar', async ({ page }) => {
      await page.goto(`${LEAGUE}/matchup`);
      const header = page.getByTestId('score-header');
      await expect(header).toBeVisible();
      await namesWhole(page);
      await expect(header.getByTestId('season-start-sit')).toBeVisible();
      await expect(header.getByTestId('season-avg')).toBeVisible();
      await expect(header.getByTestId('season-avg-against')).toContainText(/Avg pts against|\d/);
      await expect(header.getByTestId('points-split')).toBeVisible();
    });

    test('pick GW 3 shows its scores; a future GW shows a preview', async ({ page }) => {
      await page.goto(`${LEAGUE}/matchup`);
      await expect(page.getByTestId('score-header')).toBeVisible();
      await pick(page, 3);
      await expect(page).toHaveURL(/gw=3/);
      await expect(page.getByTestId('score-header').getByTestId('score-left-points')).toHaveText(
        /^\d+$/,
      );
      await pick(page, 20);
      await expect(page.getByTestId('matchup-preview')).toBeVisible();
      await expect(page.getByTestId('preview-fixture').first()).toBeVisible();
      await expect(page.getByTestId('score-header').getByTestId('score-left-points')).toHaveText(
        '–',
      );
      await expect(page.getByTestId('score-header').getByTestId('points-split')).toHaveCount(0);
      await namesWhole(page);
    });
  });
}
