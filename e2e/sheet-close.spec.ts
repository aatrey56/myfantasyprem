import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn } from './helpers';

/**
 * The Players hub's View and sort pop-ups are centred dialogs on
 * phones too (owner, 2026-10-24): a pick applies at once and closes them.
 */
test.beforeEach(async ({ page }) => signIn(page));
test.use({ viewport: { width: 390, height: 844 } });

const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);

/** The sheet's top edge, every frame, until it leaves the page. */
async function trackTop(page: Page, testId: string) {
  await page.evaluate((id) => {
    const w = window as unknown as { __tops: number[] };
    w.__tops = [];
    const tick = () => {
      const el = document.querySelector(`[data-testid="${id}"]`);
      if (!el) return;
      w.__tops.push(el.getBoundingClientRect().top);
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }, testId);
  return () => page.evaluate(() => (window as unknown as { __tops: number[] }).__tops);
}

/** Opens the View sheet and waits for its slide up to finish; its resting top. */
async function openView(page: Page) {
  await page.goto(`${LEAGUE}/players`);
  await visible(page, 'view-menu').click();
  const sheet = visible(page, 'view-sheet');
  await expect(sheet).toBeVisible();
  let last = -1;
  await expect
    .poll(async () => {
      const top = (await sheet.boundingBox())!.y;
      const still = top === last;
      last = top;
      return still;
    })
    .toBe(true);
  return last;
}

/** A centred dialog on the phone: middle of the screen, a margin on each side. */
async function expectCentred(page: Page, testId: string) {
  const box = (await visible(page, testId).boundingBox())!;
  expect(box.x).toBeGreaterThan(4);
  expect(Math.abs(box.x + box.width / 2 - 195)).toBeLessThan(2);
  expect(Math.abs(box.y + box.height / 2 - 844 / 2)).toBeLessThan(40);
}

test('the View pop-up is a centred dialog; a pick closes it and applies the view', async ({
  page,
}) => {
  await openView(page);
  await expectCentred(page, 'view-sheet');
  await visible(page, 'view-pos-DEF').click();
  await expect(visible(page, 'view-sheet')).toHaveCount(0);
  await expect(visible(page, 'view-menu')).toHaveAccessibleName('View: Defenders. Change');
  // A tap on the dimmed backdrop closes it too.
  await visible(page, 'view-menu').click();
  await expect(visible(page, 'view-sheet')).toBeVisible();
  await page.mouse.click(195, 20);
  await expect(visible(page, 'view-sheet')).toHaveCount(0);
});

test('with reduced motion a pick closes the sheet at once', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  const rest = await openView(page);
  const tops = await trackTop(page, 'view-sheet');
  await visible(page, 'view-pos-MID').click();
  await expect(visible(page, 'view-sheet')).toHaveCount(0);
  expect((await tops()).every((t) => Math.abs(t - rest) < 2)).toBe(true);
  await expect(visible(page, 'view-menu')).toHaveAccessibleName('View: Midfielders. Change');
});

test('the sort pop-up is a centred dialog too, closed by a pick', async ({ page }) => {
  await page.goto(`${LEAGUE}/players`);
  for (const [opener, sheet, option] of [
    ['sort-menu', 'sort-sheet', 'sort-total_points'],
  ] as const) {
    await visible(page, opener).click();
    await expect(visible(page, sheet)).toBeVisible();
    await page.waitForTimeout(400);
    await expectCentred(page, sheet);
    await visible(page, sheet).getByTestId(option).click();
    await expect(visible(page, sheet)).toHaveCount(0);
  }
});
