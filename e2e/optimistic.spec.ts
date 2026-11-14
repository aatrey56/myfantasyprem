import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn, useScenario } from './helpers';

/**
 * Optimistic moves: the change shows before the server answers, and a
 * refusal puts the screen back as it was with the server's reason. The
 * scenarios hold the refusal for 1.5 s, so both states are visible.
 */
test.beforeEach(async ({ page }) => signIn(page));

const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);
const visibleStarting = (page: Page, prefix: string) =>
  page.locator(`[data-testid^="${prefix}"]:visible:not([inert] *)`);

const PILL = '[data-testid^="lineup-swap-"]';
const pillId = async (page: Page, section: string, index: number) =>
  (await page.getByTestId(section).locator(PILL).nth(index).getAttribute('data-testid'))!.replace(
    'lineup-swap-',
    '',
  );

for (const vp of [
  { name: 'phone', width: 390, height: 844 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`optimistic moves (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('a keeper swap shows at once and rolls back when the server refuses', async ({ page }) => {
      await page.goto(`${LEAGUE}/team`);
      await page.getByTestId('view-list').click();
      await expect(page.getByTestId('list-starters').locator(PILL)).toHaveCount(11);
      // Slot 1 is the keeper; the first bench slot the backup keeper.
      const keeper = await pillId(page, 'list-starters', 0);
      const backup = await pillId(page, 'list-bench', 0);
      await useScenario(page, 'lineup-save-refused');

      await page.getByTestId(`lineup-swap-${keeper}`).click();
      await page.getByTestId(`swap-candidate-${backup}`).click();
      // Before the server answers (it takes 1.5 s): already swapped.
      await expect.poll(() => pillId(page, 'list-starters', 0), { timeout: 1_000 }).toBe(backup);
      expect(await pillId(page, 'list-bench', 0)).toBe(keeper);

      // The refusal: the server's reason, and the keepers back where they were.
      await expect(page.getByTestId('lineup-errors')).toContainText(
        'The server refused this lineup.',
      );
      expect(await pillId(page, 'list-starters', 0)).toBe(keeper);
      expect(await pillId(page, 'list-bench', 0)).toBe(backup);
      await expect(page.getByTestId('toast')).toHaveCount(0);
    });

    test('a free-agent signing shows at once and rolls back when the server refuses', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/players`);
      const add = page
        .locator('[data-testid^="move-"]:visible:not([inert] *)[aria-label^="Add "]')
        .first();
      await expect(add).toHaveAccessibleName(/^Add /);
      const id = (await add.getAttribute('data-testid'))!.replace('move-', '');
      const row = visible(page, `player-${id}`);
      await expect(row).not.toContainText('Your team');
      await useScenario(page, 'free-agent-refused');

      await add.click();
      await visibleStarting(page, 'drop-plr_').first().click();
      await visible(page, 'drop-confirm').click();
      // The card closes and the row is mine before the server answers.
      await expect(visible(page, 'drop-step')).toHaveCount(0);
      await expect(row).toContainText('Your team', { timeout: 1_000 });
      await expect(visible(page, `move-${id}`)).toHaveCount(0);

      // The refusal: the server's reason in the toast, the player free again.
      await expect(page.getByTestId('toast')).toContainText('Another team signed him first.');
      await expect(row).not.toContainText('Your team');
      await expect(visible(page, `move-${id}`)).toBeVisible();
    });
  });
}
