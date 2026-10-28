import { expect, test } from './fixtures';

import { signIn } from './helpers';

// Runs on chromium and webkit (see playwright.config.ts): Tab handling
// differs between engines, and Safari once moved focus instead of filling.
test.beforeEach(async ({ page }) => signIn(page));
test.use({ viewport: { width: 1280, height: 800 } });

const FILLED = 'https://draft.premierleague.com/league/98765';

test('the league input is focused when the import screen opens', async ({ page }) => {
  await page.goto('/import');
  await expect(page.getByTestId('league-input')).toBeFocused();
});

test('Tab right after opening fills the example and selects the id', async ({ page }) => {
  await page.goto('/import');
  const input = page.getByTestId('league-input');
  await expect(input).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(input).toBeFocused();
  await expect(input).toHaveValue('https://draft.premierleague.com/league/12345');
  await page.keyboard.type('98765');
  await expect(input).toHaveValue(FILLED);
});

test('Tab on the empty input fills the example and selects the id', async ({ page }) => {
  await page.goto('/import');
  await expect(page.getByTestId('example-hint')).toHaveText(
    'Press Tab to use the example, then type your league id',
  );
  const input = page.getByTestId('league-input');
  await input.focus();
  await page.keyboard.press('Tab');
  await expect(input).toBeFocused();
  await page.keyboard.type('98765');
  await expect(input).toHaveValue(FILLED);
  const preview = page.waitForRequest((r) => new URL(r.url()).pathname === '/v1/imports/preview');
  await page.getByTestId('find-button').click();
  const url = new URL((await preview).url());
  // The app sends the league id it parsed, not the link.
  expect(url.searchParams.get('fplLeagueId')).toBe('98765');
  expect(url.searchParams.get('draftLeagueUrl')).toBeNull();
  await expect(page.getByTestId('preview')).toBeVisible();
});

test('Tab with text in the input moves focus as usual', async ({ page }) => {
  await page.goto('/import');
  const input = page.getByTestId('league-input');
  await input.focus();
  await page.keyboard.type('12');
  await page.keyboard.press('Tab');
  await expect(input).not.toBeFocused();
  await expect(input).toHaveValue('12');
});

test('a league page link with no id explains itself', async ({ page }) => {
  await page.goto('/import');
  await page.getByTestId('league-input').fill('https://draft.premierleague.com/en/league');
  await page.getByTestId('find-button').click();
  await expect(page.getByTestId('input-error')).toContainText("doesn't include your league ID");
});

for (const vp of [
  { name: 'phone', width: 390, height: 844 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`import card on the leagues hub (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('the card opens the import screen and back returns to the hub', async ({ page }) => {
      await page.goto('/leagues');
      const card = page.getByTestId('import-league-card');
      await expect(card).toContainText('Import a league');
      await expect(card).toContainText('Bring your FPL Draft league over.');
      await card.click();
      await expect(page).toHaveURL(/\/import$/);
      await expect(page.getByTestId('league-input')).toBeVisible();
      const back = page.locator('[data-testid="back-link"]:visible:not([inert] *)');
      await expect(back).toHaveText('Leagues');
      await back.click();
      await expect(page).toHaveURL(/\/leagues$/);
      await expect(
        page.locator('[data-testid="import-league-card"]:visible:not([inert] *)'),
      ).toHaveCount(1);
    });
  });
}
