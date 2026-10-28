import { expect, test } from './fixtures';

import { LEAGUE, pageOverflow, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

const VIEWPORTS = [
  { name: 'phone 390', width: 390, height: 844 },
  { name: 'desktop 1280', width: 1280, height: 800 },
] as const;

for (const vp of VIEWPORTS) {
  test.describe(`draft results, ${vp.name}`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('a finished draft is reachable from the league and read-only', async ({ page }) => {
      const requests: string[] = [];
      page.on('request', (r) => requests.push(`${r.method()} ${new URL(r.url()).pathname}`));
      const problems: string[] = [];
      page.on('response', (r) => {
        if (r.status() >= 400 && new URL(r.url()).pathname.includes('/draft')) {
          problems.push(`${r.status()} ${r.url()}`);
        }
      });

      await page.goto(`${LEAGUE}/league`);
      const entry = page.getByTestId('link-draft-results');
      await expect(entry).toBeVisible();
      await expect(entry).toHaveAccessibleName('Draft results');
      const box = await entry.boundingBox();
      expect(box!.height).toBeGreaterThanOrEqual(36); // 36 plus 4pt hit slop each way
      await entry.click();
      await expect(page).toHaveURL(new RegExp(`${LEAGUE}/draft$`));

      // The board renders with every pick; nothing to draft or queue.
      await expect(page.getByRole('heading', { name: 'Draft results' })).toBeVisible();
      await expect(page.getByTestId('draft-board')).toBeVisible();
      await expect(page.getByTestId('draft-clock-title')).toHaveText('Draft complete');
      await expect(page.locator('[data-testid^="board-pick-plr_"]').first()).toBeVisible();
      expect(await page.locator('[data-testid^="board-pick-plr_"]').count()).toBeGreaterThan(30);
      await expect(page.getByTestId('board-current')).toHaveCount(0);
      await expect(page.getByTestId('draft-players')).toHaveCount(0);
      await expect(page.getByTestId('pane-players')).toHaveCount(0);
      await expect(page.getByTestId('pane-queue')).toHaveCount(0);
      await expect(page.locator('[data-testid^="draft-pick-plr_"]')).toHaveCount(0);
      await expect(page.getByTestId('draft-pause')).toHaveCount(0);
      await expect(page.getByTestId('draft-start')).toHaveCount(0);

      // Picks by team: Sam's squad first, then any other team.
      if (vp.width < 768) await page.getByTestId('pane-roster').click();
      await expect(page.getByTestId('draft-roster')).toBeVisible();
      await expect(page.getByTestId('draft-roster')).toContainText('15 of 15 picked');
      const chips = page.locator('[data-testid^="draft-team-tm_"]');
      expect(await chips.count()).toBeGreaterThan(1);
      const name = (await chips.nth(1).innerText()).trim();
      await chips.nth(1).click();
      await expect(page.getByTestId('draft-roster')).toContainText(name);
      await expect(page.getByTestId('draft-roster')).toContainText('15 of 15 picked');

      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
      expect(problems).toEqual([]);
      expect(requests.filter((r) => r.includes('/draft/queue'))).toEqual([]);
      expect(requests.filter((r) => r.startsWith('POST') || r.startsWith('PUT'))).toEqual([]);

      await page.getByTestId('draft-back').click();
      await expect(page).toHaveURL(new RegExp(`${LEAGUE}$`));
    });
  });
}

test.describe('draft results, top bar', () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test('the league bar links to the results, and not to a live room', async ({ page }) => {
    await page.goto(LEAGUE);
    await expect(page.getByTestId('nav-draft-room')).toHaveCount(0);
    await page.getByTestId('nav-draft-results').click();
    await expect(page.getByTestId('draft-board')).toBeVisible();
  });
});
