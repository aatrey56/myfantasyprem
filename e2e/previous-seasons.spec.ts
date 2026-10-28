import { expect, test, type Page } from './fixtures';

import { LEAGUE, overflowingElements, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

const visible = (page: Page, testId: string) => page.locator(`[data-testid="${testId}"]:visible`);

/**
 * Opens the first free agent's card whose mock history has seasons from
 * before 2016/17 (mocks/seasons.ts), so untracked stats show.
 */
async function openVeteran(page: Page) {
  await page.goto(`${LEAGUE}/players`);
  // Available is on by default: only unowned players.
  const rows = page.locator('[data-testid^="player-plr_"]:visible');
  await expect(rows.first()).toBeVisible();
  for (let i = 0; i < 12; i++) {
    await rows.nth(i).click();
    const card = visible(page, 'player-card');
    await expect(card.getByTestId('card-name')).toBeVisible();
    const career = card.getByTestId('card-career');
    await expect(career.or(card.getByTestId('career-empty'))).toBeVisible();
    if (
      (await career.count()) > 0 &&
      (await card.locator('[data-testid="career-row-2015/16"]').count()) > 0
    ) {
      return card;
    }
    await page.keyboard.press('Escape');
    await expect(card).toHaveCount(0);
  }
  throw new Error('no free agent with seasons before 2016/17 in the first 12');
}

for (const [name, viewport] of [
  ['phone 390', { width: 390, height: 844 }],
  ['desktop', { width: 1280, height: 900 }],
] as const) {
  test.describe(`previous seasons (${name})`, () => {
    test.use({ viewport });

    test('lists seasons newest first, a career row and dashes for untracked stats', async ({
      page,
    }) => {
      const card = await openVeteran(page);
      const table = card.getByTestId('card-career');
      await table.scrollIntoViewIfNeeded();
      await expect(table).toBeVisible();
      const ids = await table
        .locator('[data-testid^="career-row-"][aria-label]')
        .evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')));
      expect(ids[0]).toBe('career-row-2025/26');
      expect(ids.at(-1)).toBe('career-row-total');
      const seasons = ids.slice(0, -1).map((id) => id!.replace('career-row-', ''));
      expect([...seasons].sort().reverse()).toEqual(seasons);
      // FPL tracked no ICT before 2016/17, no xG before 2022/23.
      await expect(card.getByTestId('career-row-2015/16-ictIndex')).toHaveText('—');
      await expect(card.getByTestId('career-row-2015/16-expectedGoals')).toHaveText('—');
      await expect(card.getByTestId('career-row-2025/26-expectedGoals')).toHaveText(/^\d+\.\d\d$/);
      await expect(card.getByTestId('career-row-total')).toContainText('Career');
      await expect(card.getByTestId('career-row-total-points')).toHaveText(/^\d+$/);
      await expect(card.getByTestId('career-note')).toContainText('FPL assists');
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('the season column stays pinned while the stats scroll', async ({ page }) => {
      const card = await openVeteran(page);
      const scroll = card.getByTestId('card-career-scroll');
      await scroll.scrollIntoViewIfNeeded();
      const label = card.getByTestId('career-row-2025/26').getByText('2025/26');
      const before = (await label.boundingBox())!;
      const dc = card.getByTestId('career-row-2025/26-defensiveContribution');
      await dc.scrollIntoViewIfNeeded();
      await expect(dc).toBeInViewport();
      await expect.poll(async () => (await label.boundingBox())!.x).toBeCloseTo(before.x, 0);
    });
  });
}
