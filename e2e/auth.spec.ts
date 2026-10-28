import { expect, test } from './fixtures';

import {
  LEAGUE,
  MOCK_CODE,
  MOCK_LINK_TOKEN,
  MOCK_WRONG_CODE,
  PUB_INVITE_CODE,
  PUB_LEAGUE_NAME,
  pageOverflow,
  signIn,
  signInWithCode,
} from './helpers';

test.describe('signed out', () => {
  test('the root and league pages send you to sign in', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveURL(/\/sign-in/);
    await expect(page.getByRole('heading', { name: /Sign in to/ })).toBeVisible();
    await page.goto(LEAGUE);
    await expect(page).toHaveURL(/\/sign-in\?next=/);
  });

  test('email sign-in lands on the leagues hub and remembers the session', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('sign-in-email').fill('not-an-email');
    await page.getByTestId('sign-in-email-submit').click();
    await expect(page.getByText('Enter a valid email address.')).toBeVisible();
    await signInWithCode(page);
    await expect(page).toHaveURL(/\/leagues$/);
    await expect(page.getByTestId('league-list')).toContainText('The Sunday Draft');
    await page.reload();
    await expect(page).toHaveURL(/\/leagues$/);
    await expect(page.getByTestId('league-list')).toBeVisible();
  });

  test('request a link, type the code digit by digit, signed in', async ({ page }) => {
    await page.goto('/sign-in');
    await page.getByTestId('sign-in-email').fill('sam@example.com');
    await page.getByTestId('sign-in-email-submit').click();
    const sent = page.getByTestId('email-sent');
    await expect(sent).toContainText('sam@example.com');
    await expect(sent).toContainText('6-digit code');
    // Six labelled boxes; typing moves along, a wrong code says so and clears.
    const digit = (i: number) => page.getByLabel(`Sign-in code, digit ${i} of 6`);
    await digit(1).click();
    await page.keyboard.type(MOCK_WRONG_CODE);
    await expect(page.getByTestId('sign-in-code-error')).toContainText('wrong or has expired');
    await expect(digit(1)).toHaveValue('');
    await expect(digit(1)).toBeFocused();
    await page.keyboard.type(MOCK_CODE.slice(0, 5));
    await expect(digit(6)).toBeFocused();
    await expect(page).toHaveURL(/\/sign-in/);
    // Backspace in the empty sixth box clears the fifth and goes back to it.
    await page.keyboard.press('Backspace');
    await expect(digit(5)).toHaveValue('');
    await expect(digit(5)).toBeFocused();
    await page.keyboard.type(MOCK_CODE.slice(4));
    await expect(page).toHaveURL(/\/leagues$/);
    await expect(page.getByTestId('league-list')).toContainText('The Sunday Draft');
  });

  test('a pasted code fills every box and signs in', async ({ page }) => {
    await page.goto('/sign-in');
    await page.getByTestId('sign-in-email').fill('sam@example.com');
    await page.getByTestId('sign-in-email-submit').click();
    await page.getByLabel('Sign-in code, digit 3 of 6').click();
    // A real paste event, as from the clipboard.
    await page.evaluate(
      (code) => {
        const el = document.activeElement as HTMLInputElement;
        const data = new DataTransfer();
        data.setData('text/plain', code);
        el.dispatchEvent(
          new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }),
        );
        if (!el.value) {
          // Chromium does not insert synthetic pastes: do what the browser would.
          const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
          set.call(el, code);
          el.dispatchEvent(new Event('input', { bubbles: true }));
        }
      },
      `${MOCK_CODE.slice(0, 3)} ${MOCK_CODE.slice(3)}`,
    );
    await expect(page).toHaveURL(/\/leagues$/);
  });

  test('Apple sign-in returns to the page you asked for', async ({ page }) => {
    await page.goto(LEAGUE);
    await expect(page).toHaveURL(/\/sign-in/);
    await page.getByTestId('sign-in-apple').click();
    await expect(page).toHaveURL(new RegExp(`${LEAGUE}$`));
    await expect(page.getByTestId('status-loaded')).toBeVisible();
  });

  test('a sign-in next path cannot leave the app', async ({ page }) => {
    await page.goto('/sign-in?next=//evil.example/x');
    await page.getByTestId('sign-in-google').click();
    await expect(page).toHaveURL(/\/leagues$/);
  });

  for (const next of ['https://evil.example/x', '//evil.example/x', '/\\evil.example']) {
    test(`an emailed link's next cannot leave the app (${next})`, async ({ page }) => {
      await page.goto(`/sign-in?token=${MOCK_LINK_TOKEN}&next=${encodeURIComponent(next)}`);
      await page.getByTestId('magic-link-continue').click();
      await expect(page).toHaveURL(/localhost:\d+\/leagues$/);
    });
  }
});

