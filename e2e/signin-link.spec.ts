import { expect, test } from './fixtures';

import { signIn } from './helpers';

// docs/adr/0013, "Sign-in links": a manager who joined by claim link (no
// email) and signed out gets back in with a link from the commissioner.
// On the mocks (apps/app/src/mocks/imported.ts) Priya N. has claimed a
// team in the imported league the demo user runs.
const IMPORTED = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd5';

for (const size of [
  { name: 'phone', viewport: { width: 390, height: 844 }, hasTouch: true },
  { name: 'desktop', viewport: { width: 1280, height: 800 }, hasTouch: false },
]) {
  test.describe(size.name, () => {
    test.use({ viewport: size.viewport, hasTouch: size.hasTouch });

    test('the commissioner issues a sign-in link; a signed-out browser opens it and is that manager', async ({
      page,
      browser,
      baseURL,
    }) => {
      await signIn(page);
      await page.goto(`${IMPORTED}/settings`);
      const links = page.getByTestId('claim-links');
      const row = links.getByTestId('signin-link');
      // Only Priya's team: mine needs no link, unclaimed teams have claim links.
      await expect(row).toHaveCount(1);
      await row.getByTestId('signin-link-issue').click();
      await expect(row.getByTestId('signin-link-help')).toHaveText(
        'Send this to Priya N. to sign back in. Works once, expires in 7 days.',
      );
      const url = (await row.getByTestId('signin-link-url').textContent())!;
      expect(url).toMatch(/\/signin-link\/[A-Za-z0-9_-]{43}$/);
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      );
      expect(overflow).toBeLessThanOrEqual(0);

      // Another device, signed out.
      const other = await browser.newContext({
        baseURL,
        viewport: size.viewport,
        hasTouch: size.hasTouch,
      });
      const friend = await other.newPage();
      const redeems: string[] = [];
      friend.on('request', (r) => {
        if (r.method() === 'POST' && r.url().endsWith('/v1/auth/signin-link'))
          redeems.push(r.url());
      });
      await friend.goto(new URL(url).pathname);
      // Out of the URL at once; nothing is spent until a tap, so opening
      // and reloading it (as a link scanner might) leaves it working.
      await expect(friend).toHaveURL(/\/signin-link$/);
      await expect(friend.getByTestId('signin-link-ready')).toBeVisible();
      await friend.goto(new URL(url).pathname);
      await expect(friend.getByTestId('signin-link-ready')).toBeVisible();
      expect(redeems).toHaveLength(0);
      await friend.getByTestId('signin-link-confirm').click();
      await expect(friend).toHaveURL(new RegExp(`${IMPORTED}$`));
      await expect(friend.getByTestId('toast')).toContainText('Signed in as Priya N.');
      const stored = await friend.evaluate(() => localStorage.getItem('mfp.session'));
      expect(JSON.parse(stored!)).toMatchObject({ user: { displayName: 'Priya N.', email: null } });
      // The link (a credential) is not left in the history.
      expect(await friend.evaluate(() => window.location.pathname)).not.toContain('signin-link');
      expect(redeems).toHaveLength(1);
      await other.close();
    });

    test('someone signed in as another user is asked first', async ({ page }) => {
      await signIn(page);
      await page.goto(`${IMPORTED}/settings`);
      const row = page.getByTestId('claim-links').getByTestId('signin-link');
      await row.getByTestId('signin-link-issue').click();
      const url = (await row.getByTestId('signin-link-url').textContent())!;

      await page.goto(new URL(url).pathname);
      await expect(page.getByTestId('signin-link-signed-in')).toContainText(
        "You're signed in as Sam P. Sign out and use this link?",
      );
      await page.getByTestId('signin-link-confirm').click();
      await expect(page).toHaveURL(new RegExp(`${IMPORTED}$`));
      await expect(page.getByTestId('toast')).toContainText('Signed in as Priya N.');
    });
  });
}

test('a malformed sign-in link says so', async ({ page }) => {
  await page.goto('/signin-link/nope');
  await expect(page.getByTestId('signin-link-problem')).toContainText(
    "This sign-in link doesn't work",
  );
});
