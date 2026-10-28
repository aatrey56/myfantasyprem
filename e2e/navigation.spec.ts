import { expect, test, type Page } from './fixtures';

import { LEAGUE, overflowingElements, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

const TABS = [
  { label: 'Status', path: '' },
  { label: 'Match', path: '/matchup' },
  { label: 'Team', path: '/team' },
  { label: 'Players', path: '/players' },
  { label: 'League', path: '/league' },
] as const;

const leagueNav = (page: Page) => page.getByRole('navigation', { name: 'League sections' });
const appBar = (page: Page) => page.getByTestId('app-tabs');
const urlFor = (path: string) => new RegExp(`${LEAGUE}${path.replace('/', '\\/')}$`);

for (const vp of [
  { name: 'phone 390', width: 390, height: 844, nav: 'league-tabs' },
  { name: 'desktop 1280', width: 1280, height: 800, nav: 'top-nav' },
] as const) {
  test.describe(`league tabs and app bar (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('the top tabs show STATUS · MATCH · TEAM · PLAYERS · LEAGUE, Status selected', async ({
      page,
    }) => {
      await page.goto(LEAGUE);
      await expect(page.getByTestId(vp.nav)).toBeVisible();
      const tabs = leagueNav(page).getByRole('tab');
      await expect(tabs).toHaveText(TABS.map((t) => t.label));
      // Uppercase on screen, like Sleeper.
      const transform = await tabs.first().evaluate((el) => {
        const label = [...el.querySelectorAll('*')].find(
          (n) => n.childElementCount === 0 && n.textContent === 'Status',
        );
        return label ? getComputedStyle(label).textTransform : null;
      });
      expect(transform).toBe('uppercase');
      await expect(tabs.first()).toHaveAttribute('aria-selected', 'true');
      // The tabs sit under the header, at the top of the screen.
      const nav = (await page.getByTestId(vp.nav).boundingBox())!;
      expect(nav.y).toBeLessThan(120);
      // They fit: every tab inside the viewport, nothing pokes out.
      for (const tab of await tabs.all()) {
        const box = (await tab.boundingBox())!;
        expect(box.x).toBeGreaterThanOrEqual(0);
        expect(box.x + box.width).toBeLessThanOrEqual(vp.width);
        expect(box.height).toBeGreaterThanOrEqual(44);
      }
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('clicking a tab changes the tab and the URL', async ({ page }) => {
      await page.goto(LEAGUE);
      await expect(page.getByTestId('status-loaded')).toBeVisible();
      for (const tab of [...TABS.slice(1), TABS[0]]) {
        await leagueNav(page).getByRole('tab', { name: tab.label }).click();
        await expect(page).toHaveURL(urlFor(tab.path));
        await expect(leagueNav(page).getByRole('tab', { name: tab.label })).toHaveAttribute(
          'aria-selected',
          'true',
        );
        await expect(leagueNav(page).getByRole('tab', { selected: true })).toHaveCount(1);
      }
    });

    for (const tab of TABS.slice(1)) {
      test(`a deep link to ${tab.path} opens ${tab.label}`, async ({ page }) => {
        await page.goto(`${LEAGUE}${tab.path}`);
        await expect(leagueNav(page).getByRole('tab', { name: tab.label })).toHaveAttribute(
          'aria-selected',
          'true',
        );
        await expect(page).toHaveURL(urlFor(tab.path));
      });
    }

    test('a deep link inside a tab opens that tab', async ({ page }) => {
      await page.goto(`${LEAGUE}/players/waivers`);
      await expect(leagueNav(page).getByRole('tab', { name: 'Players' })).toHaveAttribute(
        'aria-selected',
        'true',
      );
      // Pressing the tab you are on goes back to its first screen.
      await leagueNav(page).getByRole('tab', { name: 'Players' }).click();
      await expect(page).toHaveURL(urlFor('/players'));
    });

    test('the app bar stays under a league and switches sections', async ({ page }) => {
      await page.goto(`${LEAGUE}/team`);
      const bar = appBar(page);
      await expect(bar).toBeVisible();
      // FEED carries the inbox's unread count as a badge.
      await expect(bar.getByRole('tab')).toHaveText(['Leagues', 'Scores', /Feed/, 'Account']);
      await expect(bar.getByRole('tab', { name: 'Leagues' })).toHaveAttribute(
        'aria-selected',
        'true',
      );
      // Flush with the bottom of the screen, under the league's pages.
      const box = (await bar.boundingBox())!;
      expect(Math.round(box.y + box.height)).toBe(vp.height);

      await bar.getByRole('tab', { name: 'Scores' }).click();
      await expect(page).toHaveURL(/\/scores$/);
      await expect(page.getByTestId('scores-list')).toBeVisible();
      await expect(page.getByTestId('fixture-live').first()).toBeVisible();

      await bar.getByRole('tab', { name: 'Feed' }).click();
      await expect(page).toHaveURL(/\/feed$/);
      await expect(page.getByTestId('feed-list')).toBeVisible();

      await bar.getByRole('tab', { name: 'Account' }).click();
      await expect(page).toHaveURL(/\/account$/);

      // Back to Leagues: the league, on the tab it was left on.
      await bar.getByRole('tab', { name: 'Leagues' }).click();
      await expect(page).toHaveURL(urlFor('/team'));
      await expect(leagueNav(page).getByRole('tab', { name: 'Team' })).toHaveAttribute(
        'aria-selected',
        'true',
      );
      // Again: the hub.
      await bar.getByRole('tab', { name: 'Leagues' }).click();
      await expect(page).toHaveURL(/\/leagues$/);
      await expect(page.getByTestId('league-list')).toBeVisible();
    });

    test('Scores steps through gameweeks', async ({ page }) => {
      await page.goto('/scores');
      await expect(page.getByTestId('gw-label')).toHaveText('GW 7');
      await page.getByTestId('gw-prev').click();
      await expect(page.getByTestId('gw-label')).toHaveText('GW 6');
      await expect(page.getByTestId('fixture-status').first()).toHaveText('FT');
      expect(await overflowingElements(page)).toEqual([]);
    });
  });
}

test.describe('touch screen (390px)', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  /** A finger drag across the pager, through Chromium's touch input. */
  async function swipe(page: Page, fromX: number, toX: number, y = 500) {
    const cdp = await page.context().newCDPSession(page);
    const steps = 8;
    await cdp.send('Input.dispatchTouchEvent', {
      type: 'touchStart',
      touchPoints: [{ x: fromX, y }],
    });
    for (let i = 1; i <= steps; i++) {
      await cdp.send('Input.dispatchTouchEvent', {
        type: 'touchMove',
        touchPoints: [{ x: fromX + ((toX - fromX) * i) / steps, y }],
      });
      await page.waitForTimeout(16);
    }
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
  }

  test('swiping left and right pages between tabs', async ({ page, browserName }) => {
    test.skip(browserName !== 'chromium', 'touch input through CDP');
    await page.goto(LEAGUE);
    await expect(page.getByTestId('status-loaded')).toBeVisible();
    await swipe(page, 330, 40);
    await expect(page).toHaveURL(urlFor('/matchup'));
    await expect(leagueNav(page).getByRole('tab', { name: 'Match' })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    await swipe(page, 330, 40);
    await expect(page).toHaveURL(urlFor('/team'));
    await swipe(page, 40, 330);
    await expect(page).toHaveURL(urlFor('/matchup'));
    // A vertical drag scrolls the page instead.
    await swipe(page, 200, 210, 600);
    await expect(page).toHaveURL(urlFor('/matchup'));
  });
});
