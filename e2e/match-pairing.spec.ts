import { expect, test } from './fixtures';

import { LEAGUE, overflowingElements, signIn } from './helpers';

// Match tab: starters pair "in order", so different formations never leave an
// empty half-row. The demo matchup is a mismatched pair (see the split badge).

test.beforeEach(async ({ page }) => signIn(page));

test.use({ viewport: { width: 390, height: 844 } });

test('MATCH (phone): 11 starter rows, a player on both sides of every row', async ({ page }) => {
  await page.goto(`${LEAGUE}/matchup`);
  const starters = page.locator('[data-testid="h2h-starters"]:visible');
  await expect(starters).toBeVisible();
  const rows = starters.locator('> *');
  await expect(rows).toHaveCount(11);
  for (let i = 0; i < 11; i++) {
    // Each side of the row holds a player (name card + points).
    await expect(rows.nth(i).locator('[data-testid^="open-card-"]')).toHaveCount(2);
    await expect(rows.nth(i).locator('[data-testid^="h2h-breakdown-"]')).toHaveCount(2);
  }
  // Row 1 is the goalkeepers; a mismatched pairing shows a split badge somewhere.
  await expect(rows.first().getByLabel('Position GK')).toBeVisible();
  const split = starters.getByTestId('pair-badge-split');
  expect(await split.count()).toBeGreaterThan(0);
  await expect(split.first().getByTestId('pair-badge-left')).toBeVisible();
  await expect(split.first().getByTestId('pair-badge-right')).toBeVisible();
  expect(await overflowingElements(page)).toEqual([]);
});
