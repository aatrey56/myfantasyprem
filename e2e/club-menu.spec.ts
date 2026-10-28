import { expect, test, type Locator, type Page } from './fixtures';

import { LEAGUE, signIn } from './helpers';

// Every Sheet is a centred dialog from 768px up and a (centred) bottom sheet on
// phones. Runs on chromium and webkit (see playwright.config.ts).
test.beforeEach(async ({ page }) => signIn(page));

const visible = (page: Page, id: string) => page.locator(`[data-testid="${id}"]:visible`);

async function expectCentred(menu: Locator, width: number, height: number, vertical: boolean) {
  const box = (await menu.boundingBox())!;
  expect(Math.abs(box.x + box.width / 2 - width / 2)).toBeLessThanOrEqual(24);
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(width + 1);
  expect(box.height).toBeLessThanOrEqual(height * 0.9 + 1);
  if (vertical) expect(Math.abs(box.y + box.height / 2 - height / 2)).toBeLessThanOrEqual(60);
}

for (const [width, height] of [
  [375, 800],
  [1280, 800],
] as const) {
  test(`the View menu opens centred at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height });
    await page.goto(`${LEAGUE}/players`);
    const trigger = page.getByTestId('view-menu');
    await trigger.click();
    const menu = page.getByTestId('view-sheet');
    await expect(menu).toBeVisible();
    await expect(menu).toHaveAttribute('role', 'dialog');
    await expect(page.getByTestId('view-all')).toBeVisible();
    await expectCentred(menu, width, height, width > 700);
    const row = (await page.getByTestId('view-all').boundingBox())!;
    expect(row.height).toBeGreaterThanOrEqual(44);
    expect(
      await page.evaluate(() =>
        document.querySelector('[data-testid="view-sheet"]')?.contains(document.activeElement),
      ),
    ).toBe(true);
    await page.keyboard.press('Escape');
    await expect(menu).toBeHidden();
    await expect(trigger).toBeFocused();
  });
}

test('the player card opens centred at 1280px and closes on the backdrop', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto(`${LEAGUE}/players`);
  await page.locator('[data-testid^="player-plr_"]:visible').first().click();
  const card = visible(page, 'player-card');
  await expect(card).toBeVisible();
  await expectCentred(card, 1280, 800, true);
  expect((await card.boundingBox())!.width).toBeLessThanOrEqual(881);
  await page.mouse.click(10, 10);
  await expect(card).toHaveCount(0);
});

test('the points breakdown opens centred at 1280px', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto(`${LEAGUE}/matchup`);
  await expect(visible(page, 'h2h-starters')).toBeVisible();
  await page.locator('[data-testid^="h2h-breakdown-"]:visible').first().click();
  const sheet = visible(page, 'points-breakdown');
  await expect(sheet).toBeVisible();
  await expectCentred(sheet, 1280, 800, true);
  await page.keyboard.press('Escape');
  await expect(sheet).toHaveCount(0);
});
