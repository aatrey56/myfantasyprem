import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn, startWithScenario } from './helpers';

// Owner chips (FixtureScreen, InjuriesCard) name the manager, "You" on my
// own team; Account → "Show owners as" switches them to team names. The
// League tab's matchup cards wrap long team names to two lines.

test.beforeEach(async ({ page }) => signIn(page));

const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);
const rows = (page: Page) => page.locator('[data-testid^="fixture-fx_"]:visible:not([inert] *)');

/** Opens gameweek 7's fixtures until one has a player of mine. */
async function openFixtureWithMine(page: Page) {
  for (let i = 0; i < 10; i++) {
    await page.goto('/scores?gw=7');
    await expect(rows(page)).toHaveCount(10);
    await rows(page).nth(i).click();
    const detail = visible(page, 'fixture-detail');
    await expect(detail).toBeVisible();
    if ((await detail.getByTestId('owner-badge-mine').count()) > 0) return detail;
  }
  throw new Error('no fixture has a player of mine');
}

/** Every owned row: its chips on one line, clear of the points. */
async function expectChipsClearOfPoints(page: Page) {
  const owned = visible(page, 'fixture-detail')
    .locator('[data-testid^="fx-player-plr_"]')
    .filter({ has: page.getByTestId('owner-badges') });
  expect(await owned.count()).toBeGreaterThan(0);
  for (const row of await owned.all()) {
    const chips = (await row.getByTestId('owner-badges').boundingBox())!;
    const points = (await row.getByTestId('fx-player-points').boundingBox())!;
    expect(chips.x + chips.width).toBeLessThanOrEqual(points.x + 0.5);
    // One line: no chip wrapped under another.
    expect(chips.height).toBeLessThanOrEqual(22);
  }
}

for (const vp of [
  { name: 'phone 375', width: 375, height: 812 },
  { name: 'desktop 1280', width: 1280, height: 800 },
] as const) {
  test.describe(`owner chips (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('first names by default, You on mine, clear of the points', async ({ page }) => {
      const detail = await openFixtureWithMine(page);
      await expect(detail.getByTestId('owner-badge-mine').first()).toHaveText('You');
      const other = detail.getByTestId('owner-badge');
      if ((await other.count()) > 0) await expect(other.first()).toHaveText(/^\p{Lu}\p{Ll}+$/u);
      await expectChipsClearOfPoints(page);
    });

    test('Show owners as: Team name, kept after a reload', async ({ page }) => {
      await page.goto('/account');
      const setting = visible(page, 'owner-label-setting');
      await expect(setting).toContainText('Show owners as');
      await setting.getByTestId('owner-label-team-name').click();
      // Kept on the device: a fresh load (openFixtureWithMine navigates) still shows team names.
      await page.reload();
      await expect(visible(page, 'owner-label-setting')).toBeVisible();

      const detail = await openFixtureWithMine(page);
      await expect(detail.getByTestId('owner-badge-mine').first()).toHaveText('You');
      const other = detail.getByTestId('owner-badge');
      if ((await other.count()) > 0) {
        // A team name (every mock team name has a space), the full one read out.
        await expect(other.first()).toHaveText(/\S\s+\S/);
        await expect(other.first()).toHaveAttribute('aria-label', / in /);
      }
      await expectChipsClearOfPoints(page);
    });
  });
}

test.describe('matchup cards (phone 375)', () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test('a long team name wraps to two lines and is shown in full', async ({ page }) => {
    await startWithScenario(page, 'long-team-names');
    await page.goto(`${LEAGUE}/league`);
    const cards = page.locator('[data-testid^="matchup-row-mu_"]:visible:not([inert] *)');
    await expect(cards.first()).toBeVisible();
    const name = page
      .locator('[data-testid^="matchup-name-"]:visible:not([inert] *)')
      .filter({ hasText: 'Reigning Champions' });
    await expect(name).toHaveCount(1);
    // Not clamped: all of it fits in the box, over two lines.
    const fit = await name.evaluate((el) => ({
      clamped: el.scrollHeight > el.clientHeight + 1 || el.scrollWidth > el.clientWidth + 1,
      lines: Math.round(
        el.getBoundingClientRect().height / parseFloat(getComputedStyle(el).lineHeight),
      ),
    }));
    expect(fit.clamped).toBe(false);
    expect(fit.lines).toBe(2);
    // The manager line: a first name, then the record, neither cut short.
    for (const manager of await page
      .locator('[data-testid$="-manager"]:visible:not([inert] *)')
      .all()) {
      const cut = await manager.evaluate((el) => el.scrollWidth > el.clientWidth + 1);
      expect(cut).toBe(false);
      await expect(manager).toHaveText(/^\p{L}[\p{L}'’-]+$/u);
    }
    // Every card the same height.
    const heights = new Set<number>();
    for (const c of await cards.all()) heights.add(Math.round((await c.boundingBox())!.height));
    expect(heights.size).toBe(1);
  });
});
