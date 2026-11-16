import { expect, test } from './fixtures';

import { pageOverflow, startWithScenario } from './helpers';

// Production's setup (no email provider, no Apple or Google client ids):
// GET /v1/config turns every sign-in method off, so the page says how to
// get in instead of showing buttons that fail.
for (const vp of [
  { name: 'phone', width: 375, height: 812 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`invite-only sign-in (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('shows the invite-only message and no sign-in buttons', async ({ page }) => {
      await startWithScenario(page, 'sign-in-invite-only');
      await page.goto('/sign-in');
      await expect(page.getByTestId('sign-in-invite-only')).toContainText(
        'This league is invite-only. Open the claim link or sign-in link your commissioner sent you.',
      );
      await expect(page.getByTestId('sign-in-apple')).toHaveCount(0);
      await expect(page.getByTestId('sign-in-google')).toHaveCount(0);
      await expect(page.getByTestId('sign-in-email')).toHaveCount(0);
      await expect(page.getByTestId('sign-in-email-submit')).toHaveCount(0);
      await expect(page.getByTestId('sign-in-config-retry')).toHaveCount(0);
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
    });
  });
}
