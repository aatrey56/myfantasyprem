import { expect, test } from './fixtures';

import { LEAGUE, signIn } from './helpers';

// The FPL sync status, re-sync and checks live in league settings only, and
// only for a league imported from FPL Draft. No league tab shows them.
const IMPORTED = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd5';
const MISMATCH = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd6';
const TABS = ['', '/matchup', '/team', '/players', '/league'];
const FPL_TEXT = /Imported from FPL|last synced|Matches FPL|FPL check|Re-synced from FPL|mismatch/;

for (const vp of [
  { name: 'phone', width: 375, height: 812 },
  { name: 'desktop', width: 1280, height: 800 },
] as const) {
  test.describe(`FPL sync placement (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });
    test.beforeEach(async ({ page }) => signIn(page));

    for (const league of [IMPORTED, MISMATCH, LEAGUE]) {
      test(`no FPL banner on any tab of ${league}`, async ({ page }) => {
        for (const tab of TABS) {
          await page.goto(`${league}${tab}`);
          await expect(
            page
              .locator(
                '[data-testid="top-bar-settings"]:visible, [data-testid="nav-settings"]:visible',
              )
              .first(),
          ).toBeVisible();
          await expect(page.locator('[data-testid^="sync-"]:visible')).toHaveCount(0);
          await expect(page.getByText(FPL_TEXT)).toHaveCount(0);
        }
      });
    }

    test('the leagues hub shows no FPL sync status', async ({ page }) => {
      await page.goto('/leagues');
      await expect(page.getByText('The Sunday Draft').first()).toBeVisible();
      await expect(page.getByText(FPL_TEXT)).toHaveCount(0);
    });

    test('settings of an imported league has the sync panel', async ({ page }) => {
      await page.goto(`${IMPORTED}/settings`);
      await expect(page.getByTestId('sync-panel')).toContainText('Imported from FPL · last synced');
      await expect(page.getByTestId('resync')).toBeVisible();
      await expect(page.getByTestId('check-sync')).toBeVisible();
    });

    test('settings of a native league has nothing FPL-related', async ({ page }) => {
      await page.goto(`${LEAGUE}/settings`);
      await expect(page.getByTestId('settings-league-name')).toBeVisible();
      await expect(page.getByTestId('sync-panel')).toHaveCount(0);
      await expect(page.getByTestId('resync')).toHaveCount(0);
      await expect(page.getByText(/FPL/)).toHaveCount(0);
    });
  });
}
