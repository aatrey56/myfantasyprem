import { expect, test, type Page } from './fixtures';

import { LEAGUE, overflowingElements, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

/** The mock league the demo user runs as commissioner, in season. */
const OFFICE = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd0';

const visible = (page: Page, testId: string) => page.locator(`[data-testid="${testId}"]:visible`);

test.use({ viewport: { width: 375, height: 812 } });

test('the waivers screen explains the reverse-standings order', async ({ page }) => {
  await page.goto(`${LEAGUE}/players/waivers`);
  await expect(visible(page, 'waiver-priority')).toBeVisible();
  await expect(visible(page, 'waiver-order-help')).toContainText('the bottom team picks first');
  expect(await overflowingElements(page)).toEqual([]);
});

test('members see the waiver order rule, read only', async ({ page }) => {
  await page.goto(`${LEAGUE}/settings`);
  await expect(visible(page, 'waiver-order-value')).toHaveText('Reverse standings');
  await expect(visible(page, 'waiver-order')).toHaveCount(0);
});

test('the commissioner switches to rolling from the next waiver run', async ({ page }) => {
  await page.goto(`${OFFICE}/settings`);
  await expect(visible(page, 'waiver-order-effect')).toContainText(
    /^A change applies from the GW \d+ waiver run, /,
  );
  await visible(page, 'waiver-order-rolling').click();
  await expect(visible(page, 'waiver-order-help')).toContainText('stays there for later runs');
  await visible(page, 'save-settings').click();
  await expect(page.getByTestId('toast')).toContainText('Settings saved');
  // The league came back with the new rule: nothing left to save.
  await expect(visible(page, 'save-settings')).toBeDisabled();
  await expect(visible(page, 'waiver-order-help')).toContainText('stays there for later runs');
  expect(await overflowingElements(page)).toEqual([]);
});
