import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn, useScenario } from './helpers';

/**
 * The phone "freeze": after a refused swap, tapping the position pill again
 * left an invisible sheet over the app that took every touch. A refusal,
 * a second tap and then a tab switch must all still work.
 */
test.beforeEach(async ({ page }) => signIn(page));
test.use({ viewport: { width: 375, height: 812 } });

const PILL = '[data-testid^="lineup-swap-"]';
const pillId = async (page: Page, section: string, index: number) =>
  (await page.getByTestId(section).locator(PILL).nth(index).getAttribute('data-testid'))!.replace(
    'lineup-swap-',
    '',
  );

test('refuse a swap, tap the position again, and a tab switch still works', async ({ page }) => {
  await page.goto(`${LEAGUE}/team`);
  await page.getByTestId('view-list').click();
  await expect(page.getByTestId('list-starters').locator(PILL)).toHaveCount(11);
  const keeper = await pillId(page, 'list-starters', 0);
  const backup = await pillId(page, 'list-bench', 0);
  await useScenario(page, 'lineup-save-refused');

  await page.getByTestId(`lineup-swap-${keeper}`).click();
  await page.getByTestId(`swap-candidate-${backup}`).click();
  await expect(page.getByTestId('lineup-errors')).toContainText('The server refused this lineup.');
  await expect(page.getByTestId('swap-sheet')).toHaveCount(0);

  // Tap the pill again: the sheet opens with its panel on screen, and closes.
  await page.getByTestId(`lineup-swap-${keeper}`).click();
  const sheet = page.getByTestId('swap-sheet');
  await expect(sheet).toBeInViewport();
  await sheet.getByTestId('sheet-close').click();
  await expect(sheet).toHaveCount(0);
  await expect(page.getByTestId('sheet-backdrop')).toHaveCount(0);

  // Nothing is left over the screen: a tab switch lands.
  await page.getByRole('tab', { name: 'League' }).first().click();
  await expect(page).toHaveURL(/\/league$/);
});
