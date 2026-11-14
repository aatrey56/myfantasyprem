import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn, startWithScenario } from './helpers';

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);

// The gameweek deadline has passed and the gameweek is not finished: no
// free-agent signings until the next waiver run, anywhere a Sign button
// would appear.
for (const vp of [
  { name: 'phone', width: 375, height: 812 },
  { name: 'desktop', width: 1280, height: 800 },
] as const) {
  test.describe(`locked gameweek (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });
    test.beforeEach(async ({ page }) => {
      await startWithScenario(page, 'transactions-locked');
      await signIn(page);
    });

    test('Status shows the phase and no Sign buttons', async ({ page }) => {
      await page.goto(LEAGUE);
      await expect(visible(page, 'transaction-phase')).toContainText(/^Waivers due \w{3} \d/);
      const top = visible(page, 'top-available');
      await expect(top).toContainText('Waivers due');
      await expect(top.getByRole('button', { name: /^(Waivers|Sign)$/ })).toHaveCount(0);
      await expect(page.locator('[data-testid^="sign-"]:visible:not([inert] *)')).toHaveCount(0);
    });

    test('Players hub and the player card say when waivers are due instead of Sign', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/players`);
      const row = page.locator('[data-testid^="player-plr_"]:visible:not([inert] *)').first();
      await expect(row).toContainText('Waivers due');
      await expect(page.locator('[data-testid^="move-"]:visible:not([inert] *)')).toHaveCount(0);
      await row.click();
      const card = visible(page, 'player-card');
      await expect(card.getByTestId('card-owner')).toContainText('Waivers due');
      await expect(card.getByTestId('card-add')).toHaveCount(0);
    });
  });
}
