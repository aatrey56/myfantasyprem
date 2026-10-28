import { expect, test, type Page } from './fixtures';

import { expectLeagueTab, LEAGUE, overflowingElements, signIn, startWithScenario } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

const VIEWPORTS = [
  { name: 'phone 375x812', width: 375, height: 812, nav: 'league-tabs', otherNav: 'top-nav' },
  { name: 'desktop 1280x800', width: 1280, height: 800, nav: 'top-nav', otherNav: 'league-tabs' },
] as const;

for (const vp of VIEWPORTS) {
  test.describe(vp.name, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('league home loads from mocks', async ({ page }) => {
      await page.goto(`${LEAGUE}/league`);
      await expectLeagueTab(page);
      await expect(page.getByTestId('loaded')).toBeVisible();
    });

    test('has no horizontal scroll', async ({ page }) => {
      await page.goto(LEAGUE);
      await expect(page.getByTestId('status-loaded')).toBeVisible();
      const widths = await page.evaluate(() => ({
        doc: document.documentElement.scrollWidth,
        body: document.body.scrollWidth,
        inner: window.innerWidth,
      }));
      expect(widths.doc).toBeLessThanOrEqual(widths.inner);
      expect(widths.body).toBeLessThanOrEqual(widths.inner);
      expect(await overflowingElements(page)).toEqual([]);
    });

    test(`shows ${vp.nav} as a navigation landmark and not ${vp.otherNav}`, async ({ page }) => {
      await page.goto(LEAGUE);
      await expect(page.getByTestId(vp.nav)).toBeVisible();
      await expect(page.getByTestId(vp.otherNav)).toHaveCount(0);
      const nav = page.getByRole('navigation', { name: 'League sections' });
      await expect(nav).toBeVisible();
      for (const label of ['Status', 'Match', 'Team', 'Players', 'League']) {
        await expect(nav.getByText(label, { exact: true })).toBeVisible();
      }
    });

    test('shows no disclaimer footer and no freshness banner', async ({ page }) => {
      await page.goto(LEAGUE);
      await expect(page.getByTestId('status-loaded')).toBeVisible();
      // The in-app disclaimer was removed at the owner's request (AGENTS.md).
      await expect(page.getByText(/Not affiliated|independent fantasy game/)).toHaveCount(0);
      await expect(page.getByTestId('footer')).toHaveCount(0);
      for (const id of ['stale-banner', 'unreachable-banner', 'offline-banner']) {
        await expect(page.getByTestId(id)).toHaveCount(0);
      }
    });

    test('tab navigation reaches a live matchup between two teams', async ({ page }) => {
      await page.goto(LEAGUE);
      await page.getByTestId(vp.nav).getByText('Match', { exact: true }).click();
      await expect(page).toHaveURL(/\/matchup$/);
      // My matchup opens by default: two real teams head to head.
      await expect(page.locator('[data-testid="score-header"]:visible')).toBeVisible();
      await expect(page.locator('[data-testid="score-state"]:visible')).toHaveText('Live');
      await expect(page.locator('[data-testid="h2h-starters"]:visible')).toBeVisible();
      // Another matchup from the gameweek list, without the league average:
      // the header carousel moves to it in place.
      await page
        .locator('[data-testid^="matchup-row-"]:visible')
        .filter({ hasNotText: 'League average' })
        .nth(1)
        .click();
      await expect(page).toHaveURL(/\/matchup$/);
      await expect(page.locator('[data-testid="carousel-dot-0"]:visible')).not.toHaveAttribute(
        'aria-current',
        'true',
      );
      await expect(page.locator('[data-testid="score-left-points"]:visible')).toHaveText(/^\d+$/);
      await expect(page.locator('[data-testid="h2h-lineups"]:visible')).toBeVisible();
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('the league-average matchup shows a score and no lineup', async ({ page }) => {
      await page.goto(`${LEAGUE}/matchup`);
      await page
        .locator('[data-testid^="matchup-row-"]:visible')
        .filter({ hasText: 'League average' })
        .click();
      const average = page.locator('[data-testid="average-opponent"]:visible');
      await expect(average).toBeVisible();
      await expect(average).toContainText('League average');
      await expect(page.locator('[data-testid="average-points"]:visible')).toHaveText(/^\d+$/);
      await expect(page.locator('[data-testid="team-lineup"]:visible')).toHaveCount(1);
      await expect(page.locator('[data-testid="h2h-lineups"]:visible')).toHaveCount(0);
    });
  });
}

test('my team lists next-gameweek fixtures for the whole squad', async ({ page }) => {
  await page.goto(`${LEAGUE}/team`);
  await page.getByTestId('view-list').click();
  // Tabs keep earlier screens mounted (hidden), so only look at visible rows.
  const chips = page.locator('[data-testid="next-fixtures"]:visible');
  await expect(chips).toHaveCount(15);
  for (const text of await chips.allTextContents()) {
    // One fixture, two in a double gameweek ("v ABC Sat 15:00@ DEF Sun 14:00"), or a blank.
    // Kickoffs six days or more away add the date: "Tue 20 Oct, 16:30". A fixture
    // still to play ends with its difficulty rank: "(18th)" (ADR 0014).
    expect(text).toMatch(
      /^((v|@) [A-Z]{3}(\w{3} (\d{1,2} \w{3}, )?\d\d:\d\d(\(\d{1,2}(st|nd|rd|th)\))?|LIVE|FT|PP))+$|^No fixture$/,
    );
  }
});

/** The horizontal league nav from 768px: tablet and desktop. */
for (const width of [768, 1280]) {
  test.describe(`top nav at ${width}px`, () => {
    test.use({ viewport: { width, height: 800 } });

    const nav = (page: Page) => page.getByRole('navigation', { name: 'League sections' });

    test('is one horizontal bar: switcher at the left, then the tabs and settings', async ({
      page,
    }) => {
      await page.goto(LEAGUE);
      await expect(page.getByTestId('status-loaded')).toBeVisible();
      await expect(page.getByTestId('top-nav')).toBeVisible();
      // No left sidebar, icon rail or bottom bar.
      for (const id of ['sidebar', 'bottom-tabs', 'league-tabs'])
        await expect(page.getByTestId(id)).toHaveCount(0);
      const bar = (await page.getByTestId('top-nav').boundingBox())!;
      expect(bar.y).toBe(0);
      expect(bar.width).toBe(width);
      expect(bar.height).toBeLessThanOrEqual(64);
      const switcher = (await page.getByTestId('league-switcher-button').boundingBox())!;
      const tabs = nav(page).getByRole('tab');
      await expect(tabs).toHaveText(['Status', 'Match', 'Team', 'Players', 'League']);
      let lastX = switcher.x + switcher.width;
      for (const tab of await tabs.all()) {
        const box = (await tab.boundingBox())!;
        expect(box.x).toBeGreaterThanOrEqual(lastX - 1);
        expect(box.y).toBeGreaterThanOrEqual(bar.y);
        expect(box.y + box.height).toBeLessThanOrEqual(bar.y + bar.height + 1);
        expect(box.height).toBeGreaterThanOrEqual(44);
        lastX = box.x + box.width;
      }
      const settings = (await page.getByTestId('nav-settings').boundingBox())!;
      expect(settings.x).toBeGreaterThan(lastX);
      expect(settings.height).toBeGreaterThanOrEqual(44);
      expect(settings.width).toBeGreaterThanOrEqual(44);
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('marks the active tab and follows navigation', async ({ page }) => {
      await page.goto(LEAGUE);
      const league = nav(page).getByRole('tab', { name: 'Status' });
      await expect(league).toHaveAttribute('aria-selected', 'true');
      const underline = () => league.evaluate((el) => getComputedStyle(el).borderBottomColor);
      const team = nav(page).getByRole('tab', { name: 'Team' });
      const inactive = await team.evaluate((el) => getComputedStyle(el).borderBottomColor);
      expect(await underline()).not.toBe(inactive);
      await team.click();
      await expect(page).toHaveURL(/\/team$/);
      await expect(team).toHaveAttribute('aria-selected', 'true');
      await expect(league).toHaveAttribute('aria-selected', 'false');
    });

    test('stays at the top while the page scrolls', async ({ page }) => {
      await page.goto(`${LEAGUE}/league`);
      const top = page.getByTestId('league-matchups-header');
      await expect(top).toBeVisible();
      await page.mouse.move(width / 2, 500);
      await page.mouse.wheel(0, 2000);
      await expect.poll(async () => (await top.boundingBox())?.y ?? 0).toBeLessThan(0);
      expect((await page.getByTestId('top-nav').boundingBox())!.y).toBe(0);
      await expect(nav(page).getByRole('tab', { name: 'Players' })).toBeInViewport();
    });

    test('is reachable with the keyboard', async ({ page }) => {
      await page.goto(LEAGUE);
      await expect(page.getByTestId('status-loaded')).toBeVisible();
      const reached: string[] = [];
      for (let i = 0; i < 10; i++) {
        await page.keyboard.press('Tab');
        reached.push(
          await page.evaluate(
            () =>
              document.activeElement?.getAttribute('aria-label') ??
              document.activeElement?.textContent ??
              '',
          ),
        );
      }
      for (const label of ['Match', 'Team', 'Players', 'League']) {
        expect(reached).toContain(label);
      }
    });
  });
}

test('a league whose draft ended after the season says so', async ({ page }) => {
  await startWithScenario(page, 'league-season-over');
  await page.goto(LEAGUE);
  await expect(page.getByTestId('season-over')).toContainText('Season over — no gameweeks left');
  await expect(page.getByTestId('my-matchup')).toHaveCount(0);
});
