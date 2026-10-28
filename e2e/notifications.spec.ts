import { expect, test, type Page } from './fixtures';

import { LEAGUE, pageOverflow, signIn } from './helpers';

// Notifications on the mocks (apps/app/src/mocks/notifications.ts): every
// type starts on in every league; the inbox has six items, four unread.

test.beforeEach(async ({ page }) => signIn(page));

/** Sections keep earlier screens mounted but hidden: only match what is on screen. */
const visible = (page: Page, testId: string) => page.locator(`[data-testid="${testId}"]:visible`);
const LEAGUE_ID = LEAGUE.split('/').pop()!;
const LABELS: Record<string, string> = {
  player_goal: 'My players: goals',
  opponent_news: "Opponent's players: injury and status news",
};
const switchFor = (page: Page, type: string) =>
  page.getByRole('switch', { name: LABELS[type], exact: true });

for (const vp of [
  { name: 'phone 390', width: 390, height: 844 },
  { name: 'desktop 1280', width: 1280, height: 800 },
] as const) {
  test.describe(`notifications (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('Account opens the settings; a switch, All off and All on flip the switches', async ({
      page,
    }) => {
      // Browser push, blocked for this site: headless Chromium reports that by
      // default, WebKit reports "default" (not asked yet), so pin it.
      await page.addInitScript(() =>
        Object.defineProperty(Notification, 'permission', { get: () => 'denied' }),
      );
      await page.goto('/account');
      await visible(page, 'account-notifications').click();
      await expect(page).toHaveURL(/\/account\/notifications$/);
      await expect(visible(page, 'notification-group-personal')).toContainText('Personal');
      await expect(visible(page, 'notification-group-league')).toContainText('League');
      await expect(visible(page, 'push-prompt')).toContainText('Push notifications on this device');
      if (/iPhone|iPad/.test(await page.evaluate(() => navigator.userAgent))) {
        // iOS Safari has web push only for a Home Screen app.
        await expect(visible(page, 'push-unavailable')).toContainText('Home Screen');
      } else {
        // Blocked: the card says how to allow them rather than offering the button.
        await expect(visible(page, 'push-denied')).toContainText('site settings');
      }

      await visible(page, `notification-league-${LEAGUE_ID}`).click();
      const goal = switchFor(page, 'player_goal');
      await expect(goal).toBeChecked();
      await goal.click();
      await expect(goal).not.toBeChecked();

      await visible(page, 'notifications-all-off').click();
      await expect(switchFor(page, 'opponent_news')).not.toBeChecked();
      await expect(visible(page, 'notifications-all-off')).toBeDisabled();
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);

      await visible(page, 'notifications-all-on').click();
      await expect(switchFor(page, 'player_goal')).toBeChecked();
    });

    test('FEED shows the inbox with an unread badge, and marking read clears it', async ({
      page,
    }) => {
      await page.goto('/feed');
      const badge = page.getByTestId('app-tab-feed-badge');
      await expect(badge).toHaveText('4');
      await expect(visible(page, 'inbox')).toContainText('Notifications · 4 new');
      await expect(visible(page, 'inbox').getByTestId('inbox-unread-dot')).toHaveCount(4);
      // Opening one marks it read and follows its link.
      await visible(page, 'inbox-item-ntf_01k7m2xq8z0000000000000005').click();
      await expect(page).toHaveURL(/\/players\/trades/);
      await expect(badge).toHaveText('3');
      await page.goto('/feed');
      await visible(page, 'inbox-mark-all-read').click();
      await expect(badge).toHaveCount(0);
      await expect(visible(page, 'inbox')).toContainText(/^Notifications/);
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
    });

    test('a news notification opens the player card with the full news; waivers open claims', async ({
      page,
    }) => {
      await page.goto('/feed');
      // Stored the way older server rows were (/league/...): no "Page not found".
      await visible(page, 'inbox-item-ntf_01k7m2xq8z0000000000000004').click();
      await expect(page).toHaveURL(new RegExp(`/leagues/${LEAGUE_ID}/players/plr_`));
      await expect(page.getByText('Page not found')).toHaveCount(0);
      await expect(visible(page, 'card-status')).toContainText(
        'Hamstring injury, out for several weeks',
      );
      await expect(visible(page, 'card-status-time')).toBeVisible();
      await expect(visible(page, 'back-link')).toHaveText(/Feed/);
      await visible(page, 'back-link').click();
      await expect(page).toHaveURL(/\/feed$/);
      await visible(page, 'inbox-item-ntf_01k7m2xq8z0000000000000003').click();
      await expect(page).toHaveURL(new RegExp(`/leagues/${LEAGUE_ID}/players/waivers`));
      await expect(page.getByText('Page not found')).toHaveCount(0);
    });
  });
}
