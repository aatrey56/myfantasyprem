import { expect, test, type Page } from './fixtures';

import { LATE_LEAGUE, LEAGUE, overflowingElements, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);

for (const vp of [
  { name: 'phone', width: 375, height: 812 },
  { name: 'desktop', width: 1280, height: 800 },
] as const) {
  test.describe(`commissioner moves (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('the Status banner leads to the moves under League rules, and goes once seen', async ({
      page,
    }) => {
      // The demo user is a manager in The Sunday Draft; its commissioner made three moves.
      await page.goto(LEAGUE);
      const banner = visible(page, 'commissioner-moves-banner');
      await expect(banner).toBeVisible();
      await expect(banner.getByTestId('commissioner-moves-title')).toHaveText(
        'Commissioner moves · 3 new',
      );
      await expect(banner.getByTestId('commissioner-moves-latest')).toHaveText(
        'Changed the trade review period to 48 hours.',
      );
      expect(await overflowingElements(page)).toEqual([]);

      await banner.click();
      await expect(page).toHaveURL(/\/settings\?section=commissioner-moves$/);

      // League settings, scrolled to the section, which sits under League rules.
      const section = page.getByTestId('commissioner-moves-section');
      await expect(section.getByRole('heading', { name: 'Commissioner Moves' })).toBeVisible();
      const rows = section.locator('[data-testid^="commissioner-move-cmv_"]');
      await expect(rows).toHaveCount(3);
      await expect(rows.nth(0)).toContainText('Changed the trade review period to 48 hours.');
      await expect(rows.nth(0)).toContainText('By ');
      await expect(rows.nth(1)).toContainText('Vetoed the trade between');
      await expect(rows.nth(2)).toContainText('Changed the veto threshold to 33% of managers.');
      await expect(section).toBeInViewport();
      const rules = (await page.getByText('League rules', { exact: true }).boundingBox())!;
      const moves = (await section.boundingBox())!;
      expect(rules.y).toBeLessThan(moves.y);
      expect(await overflowingElements(page)).toEqual([]);

      // Tapped: seen. Back on Status the banner is gone.
      await page.goBack();
      await expect(visible(page, 'status-loaded')).toBeVisible();
      await expect(page.getByTestId('commissioner-moves-banner')).toHaveCount(0);
    });

    test('the commissioner switches an in-season league to a league vote, and it is logged', async ({
      page,
    }) => {
      // Drafted mid-season, on commissioner review; the demo user is its commissioner.
      await page.goto(`${LATE_LEAGUE}/settings`);
      const review = visible(page, 'trade-review');
      await expect(review).toBeVisible();
      await expect(visible(page, 'trade-review-hint')).toContainText(
        'Trades already in review keep the review they started with',
      );
      await expect(visible(page, 'veto-threshold')).toHaveCount(0);
      await review.getByRole('radio', { name: 'Vote' }).click();
      await expect(visible(page, 'veto-threshold')).toBeVisible();
      await visible(page, 'save-settings').click();
      await expect(page.getByTestId('toast')).toContainText('Settings saved');
      await expect(visible(page, 'save-settings')).toBeDisabled();
      // The league came back on a vote: its veto threshold shows.
      await expect(visible(page, 'veto-threshold')).toBeVisible();

      const section = page.getByTestId('commissioner-moves-section');
      const rows = section.locator('[data-testid^="commissioner-move-cmv_"]');
      // The league's three earlier moves, and this one on top.
      await expect(rows).toHaveCount(4);
      await expect(rows.first()).toContainText('Changed the trade review to league vote');
      expect(await overflowingElements(page)).toEqual([]);
    });
  });
}
