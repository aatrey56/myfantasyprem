import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn } from './helpers';

/**
 * A sheet that opens while the page is busy (a slow phone, or a list
 * rendering a page of rows) stays open. Its "panel never laid out" safety
 * net once judged by the panel's onLayout alone: when that report came
 * after 500 ms, a dialog already on screen closed by itself under the
 * user's finger (CI: club-menu, players-hub "Sort works together with View").
 */
test.beforeEach(async ({ page }) => signIn(page));

const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);

for (const [name, width, height] of [
  ['phone', 390, 844],
  ['desktop', 1280, 800],
] as const) {
  test(`the View menu stays open when its layout reports late for a pick (${name})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height });
    // A busy main thread delivers layout reports late: react-native-web's
    // onLayout waits for a ResizeObserver pass and then a timer. Hold every
    // report 1.2 s, past the safety net's 500 ms, as a 2-core CI runner did.
    await page.addInitScript(() => {
      const w = window as unknown as { __heldReports: number };
      w.__heldReports = 0;
      const Native = window.ResizeObserver;
      window.ResizeObserver = class extends Native {
        constructor(callback: ResizeObserverCallback) {
          super((entries, observer) => {
            w.__heldReports += 1;
            setTimeout(() => {
              w.__heldReports -= 1;
              callback(entries, observer);
            }, 1200);
          });
        }
      };
    });
    await page.goto(`${LEAGUE}/players`);
    await visible(page, 'view-menu').click();
    await expect(visible(page, 'view-sheet')).toBeVisible();
    // The sheet's own report has landed (and the safety net's 500 ms passed).
    await expect
      .poll(() =>
        page.evaluate(() => (window as unknown as { __heldReports: number }).__heldReports),
      )
      .toBe(0);
    await expect(visible(page, 'view-sheet')).toBeVisible();
    await visible(page, 'view-pos-MID').click();
    await expect(visible(page, 'view-sheet')).toHaveCount(0);
    await expect(visible(page, 'view-menu')).toHaveText('View: Midfielders');
  });
}
