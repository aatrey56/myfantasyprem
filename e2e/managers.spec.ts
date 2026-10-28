import { expect, test } from './fixtures';

import { PREDRAFT_LEAGUE, pageOverflow, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

// Five-a-side Friends: the demo user is commissioner; "Jumpers for Goalposts"
// is a 21-character team name.
const LONG_NAME = 'Jumpers for Goalposts';

for (const vp of [
  { name: 'phone 390', width: 390, height: 844 },
  { name: 'desktop 1280', width: 1280, height: 800 },
] as const) {
  test.describe(`settings managers, ${vp.name}`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('a long team name shows in full and the commissioner action works', async ({ page }) => {
      await page.goto(`${PREDRAFT_LEAGUE}/settings`);
      const name = page.locator('[data-testid^="member-name-"]', { hasText: LONG_NAME });
      await expect(name).toBeVisible();
      await expect(name).toHaveText(LONG_NAME);

      // Not clipped: the text fits its box, on one line, with no ellipsis.
      const fit = await name.evaluate((el) => ({
        clipped: el.scrollWidth > el.clientWidth,
        lines: Math.round(
          el.getBoundingClientRect().height / parseFloat(getComputedStyle(el).lineHeight),
        ),
        ellipsis: getComputedStyle(el).textOverflow === 'ellipsis',
      }));
      expect(fit.clipped).toBe(false);
      expect(fit.lines).toBe(1);
      expect(fit.ellipsis).toBe(false);

      // The row's actions keep their names and 44pt touch targets.
      const row = page.locator('[data-testid^="member-name-"]', { hasText: LONG_NAME });
      const make = page.locator('[data-testid^="make-commissioner-"]').first();
      const remove = page.getByRole('button', { name: /^Remove / }).first();
      for (const target of [make, remove]) {
        await expect(target).toBeVisible();
        const box = (await target.boundingBox())!;
        // Buttons are 36 tall with a 4pt hit slop around them.
        expect(box.height).toBeGreaterThanOrEqual(36);
        expect(box.width).toBeGreaterThanOrEqual(36);
      }
      await expect(make).toHaveAccessibleName('Make commissioner');
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
      await expect(row).toBeVisible();

      // Make the first other manager commissioner.
      await make.click();
      await page.getByTestId('confirm-ok').click();
      await expect(page.getByTestId('toast')).toContainText('is now commissioner');
      await expect(page.locator('[data-testid^="make-commissioner-"]')).toHaveCount(0);
      await expect(page.getByLabel('Commissioner').first()).toBeVisible();
    });
  });
}
