import { expect, test, type Page } from './fixtures';

import { LEAGUE, LEAGUE_NAME, overflowingElements, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

async function boxes(page: Page) {
  const gw = await page.getByTestId('top-bar-gw').boundingBox();
  const settings = await page.getByTestId('top-bar-settings').boundingBox();
  const switcher = await page.getByTestId('league-switcher-button').boundingBox();
  const back = await page.getByTestId('back-to-leagues').boundingBox();
  const bar = await page.getByTestId('league-top-bar').boundingBox();
  if (!gw || !settings || !switcher || !back || !bar) throw new Error('top bar parts missing');
  return { gw, settings, switcher, back, bar };
}

const overlaps = (
  a: { x: number; y: number; width: number; height: number },
  b: { x: number; y: number; width: number; height: number },
) => a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;

for (const width of [320, 375, 767]) {
  test.describe(`top bar at ${width}px`, () => {
    test.use({ viewport: { width, height: 800 } });

    test('GW badge, switcher and settings do not overlap and stay inside the bar', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/team`);
      await expect(page.getByTestId('top-bar-gw')).toBeVisible();
      const b = await boxes(page);
      expect(overlaps(b.gw, b.settings)).toBe(false);
      expect(overlaps(b.gw, b.switcher)).toBe(false);
      expect(overlaps(b.back, b.switcher)).toBe(false);
      // The badge sits fully inside the bar, centred on the settings button.
      expect(b.gw.y).toBeGreaterThanOrEqual(b.bar.y);
      expect(b.gw.y + b.gw.height).toBeLessThanOrEqual(b.bar.y + b.bar.height);
      const gwMid = b.gw.y + b.gw.height / 2;
      const setMid = b.settings.y + b.settings.height / 2;
      expect(Math.abs(gwMid - setMid)).toBeLessThanOrEqual(2);
      expect(await overflowingElements(page)).toEqual([]);
    });
  });
}

test.describe('top bar with a long league name', () => {
  test.use({ viewport: { width: 320, height: 800 } });

  test('the switcher truncates instead of pushing the badge', async ({ page }) => {
    await page.goto(`${LEAGUE}/team`);
    await expect(page.getByTestId('top-bar-gw')).toBeVisible();
    const LONG = 'An extremely long league name that cannot possibly fit';
    const label = page.getByTestId('league-switcher-name');
    await expect(label).toHaveText(LEAGUE_NAME);
    await label.evaluate((el, name) => {
      el.textContent = name;
    }, LONG);
    await expect(label).toHaveText(LONG);
    const b = await boxes(page);
    // The label is clipped to the switcher rather than widening it.
    const nameBox = (await label.boundingBox())!;
    expect(nameBox.x + nameBox.width).toBeLessThanOrEqual(b.switcher.x + b.switcher.width);
    expect(overlaps(b.gw, b.switcher)).toBe(false);
    expect(overlaps(b.gw, b.settings)).toBe(false);
    expect(b.settings.x + b.settings.width).toBeLessThanOrEqual(320);
  });
});

test.describe('deadlines', () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test('shows the next deadline with a live countdown, and collapses past gameweeks', async ({
    page,
  }) => {
    await page.clock.install();
    await page.goto(`${LEAGUE}/activity/deadlines`);
    await expect(page.getByTestId('deadline-next')).toBeVisible();
    await expect(page.getByTestId('deadline-next')).toContainText('NEXT · GAMEWEEK');
    const timer = page.getByRole('timer');
    await expect(timer).toHaveAccessibleName(/ for Gameweek \d+ in /);
    // The visible clock follows the clock: two hours later it reads two hours less.
    const clock = page.getByTestId('countdown').locator('div').first();
    const before = await clock.textContent();
    await page.clock.fastForward('02:00:00');
    await expect(clock).not.toHaveText(before ?? '');
    await expect(page.getByTestId('deadlines-zone')).toContainText('time zone');
    // Three milestones per upcoming gameweek.
    for (const key of ['trades', 'waivers', 'lock']) {
      await expect(page.locator(`[data-testid$="-${key}"]`).first()).toBeVisible();
    }
    // The current gameweek's lock has passed: collapsed under "Past".
    const toggle = page.getByTestId('past-toggle');
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await expect(page.getByText(/^Passed/)).toHaveCount(0);
    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');
    // "Passed · 3h ago" within a day, then just "Passed" next to the date.
    await expect(page.getByText(/^Passed( · (just now|\d+[mh] ago))?$/).first()).toBeVisible();
    expect(await overflowingElements(page)).toEqual([]);
  });

  test('is reachable from the activity path and leads back to Activity', async ({ page }) => {
    // Activity (and its Deadlines) open over the league tabs, from League.
    await page.goto(`${LEAGUE}/activity/deadlines`);
    await expect(page.getByRole('heading', { name: 'Deadlines' })).toBeVisible();
    await expect(page.getByTestId('back-link')).toContainText('Activity');
  });
});

test.describe('web metadata', () => {
  test('manifest, theme colour and icons are served', async ({ page, request }) => {
    await page.goto('/');
    const href = await page.locator('link[rel="manifest"]').getAttribute('href');
    const res = await request.get(href as string);
    expect(res.ok()).toBe(true);
    const m = await res.json();
    expect(m.name).toBe('MyFantasyPrem');
    expect(m.theme_color).toBe('#0E1013');
    for (const icon of m.icons as { src: string }[]) {
      expect((await request.get(icon.src)).ok()).toBe(true);
    }
    const touch = await page.locator('link[rel="apple-touch-icon"]').getAttribute('href');
    expect((await request.get(touch as string)).ok()).toBe(true);
    await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute('content', '#0E1013');
  });
});

test.describe('dark only, on a device set to light', () => {
  test.use({ colorScheme: 'light', viewport: { width: 390, height: 844 } });

  test('the page and the app stay on the dark background', async ({ page }) => {
    await page.goto(LEAGUE);
    await expect(page.getByTestId('league-switcher-button')).toContainText(LEAGUE_NAME);
    await expect(page.locator('meta[name="color-scheme"]')).toHaveAttribute('content', 'dark');
    const backgrounds = await page.evaluate(() => {
      const bg = (el: Element) => getComputedStyle(el).backgroundColor;
      // The first painted layer inside #root: the root layout's view.
      const app = [...document.querySelectorAll('#root *')].find(
        (el) => bg(el) !== 'rgba(0, 0, 0, 0)',
      );
      return { body: bg(document.body), app: app ? bg(app) : null };
    });
    // #0B0D10, darkColors.background.
    expect(backgrounds).toEqual({ body: 'rgb(11, 13, 16)', app: 'rgb(11, 13, 16)' });
  });
});
