import { expect, test, type Page } from './fixtures';

import { expectLeagueTab, LEAGUE, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) => page.locator(`[data-testid="${testId}"]:visible`);

const PAGES = [
  {
    link: 'waivers',
    title: 'Waivers',
    fromTab: 'players',
    tabLink: 'open-waivers',
    tab: 'Players',
  },
  {
    link: 'deadlines',
    title: 'Deadlines',
    fromTab: 'activity',
    tabLink: 'open-deadlines',
    tab: 'Activity',
  },
] as const;

for (const vp of [
  { name: 'phone', width: 375, height: 812 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`back links (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('Activity opened from League goes back to League', async ({ page }) => {
      await page.goto(`${LEAGUE}/league`);
      await visible(page, 'link-activity').click();
      await expect(page.getByRole('heading', { name: 'Activity' })).toBeVisible();
      const back = visible(page, 'back-link');
      await expect(back).toHaveText('League');
      await back.click();
      await expect(page).toHaveURL(new RegExp(`${LEAGUE}/league$`));
      await expectLeagueTab(page);
    });

    for (const p of PAGES) {
      // The League tab no longer has a Trades / Waivers / Deadlines button row
      // (Sleeper's layout); a link that says it came from League still leads back.
      test(`${p.title} linked from League goes back to League`, async ({ page }) => {
        const path = p.link === 'deadlines' ? 'activity/deadlines' : `players/${p.link}`;
        await page.goto(`${LEAGUE}/${path}?from=league`);
        await expect(page.getByRole('heading', { name: p.title })).toBeVisible();
        const back = visible(page, 'back-link');
        await expect(back).toHaveText('League');
        await back.click();
        await expect(page).toHaveURL(new RegExp(`${LEAGUE}/league$`));
        await expectLeagueTab(page);
      });

      test(`${p.title} opened from ${p.tab} goes back to ${p.tab}`, async ({ page }) => {
        await page.goto(`${LEAGUE}/${p.fromTab}`);
        await visible(page, p.tabLink).click();
        await expect(page.getByRole('heading', { name: p.title })).toBeVisible();
        const back = visible(page, 'back-link');
        await expect(back).toHaveText(p.tab);
        await back.click();
        await expect(page).toHaveURL(new RegExp(`${LEAGUE}/${p.fromTab}$`));
      });
    }

    // Trades is a view of the Players tab (SLEEPER-OBSERVED): its nav card
    // leads around the tab and the top tabs lead out, so it has neither a back
    // link nor a page title, even when a link says it came from League.
    test('Trades opened from Players sits in the Players tab without a back link', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/players`);
      await visible(page, 'open-trades').click();
      await expect(visible(page, 'trades-title')).toHaveText('Active Trades');
      await expect(visible(page, 'back-link')).toHaveCount(0);
      await expect(page.getByRole('heading', { name: 'Trades', exact: true })).toHaveCount(0);
      await expect(visible(page, 'open-trades')).toHaveAttribute('aria-current', 'page');
      await visible(page, 'open-available').click();
      await expect(page).toHaveURL(new RegExp(`${LEAGUE}/players(\\?view=available)?$`));
      await expect(visible(page, 'players-scope-title')).toHaveText('Available');
    });

    test('Trades linked from League has no back link; the League tab leads back', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/players/trades?from=league`);
      await expect(visible(page, 'trades-title')).toHaveText('Active Trades');
      await expect(visible(page, 'back-link')).toHaveCount(0);
      await expect(page.getByRole('heading', { name: 'Trades', exact: true })).toHaveCount(0);
      await expect(visible(page, 'open-trades')).toHaveAttribute('aria-current', 'page');
      await expect(visible(page, 'tab-players')).toHaveAttribute('aria-selected', 'true');
      await visible(page, 'tab-league').click();
      await expect(page).toHaveURL(new RegExp(`${LEAGUE}/league$`));
      await expectLeagueTab(page);
    });
  });
}