test.describe('signing in from an invite link', () => {
  const JOIN = new RegExp(`/join/${PUB_INVITE_CODE}$`);

  test('signed out, sign in with a code and land on the join screen, name filled in', async ({
    page,
  }) => {
    await page.goto(`/join/${PUB_INVITE_CODE}`);
    await expect(page.getByTestId('invite-league-name')).toHaveText(PUB_LEAGUE_NAME);
    await page.getByTestId('join-sign-in').click();
    await expect(page).toHaveURL(/\/sign-in\?next=/);
    // The emailed link carries the invite too.
    const request = page.waitForRequest((r) => r.url().endsWith('/v1/auth/email'));
    await signInWithCode(page);
    expect((await request).postDataJSON()).toEqual({
      email: 'sam@example.com',
      next: `/join/${PUB_INVITE_CODE}`,
    });
    await expect(page).toHaveURL(JOIN);
    await expect(page.getByTestId('invite-league-name')).toHaveText(PUB_LEAGUE_NAME);
    await expect(page.locator('[data-testid="join-team-name"]:visible:not([inert] *)')).toHaveValue(
      'Sam FC',
    );
    await page.locator('[data-testid="join-submit"]:visible:not([inert] *)').click();
    await expect(page.getByTestId('toast')).toContainText(`You joined ${PUB_LEAGUE_NAME}`);
  });

  test('the emailed link opened in another browser goes to the join screen', async ({
    page,
    browser,
  }) => {
    await page.goto(`/join/${PUB_INVITE_CODE}`);
    await page.getByTestId('join-sign-in').click();
    await page.getByTestId('sign-in-email').fill('sam@example.com');
    await page.getByTestId('sign-in-email-submit').click();
    await expect(page.getByTestId('email-sent')).toBeVisible();
    // A fresh browser: no session storage, no stored session.
    const other = await browser.newContext();
    const tab = await other.newPage();
    const link = `/sign-in?token=${MOCK_LINK_TOKEN}&next=${encodeURIComponent(`/join/${PUB_INVITE_CODE}`)}`;
    await tab.goto(new URL(link, page.url()).toString());
    await tab.getByTestId('magic-link-continue').click();
    await expect(tab).toHaveURL(JOIN);
    await expect(tab.locator('[data-testid="join-team-name"]:visible:not([inert] *)')).toHaveValue(
      'Sam FC',
    );
    await other.close();
  });

  test('a link without next still returns to the invite in the same tab', async ({ page }) => {
    await page.goto(`/join/${PUB_INVITE_CODE}`);
    await page.getByTestId('join-sign-in').click();
    await page.getByTestId('sign-in-email').fill('sam@example.com');
    await page.getByTestId('sign-in-email-submit').click();
    await expect(page.getByTestId('email-sent')).toBeVisible();
    // Kept in session storage: a link that lost its next still comes back.
    await page.goto(`/sign-in?token=${MOCK_LINK_TOKEN}`);
    await page.getByTestId('magic-link-continue').click();
    await expect(page).toHaveURL(JOIN);
  });

  test('Google sign-in from the invite goes straight to joining', async ({ page }) => {
    await page.goto(`/join/${PUB_INVITE_CODE}`);
    await page.getByTestId('join-sign-in').click();
    await page.getByTestId('sign-in-google').click();
    await expect(page).toHaveURL(JOIN);
    await expect(page.locator('[data-testid="join-team-name"]:visible:not([inert] *)')).toHaveValue(
      'Sam FC',
    );
  });
});

for (const vp of [
  { name: 'phone', width: 375, height: 812 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`account (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });
    test.beforeEach(async ({ page }) => signIn(page));

    test('sign out returns to sign in', async ({ page }) => {
      await page.goto('/leagues');
      await page.getByTestId('account-link').click();
      await expect(page.getByTestId('account-name')).toHaveText('Sam P.');
      // No About section and no disclaimer (removed at the owner's request, AGENTS.md).
      await expect(page.getByTestId('about')).toHaveCount(0);
      await expect(page.getByText(/Not affiliated|independent fantasy game/)).toHaveCount(0);
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
      await page.getByTestId('sign-out').click();
      await expect(page).toHaveURL(/\/sign-in/);
      expect(await page.evaluate(() => localStorage.getItem('mfp.session'))).toBeNull();
    });

    test('delete account asks first, then signs out', async ({ page }) => {
      await page.goto('/account');
      await page.getByTestId('delete-account').click();
      const dialog = page.getByTestId('delete-account-dialog');
      await expect(dialog).toBeVisible();
      await dialog.getByTestId('confirm-cancel').click();
      await expect(dialog).toHaveCount(0);
      await expect(page).toHaveURL(/\/account$/);
      await page.getByTestId('delete-account').click();
      await page.getByTestId('confirm-ok').click();
      await expect(page).toHaveURL(/\/sign-in/);
    });
  });
}
