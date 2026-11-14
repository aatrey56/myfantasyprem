import { expect, test, type Page } from './fixtures';

import { LATE_LEAGUE, signIn } from './helpers';

// "Late Kickoff" was drafted after gameweek 7's deadline (the mocks' season
// is at gameweek 7), so its first gameweek is 8 (ADR 0010). Every view
// starts there: nothing offers gameweek 7 or earlier, and nothing says GW 0.

test.beforeEach(async ({ page }) => signIn(page));

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);

async function noEarlyGameweeks(page: Page) {
  await expect(page.locator('body')).not.toContainText(/\bGW ?0\b/);
  await expect(page.locator('body')).not.toContainText(/after GW/);
}

test('the League tab starts its matchups at the first gameweek', async ({ page }) => {
  await page.goto(`${LATE_LEAGUE}/league`);
  await expect(visible(page, 'loaded')).toBeVisible();
  // It opens on GW 8's pairings, still to play: no scores, and no stepping back.
  const header = visible(page, 'league-matchups-header');
  await expect(header.getByTestId('gw-label')).toHaveText('GW 8');
  await expect(header.getByTestId('gw-prev')).toBeDisabled();
  const cards = page.locator('[data-testid^="matchup-row-mu_"]:visible:not([inert] *)');
  await expect(cards.first()).toBeVisible();
  // A card's label is `Home 12 versus Away 9` once there are scores.
  for (const label of await cards.evaluateAll((els) =>
    els.map((e) => e.getAttribute('aria-label') ?? ''),
  )) {
    expect(label).toMatch(/ versus /);
    expect(label).not.toMatch(/ -?\d+ versus | -?\d+(, live)?$/);
  }
  await noEarlyGameweeks(page);
});

test('the Matchup tab shows the first gameweek as a pairing preview', async ({ page }) => {
  await page.goto(`${LATE_LEAGUE}/matchup`);
  await expect(visible(page, 'matchup-preview')).toBeVisible();
  await expect(visible(page, 'gw-label')).toHaveText('GW 8');
  await expect(visible(page, 'gw-prev')).toBeDisabled();
  await expect(page.getByText('Something went wrong')).toHaveCount(0);
  await noEarlyGameweeks(page);
  // A link to an earlier gameweek opens the first one.
  await page.goto(`${LATE_LEAGUE}/matchup?gw=3`);
  await expect(visible(page, 'gw-label')).toHaveText('GW 8');
});

test("another team's page opens at the first gameweek after the Status tab", async ({ page }) => {
  // In-app navigation keeps the season's gameweek (7) the Status tab loaded.
  await page.goto(LATE_LEAGUE);
  await expect(visible(page, 'status-loaded')).toBeVisible();
  await page
    .getByRole('navigation', { name: 'League sections' })
    .getByRole('tab', { name: 'League' })
    .click();
  await visible(page, 'standings')
    .locator('[data-testid^="standing-"]')
    .filter({ hasText: 'Transfer Window Shoppers' })
    .first()
    .click();
  await expect(visible(page, 'gw-label')).toHaveText('GW 8');
  await expect(visible(page, 'gw-prev')).toBeDisabled();
  await expect(page.getByText('Something went wrong')).toHaveCount(0);
  await expect(page.getByText(/no players for that gameweek/)).toHaveCount(0);
});
