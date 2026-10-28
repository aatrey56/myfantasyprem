import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

/** The underline's centre and width, and each tab's centre, in px. */
async function geometry(page: Page) {
  const bar = page.getByTestId('league-tabs');
  const line = (await bar.getByTestId('league-tabs-indicator').boundingBox())!;
  const tabs = await Promise.all(
    ['index', 'matchup', 'team', 'players', 'league'].map(async (name) => {
      const b = (await bar.getByTestId(`tab-${name}`).boundingBox())!;
      return b.x + b.width / 2;
    }),
  );
  return { centre: line.x + line.width / 2, width: line.width, tabs };
}

test.describe('tab underline (touch, 390px)', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test('follows the finger part-way through a swipe, then rests under the new tab', async ({
    page,
    browserName,
  }) => {
    test.skip(browserName !== 'chromium', 'touch input through CDP');
    await page.goto(LEAGUE);
    await expect(page.getByTestId('status-loaded')).toBeVisible();
    const start = await geometry(page);
    expect(Math.abs(start.centre - start.tabs[0]!)).toBeLessThan(2);

    const cdp = await page.context().newCDPSession(page);
    const y = 500;
    await cdp.send('Input.dispatchTouchEvent', {
      type: 'touchStart',
      touchPoints: [{ x: 330, y }],
    });
    for (let i = 1; i <= 8; i++) {
      await cdp.send('Input.dispatchTouchEvent', {
        type: 'touchMove',
        touchPoints: [{ x: 330 - (150 * i) / 8, y }],
      });
      await page.waitForTimeout(16);
    }
    // Mid-swipe (finger still down, 150 of 390px): between STATUS and MATCH.
    const mid = await geometry(page);
    expect(mid.centre).toBeGreaterThan(start.tabs[0]! + 10);
    expect(mid.centre).toBeLessThan(start.tabs[1]! - 10);
    // Past half the page before lifting the finger: whether 150px commits
    // depends on the fling speed, which CDP events spaced out by a loaded
    // machine do not reach, and the pager then snaps back to STATUS.
    for (let i = 1; i <= 4; i++) {
      await cdp.send('Input.dispatchTouchEvent', {
        type: 'touchMove',
        touchPoints: [{ x: 180 - (100 * i) / 4, y }],
      });
      await page.waitForTimeout(16);
    }
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    await expect
      .poll(async () => Math.abs((await geometry(page)).centre - start.tabs[1]!))
      .toBeLessThan(2);
  });

  test('slides under a tapped tab', async ({ page }) => {
    await page.goto(LEAGUE);
    await expect(page.getByTestId('status-loaded')).toBeVisible();
    await page.getByTestId('league-tabs').getByTestId('tab-players').tap();
    const { tabs } = await geometry(page);
    await expect
      .poll(async () => Math.abs((await geometry(page)).centre - tabs[3]!))
      .toBeLessThan(2);
  });

  test('with reduced motion, jumps under a tapped tab instead of sliding', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto(LEAGUE);
    await expect(page.getByTestId('status-loaded')).toBeVisible();
    const { tabs } = await geometry(page);
    await page.getByTestId('league-tabs').getByTestId('tab-league').tap();
    // Never caught part-way between STATUS and LEAGUE.
    for (let i = 0; i < 6; i++) {
      const { centre } = await geometry(page);
      expect(Math.abs(centre - tabs[0]!) < 2 || Math.abs(centre - tabs[4]!) < 2).toBe(true);
      await page.waitForTimeout(30);
    }
    await expect
      .poll(async () => Math.abs((await geometry(page)).centre - tabs[4]!))
      .toBeLessThan(2);
  });
});
