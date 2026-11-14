import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn } from './helpers';

/**
 * A sheet that opens while the page is busy (a slow phone, or a list
 * rendering a page of rows) stays open and on screen. Its "panel never laid
 * out" safety net once judged by the panel's onLayout alone: when that
 * report came after 500 ms, a dialog already on screen closed by itself
 * under the user's finger (CI: club-menu, players-hub "Sort works together
 * with View"); and a phone sheet whose report never came sat below the
 * screen, an invisible layer over the app.
 */
test.beforeEach(async ({ page }) => signIn(page));

const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);

/** What the page saw of the dialog panel's own layout reports. */
interface Reports {
  /** Reports for the panel taken from react-native-web. */
  held: number;
  /** Reports for the panel handed on to it (late). */
  delivered: number;
  /** When the first one was taken (ms, performance.now). */
  heldAt: number;
  /** When the last one was handed on. */
  deliveredAt: number;
}

/**
 * A busy main thread delivers layout reports late: react-native-web's
 * onLayout waits for a ResizeObserver pass and then a timer. Every report
 * is held 1.2 s, past the safety net's 500 ms, as a 2-core CI runner did;
 * `lost` drops the dialog panel's own instead.
 */
async function slowLayout(page: Page, panel: string, lost: boolean) {
  await page.addInitScript(
    ([id, drop]) => {
      const w = window as unknown as { __reports: Reports };
      w.__reports = { held: 0, delivered: 0, heldAt: 0, deliveredAt: 0 };
      const r = w.__reports;
      const isPanel = (e: ResizeObserverEntry) =>
        e.target instanceof HTMLElement && e.target.dataset.testid === id;
      const Native = window.ResizeObserver;
      window.ResizeObserver = class extends Native {
        constructor(callback: ResizeObserverCallback) {
          super((all, observer) => {
            const mine = all.some(isPanel);
            if (mine) {
              r.held += 1;
              if (r.heldAt === 0) r.heldAt = performance.now();
            }
            const entries = drop ? all.filter((e) => !isPanel(e)) : all;
            setTimeout(() => {
              if (mine && !drop) {
                r.delivered += 1;
                r.deliveredAt = performance.now();
              }
              if (entries.length > 0) callback(entries, observer);
            }, 1200);
          });
        }
      };
    },
    [panel, lost] as const,
  );
}

const reports = (page: Page) =>
  page.evaluate(() => (window as unknown as { __reports: Reports }).__reports);

/** The panel's report was taken and handed on, well after the 500 ms deadline. */
async function reportLandedLate(page: Page) {
  await expect
    .poll(async () => {
      const r = await reports(page);
      return r.held > 0 && r.delivered > 0 && r.deliveredAt - r.heldAt > 1000;
    })
    .toBe(true);
}

/** The panel's report was taken and dropped, and the deadline long passed. */
async function reportLost(page: Page) {
  await expect
    .poll(async () => {
      const r = await reports(page);
      const now = await page.evaluate(() => performance.now());
      return r.held > 0 && r.delivered === 0 && now - r.heldAt > 1200;
    })
    .toBe(true);
}

for (const [name, width, height] of [
  ['phone', 390, 844],
  ['desktop', 1280, 800],
] as const) {
  test(`the View menu stays open when its layout reports late for a pick (${name})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height });
    await slowLayout(page, 'view-sheet', false);
    await page.goto(`${LEAGUE}/players`);
    await visible(page, 'view-menu').click();
    await expect(visible(page, 'view-sheet')).toBeVisible();
    await reportLandedLate(page);
    await expect(visible(page, 'view-sheet')).toBeVisible();
    await visible(page, 'view-pos-MID').click();
    await expect(visible(page, 'view-sheet')).toHaveCount(0);
    await expect(visible(page, 'view-menu')).toHaveText('View: Midfielders');
  });
}

for (const lost of [false, true]) {
  test(`a phone bottom sheet whose layout report is ${lost ? 'lost' : 'late'} slides on screen and works`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await slowLayout(page, 'player-card', lost);
    await page.goto(`${LEAGUE}/players`);
    await page.locator('[data-testid^="player-plr_"]:visible:not([inert] *)').first().click();
    const card = visible(page, 'player-card');
    await expect(card).toHaveCount(1);
    await (lost ? reportLost(page) : reportLandedLate(page));
    // On screen, not left translated below it.
    await expect
      .poll(async () => {
        const box = await card.boundingBox();
        return box !== null && box.y >= 0 && box.y < 844 / 2 && box.y + box.height <= 844 + 1;
      })
      .toBe(true);
    await card.getByTestId('card-tab-log').click();
    await expect(card.getByTestId('card-log')).toBeVisible();
    await visible(page, 'sheet-close').first().click();
    await expect(card).toHaveCount(0);
  });
}
