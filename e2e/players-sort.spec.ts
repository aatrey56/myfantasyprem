import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);
const boxes = (page: Page) =>
  page.locator('[data-testid="player-list"]:visible:not([inert] *) [data-testid="player-points"]');

/** The numbers in the rows' boxes, top to bottom (the first rows). */
async function boxValues(page: Page, n = 12): Promise<number[]> {
  const texts = (await boxes(page).allTextContents()).slice(0, n);
  return texts.map(Number);
}

for (const vp of [
  { name: 'phone', width: 390, height: 844 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`players sort (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('searches xg, sorts by xGI, highest first; no ascending control', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      await expect(visible(page, 'sort-menu')).toContainText('Sort: PTS');
      await expect(visible(page, 'sort-menu')).not.toContainText(/[↓↑]/);

      await visible(page, 'sort-menu').click();
      const sheet = visible(page, 'sort-sheet');
      await expect(sheet.getByTestId('sort-goals')).toBeVisible();
      await expect(sheet.getByTestId('sort-order-asc')).toHaveCount(0);
      await expect(sheet.getByText(/ascending|descending/i)).toHaveCount(0);
      await expect(sheet.getByText('Form', { exact: true })).toBeVisible();
      await expect(sheet.getByText(/\(/)).toHaveCount(0);
      await sheet.getByTestId('sort-search').fill('xg');
      await expect(sheet.getByTestId('sort-expected_goals')).toBeVisible();
      await expect(sheet.getByTestId('sort-expected_goals_conceded')).toBeVisible();
      await expect(sheet.getByTestId('sort-expected_assists')).toHaveCount(0);
      await expect(sheet.getByTestId('sort-goals')).toHaveCount(0);
      await sheet.getByTestId('sort-expected_goal_involvements').click();
      await expect(visible(page, 'sort-sheet')).toHaveCount(0);

      await expect(visible(page, 'sort-menu')).toContainText('Sort: xGI');
      await expect(visible(page, 'sort-menu')).not.toContainText(/[↓↑]/);
      const first = boxes(page).first();
      await expect(first.locator('..')).toContainText('xGI');
      const desc = await boxValues(page);
      expect(desc.length).toBeGreaterThan(5);
      expect(desc[0]).toBeGreaterThan(0);
      expect(desc).toEqual([...desc].sort((a, b) => b - a));
    });

    test('a player row lists more than three fixtures in one line', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      const row = page
        .locator('[data-testid="player-list"]:visible:not([inert] *) [data-testid^="player-plr_"]')
        .first();
      const strip = row.getByTestId('next-fixtures');
      await expect(strip).toBeVisible();
      expect(await strip.getByTestId('fixture-strip-item').count()).toBeGreaterThan(3);
      const one = await strip.getByTestId('fixture-strip-item').first().boundingBox();
      const all = await strip.boundingBox();
      expect(all!.height).toBeLessThan(one!.height * 1.5);
    });
  });
}
