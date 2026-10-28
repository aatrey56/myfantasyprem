import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn, useScenario } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) => page.locator(`[data-testid="${testId}"]:visible`);

// Touch: the pager keeps the tabs either side of the visible one mounted, so
// going back to one does not remount it (and refetchOnMount never fires).
test.describe('a tab shown again (touch, 390px)', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test('refetches what went stale while it was away', async ({ page }) => {
    await page.clock.install();
    await page.goto(LEAGUE);
    const position = visible(page, 'summary-position');
    await expect(position).toBeVisible();
    await expect(position).not.toContainText('▲ 7');

    const tabs = page.getByTestId('league-tabs');
    await tabs.getByTestId('tab-matchup').tap();
    await expect(tabs.getByRole('tab', { name: 'Match' })).toHaveAttribute('aria-selected', 'true');

    // The server's answer changes, and the Status page's copy goes stale.
    await useScenario(page, 'status-rank-up');
    await page.clock.fastForward('00:20');

    const statusRequest = page.waitForRequest((r) => r.url().endsWith('/status'));
    await tabs.getByTestId('tab-index').tap();
    await statusRequest;
    await expect(position).toContainText('▲ 7');
  });
});
