import { expect, test } from './fixtures';

import { PUB_INVITE_CODE, PUB_LEAGUE_NAME, pageOverflow, signIn, signInWithCode } from './helpers';

for (const vp of [
  { name: 'phone', width: 375, height: 812 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`leagues hub (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('sign in, create a league and see its invite code', async ({ page }) => {
      await page.goto('/sign-in');
      await signInWithCode(page);
      await expect(page.getByTestId('league-list')).toBeVisible();
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);

      await page.getByTestId('create-league').click();
      await expect(page).toHaveURL(/\/leagues\/new$/);
      // Rules are checked inline before anything is sent.
      await page.getByTestId('league-name-input').fill('ab');
      await page.getByTestId('create-league-submit').click();
      await expect(page.getByText('League name must be 3 to 40 characters.')).toBeVisible();

      await page.getByTestId('league-name-input').fill('Thursday Night Draft');
      await page.getByTestId('team-count-plus').click();
      await expect(page.getByTestId('team-count-value')).toHaveText('11 teams');
      await page.getByTestId('trade-review').getByRole('radio', { name: 'Vote' }).click();
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
      await page.getByTestId('create-league-submit').click();

      await expect(page.getByTestId('league-created')).toContainText('Thursday Night Draft');
      await expect(page.getByTestId('invite-code')).toHaveText(/^[2-9A-HJKMNP-TV-Z]{10}$/);
      await expect(page.getByTestId('invite-url')).toContainText('/join/');
      await page.getByTestId('go-to-league').click();
      await expect(page).toHaveURL(/\/leagues\/lg_[0-9a-z]{26}$/);
    });

    test('join a league by invite code', async ({ page }) => {
      await signIn(page);
      await page.goto('/leagues');
      await expect(page.getByTestId('league-list')).not.toContainText(PUB_LEAGUE_NAME);
      await page.getByTestId('join-league').click();
      await page.getByTestId('join-code-input').fill('nope');
      await page.getByTestId('join-code-submit').click();
      await expect(page.getByText(/Invite codes are 10 letters/)).toBeVisible();
      await page.getByTestId('join-code-input').fill(PUB_INVITE_CODE.toLowerCase());
      await page.getByTestId('join-code-submit').click();

      await expect(page).toHaveURL(new RegExp(`/join/${PUB_INVITE_CODE}$`));
      await expect(page.getByTestId('invite-league-name')).toHaveText(PUB_LEAGUE_NAME);
      await page.getByTestId('join-team-name').fill('Late Winners');
      await page.getByTestId('join-submit').click();
      await expect(page).toHaveURL(/\/leagues\/lg_[0-9a-z]{26}$/);
      await expect(page.getByTestId('toast')).toContainText(`You joined ${PUB_LEAGUE_NAME}`);

      // In-app navigation: a reload would reset the mocks.
      await page.getByTestId(vp.width < 768 ? 'back-to-leagues' : 'nav-home').click();
      await expect(
        page.locator('[data-testid="league-list"]:visible:not([inert] *)'),
      ).toContainText(PUB_LEAGUE_NAME);
    });
  });
}

test('an invite link previews the league before sign-in', async ({ page }) => {
  await page.goto(`/join/${PUB_INVITE_CODE}`);
  await expect(page.getByTestId('invite-league-name')).toHaveText(PUB_LEAGUE_NAME);
  await page.getByTestId('join-sign-in').click();
  await expect(page).toHaveURL(/\/sign-in\?next=/);
  await page.getByTestId('sign-in-google').click();
  await expect(page).toHaveURL(new RegExp(`/join/${PUB_INVITE_CODE}$`));
  await expect(page.locator('[data-testid="join-submit"]:visible:not([inert] *)')).toBeVisible();
});

test('an invite to a league I am already in opens that league', async ({ page }) => {
  await signIn(page);
  // Five-a-side Friends' code; the demo user is its commissioner.
  await page.goto('/join/K7QM2XRP4D');
  await page.locator('[data-testid="join-open-league"]:visible:not([inert] *)').click();
  await expect(page).toHaveURL(/\/leagues\/lg_01k7m2xq8rfv3c9a4t6wbn5hd2$/);
});

test('a commissioner sees the invite in league settings', async ({ page }) => {
  await signIn(page);
  // Five-a-side Friends: pre-draft, the demo user is commissioner.
  await page.goto('/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd2/settings');
  await expect(page.getByTestId('invite-code')).toHaveText('K7QM2XRP4D');
  // 5 of 8 managers is enough to start; the empty places become open seats.
  await expect(page.getByTestId('start-draft')).toBeEnabled();
});

test('the invite link the server returns opens its join screen', async ({ page, baseURL }) => {
  await signIn(page);
  await page.goto('/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd2/settings');
  // The server builds the link from APP_URL, this app's origin; the app
  // shows it as is.
  const url = `${baseURL}/join/K7QM2XRP4D`;
  await expect(page.getByTestId('invite-url')).toHaveText(url);
  await page.goto(url);
  await expect(page.getByTestId('invite-league-name')).toHaveText('Five-a-side Friends');
});

test('an invite link opened directly while signed in suggests a team name', async ({ page }) => {
  await signIn(page);
  await page.goto(`/join/${PUB_INVITE_CODE}`);
  await expect(page.getByTestId('invite-league-name')).toHaveText(PUB_LEAGUE_NAME);
  await expect(page.locator('[data-testid="join-team-name"]:visible:not([inert] *)')).toHaveValue(
    'Sam FC',
  );
});
