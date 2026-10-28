import { expect, test } from './fixtures';

import { expectLeagueTab, LEAGUE, signIn, startWithScenario, useScenario } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

test.describe('league home failures (MSW overrides)', () => {
  test('a 500 retries, shows the error card, then recovers on Try again', async ({ page }) => {
    test.slow(); // three retries with exponential backoff, about 7 s
    await startWithScenario(page, 'league-500');
    let requests = 0;
    page.on('request', (r) => {
      if (/\/v1\/leagues\/lg_[0-9a-z]{26}$/.test(new URL(r.url()).pathname)) requests++;
    });
    await page.goto(`${LEAGUE}/league`);
    const card = page.getByTestId('query-error');
    await expect(card).toBeVisible({ timeout: 20_000 });
    await expect(card).toContainText('The server had a problem');
    expect(requests).toBe(4); // the first try and three retries
    await useScenario(page, null);
    await card.getByTestId('retry').click();
    await expectLeagueTab(page);
    await expect(page.getByTestId('query-error')).toHaveCount(0);
  });

  test('a 404 fails at once without retrying', async ({ page }) => {
    await startWithScenario(page, 'league-404');
    let requests = 0;
    page.on('request', (r) => {
      if (/\/v1\/leagues\/lg_[0-9a-z]{26}$/.test(new URL(r.url()).pathname)) requests++;
    });
    await page.goto(`${LEAGUE}/league`);
    await expect(page.getByTestId('query-error')).toContainText('League not found.', {
      timeout: 3_000,
    });
    await page.waitForTimeout(1_500); // a retry would have fired by now
    expect(requests).toBe(1);
  });

  test('a network error says the server is unreachable', async ({ page }) => {
    test.slow();
    await startWithScenario(page, 'league-network-error');
    await page.goto(`${LEAGUE}/league`);
    await expect(page.getByTestId('query-error')).toContainText("Can't reach the server", {
      timeout: 20_000,
    });
  });

  test('a failed refetch keeps the data and shows the strip and banner', async ({ page }) => {
    await page.clock.install();
    await page.goto(`${LEAGUE}/league`);
    await expectLeagueTab(page);
    await useScenario(page, 'league-500');
    // Live data polls every 30 s; then the three retries (1 + 2 + 4 s) run out.
    // Each retry is scheduled only after the previous request settles in real
    // time, so advance the fake clock in steps.
    await page.clock.runFor(31_000);
    const strip = page.getByTestId('refresh-error');
    await expect(async () => {
      await page.clock.runFor(1_000);
      await expect(strip).toBeVisible({ timeout: 100 });
    }).toPass({ timeout: 20_000 });
    await expect(strip).toContainText(/Couldn't refresh · last updated \d\d:\d\d ·\s*Retry/);
    await expect(page.getByTestId('unreachable-banner')).toContainText(
      /Can't reach the server · showing data from \d\d:\d\d/,
    );
    await expectLeagueTab(page);
    await expect(page.getByTestId('query-error')).toHaveCount(0);
    // Recovery from the strip.
    await useScenario(page, null);
    await page.getByTestId('refresh-retry').click();
    await expect(page.getByTestId('refresh-error')).toHaveCount(0);
    await expect(page.getByTestId('unreachable-banner')).toHaveCount(0);
  });

  test('stale upstream data shows the delayed banner over the data', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('mfp.mockStale', '1'));
    await page.goto(`${LEAGUE}/league`);
    await expectLeagueTab(page);
    const banner = page.getByTestId('stale-banner');
    await expect(banner).toHaveText(/^Player data delayed since \d\d:\d\d$/);
    await expect(banner).toHaveAttribute('aria-live', 'polite');
    await expect(banner).not.toHaveAttribute('role', 'alert');
  });

  test('going offline shows the offline banner', async ({ page, context }) => {
    await page.goto(`${LEAGUE}/league`);
    await expectLeagueTab(page);
    await context.setOffline(true);
    await expect(page.getByTestId('offline-banner')).toHaveText("You're offline");
    await context.setOffline(false);
    await expect(page.getByTestId('offline-banner')).toHaveCount(0);
  });
});

test.describe('phone 375px', () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test('the bottom bar pads for the bottom safe-area inset', async ({ page, browserName }) => {
    // Playwright can only fake safe-area insets through Chromium's CDP
    // (Emulation.setSafeAreaInsetsOverride); WebKit has no way to set them.
    test.skip(browserName !== 'chromium', 'safe-area inset emulation is CDP only');
    // Emulate a notched phone. env(safe-area-inset-*) is only non-zero with
    // viewport-fit=cover, so this also checks public/index.html.
    const cdp = await page.context().newCDPSession(page);
    await cdp.send(
      'Emulation.setSafeAreaInsetsOverride' as never,
      {
        insets: { top: 47, bottom: 34, left: 0, right: 0 },
      } as never,
    );
    await page.goto(`${LEAGUE}/league`);
    // The app's bottom bar (LEAGUES, SCORES, FEED, ACCOUNT) stays under a league.
    const bar = page.getByTestId('app-tabs');
    await expect(bar).toBeVisible();
    const box = (await bar.boundingBox())!;
    expect(Math.round(box.y + box.height)).toBe(812); // flush with the bottom edge
    const padding = await bar.evaluate((el) => parseFloat(getComputedStyle(el).paddingBottom));
    expect(padding).toBe(34);
    // The tabs themselves keep their full 60px height above the inset.
    const tab = bar.getByRole('tab', { name: 'Leagues' });
    const tabBox = (await tab.boundingBox())!;
    expect(tabBox.y + tabBox.height).toBeLessThanOrEqual(812 - padding + 0.5);
  });
});

test.describe('import', () => {
  test('a classic FPL link is refused with an explanation', async ({ page }) => {
    await page.goto('/import');
    await page
      .getByTestId('league-input')
      .fill('https://fantasy.premierleague.com/leagues/314/standings/c');
    await page.getByTestId('find-button').click();
    await expect(page.getByTestId('input-error')).toContainText('classic FPL link');
    await expect(page.getByTestId('preview')).toHaveCount(0);
  });
});

test.describe('polling', () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test('live data polls every 30 s only while its tab is focused', async ({ page }) => {
    await page.clock.install();
    let leagueRequests = 0;
    page.on('request', (r) => {
      if (/\/v1\/leagues\/lg_[0-9a-z]{26}$/.test(new URL(r.url()).pathname)) leagueRequests++;
    });
    await page.goto(`${LEAGUE}/league`);
    await expectLeagueTab(page);
    const afterLoad = leagueRequests;
    // Focused: one refetch per 30 s.
    await page.clock.runFor(31_000);
    await expect.poll(() => leagueRequests).toBe(afterLoad + 1);
    // Switch tabs: the league home is no longer focused, so it stops polling.
    await page.getByTestId('league-tabs').getByText('Players', { exact: true }).click();
    await expect(page).toHaveURL(/\/players$/);
    const afterSwitch = leagueRequests;
    await page.clock.runFor(95_000);
    await page.waitForTimeout(300);
    expect(leagueRequests).toBe(afterSwitch);
  });
});
