import { expect, test } from './fixtures';

import { signIn } from './helpers';

// The import screen on a phone (390px, touch, no hardware keyboard): the
// links people paste from the FPL Draft site, and no desktop-only hints.
test.beforeEach(async ({ page }) => signIn(page));
test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

for (const link of [
  'https://www.draft.premierleague.com/league/90001',
  'http://draft.premierleague.com/en/league/90001/standings?tab=1#top',
  ' 90001 ',
]) {
  test(`pasting ${JSON.stringify(link)} finds the league`, async ({ page }) => {
    await page.goto('/import');
    await page.getByTestId('league-input').fill(link);
    const preview = page.waitForRequest((r) => new URL(r.url()).pathname === '/v1/imports/preview');
    await page.getByTestId('find-button').tap();
    expect(new URL((await preview).url()).searchParams.get('fplLeagueId')).toBe('90001');
    await expect(page.getByTestId('preview')).toBeVisible();
    await expect(page.getByTestId('input-error')).toHaveCount(0);
    await expect(page.getByTestId('preview-error')).toHaveCount(0);
  });
}

test('a link from another site is explained in plain English', async ({ page }) => {
  await page.goto('/import');
  await page.getByTestId('league-input').fill('https://example.com/league/90001');
  await page.getByTestId('find-button').tap();
  await expect(page.getByTestId('input-error')).toHaveText(
    "That doesn't look like an FPL Draft league link. Paste the link from your league page, or just the league ID.",
  );
  await expect(page.getByText(/draftLeagueUrl|fplLeagueId/)).toHaveCount(0);
});

test('no Tab hint on a touch screen; the example button is there instead', async ({ page }) => {
  await page.goto('/import');
  await expect(page.getByTestId('league-input')).toBeVisible();
  await expect(page.getByTestId('example-hint')).toHaveCount(0);
  await expect(page.getByText(/Press Tab/)).toHaveCount(0);
  await page.getByTestId('use-example-button').tap();
  await expect(page.getByTestId('league-input')).toHaveValue(
    'https://draft.premierleague.com/league/12345',
  );
});
