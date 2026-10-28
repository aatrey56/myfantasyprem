import { expect, test, type Page } from './fixtures';

import { LEAGUE, overflowingElements, signIn, wheel } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) => page.locator(`[data-testid="${testId}"]:visible`);
const visibleStarting = (page: Page, prefix: string) =>
  page.locator(`[data-testid^="${prefix}"]:visible`);

for (const vp of [
  { name: 'phone', width: 375, height: 812, nav: 'league-tabs' },
  { name: 'desktop', width: 1280, height: 800, nav: 'top-nav' },
] as const) {
  test.describe(`status (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('is the tab a league opens on', async ({ page }) => {
      await page.goto(LEAGUE);
      const nav = page.getByTestId(vp.nav);
      await expect(nav.getByRole('tab').first()).toHaveText('Status');
      await expect(nav.getByRole('tab', { name: 'Status' })).toHaveAttribute(
        'aria-selected',
        'true',
      );
      await expect(visible(page, 'status-loaded')).toBeVisible();
      for (const id of ['gw-summary', 'top-available', 'potw', 'totw', 'status-table']) {
        await expect(visible(page, id)).toBeAttached();
      }
      await expect(page.getByRole('heading', { name: 'Injuries and Suspensions' })).toBeAttached();
      await expect(page.getByText('Transactions')).toHaveCount(0);
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('a critical attention item is a solid red stop sign that leads to the team', async ({
      page,
    }) => {
      await page.goto(LEAGUE);
      const item = visible(page, 'attention-starters_without_fixture');
      await expect(item).toContainText(/starters? ha(s|ve) no game in GW 8/);
      expect(await item.evaluate((el) => getComputedStyle(el).backgroundColor)).toBe(
        'rgb(218, 41, 28)',
      );
      await item.click();
      await expect(page).toHaveURL(/\/team$/);
    });

    test('Team of the Week fields 11 players and follows the gameweek', async ({ page }) => {
      await page.goto(LEAGUE);
      const totw = visible(page, 'totw');
      const players = totw.locator('[data-testid^="totw-player-"]');
      await expect(players).toHaveCount(11);
      await expect(totw.getByTestId('gw-label')).toHaveText('GW 6');
      await expect(totw).not.toContainText(/\b[345]-[2-5]-[1-3]\b/);
      const xi = () =>
        players.evaluateAll((els) => els.map((el) => el.getAttribute('data-testid')).join());
      const before = await xi();
      await totw.getByTestId('gw-prev').click();
      await expect(totw.getByTestId('gw-label')).toHaveText('GW 5');
      await expect(players).toHaveCount(11);
      await expect.poll(xi).not.toBe(before);
      // The Team page's pitch: every player over a bordered points box.
      await expect(totw.getByTestId('totw-pitch').getByTestId('pitch-background')).toBeAttached();
      await expect(players.getByTestId('pitch-points')).toHaveCount(11);
      for (const box of await players.getByTestId('pitch-points').all()) {
        await expect(box).toHaveText(/^-?\d+$/);
        await expect(box).toHaveCSS('border-top-color', 'rgb(242, 244, 247)');
      }
      await expect(totw.getByTestId(/^player-info-/)).toHaveCount(0);
      // Tapping a player opens their card.
      const name = (await players.first().getByTestId('pitch-name').textContent())!;
      await players.first().click();
      const card = visible(page, 'player-card');
      await expect(card).toBeVisible();
      await expect(card).toContainText(name);
    });

    test('Player of the Week and Top Available players open the player card', async ({ page }) => {
      await page.goto(LEAGUE);
      const potw = visibleStarting(page, 'potw-card-').last();
      const potwName = (await potw.getByTestId('potw-name').textContent())!;
      await potw.click();
      const card = visible(page, 'player-card');
      await expect(card).toBeVisible();
      await expect(card).toContainText(potwName);
      await page.keyboard.press('Escape');
      await expect(card).toHaveCount(0);

      const top = visible(page, 'top-available').getByRole('button', { name: /^Player card for / });
      const label = (await top.first().getAttribute('aria-label'))!;
      await top.first().click();
      await expect(card).toBeVisible();
      await expect(card).toContainText(label.replace('Player card for ', ''));
    });

    test('an injury badge opens the player card', async ({ page }) => {
      await page.goto(LEAGUE);
      const badge = visibleStarting(page, 'injury-badge-').first();
      await expect(badge).toHaveText('Out');
      const label = (await badge.getAttribute('aria-label'))!;
      const name = /^Player card for (.+?), /.exec(label)![1]!;
      await badge.click();
      const card = visible(page, 'player-card');
      await expect(card).toBeVisible();
      await expect(card).toContainText(name);
    });

    test('Sign opens the free-agent flow on the drop step', async ({ page }) => {
      await page.goto(LEAGUE);
      await visibleStarting(page, 'sign-plr_').first().click();
      const card = visible(page, 'player-card');
      await expect(card).toBeVisible();
      await expect(card.getByTestId('drop-step')).toBeVisible();
    });
  });
}

/** The gameweeks of the Player of the Week cards wholly inside the row, left to right. */
const potwShown = (page: Page) =>
  visible(page, 'potw-row').evaluate((row) => {
    const r = row.getBoundingClientRect();
    return [...row.querySelectorAll('[data-testid^="potw-card-"]')]
      .filter((c) => {
        const b = c.getBoundingClientRect();
        return b.left >= r.left - 1 && b.right <= r.right + 1;
      })
      .map((c) => Number(c.getAttribute('data-testid')!.replace('potw-card-', '')));
  });

for (const vp of [
  { name: 'phone', width: 390, height: 844 },
  { name: 'desktop', width: 1280, height: 800 },
] as const) {
  test.describe(`Player of the Week row (${vp.width}px)`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('shows five cards, the latest gameweek rightmost', async ({ page }) => {
      await page.goto(LEAGUE);
      await expect(visibleStarting(page, 'potw-card-').first()).toBeAttached();
      const all = await visibleStarting(page, 'potw-card-').evaluateAll((cards) =>
        cards.map((c) => Number(c.getAttribute('data-testid')!.replace('potw-card-', ''))),
      );
      const latest = Math.max(...all);
      await expect.poll(() => potwShown(page)).toEqual([4, 3, 2, 1, 0].map((i) => latest - i));
      await expect(visible(page, 'potw-next')).toBeDisabled();
      await expect(visible(page, 'potw-prev')).toBeEnabled();
      // Each card keeps a readable name box and a bordered points box.
      const card = visible(page, `potw-card-${latest}`);
      const box = (await card.boundingBox())!;
      if (vp.width === 390) {
        expect(box.width).toBeGreaterThanOrEqual(60);
        expect(box.width).toBeLessThanOrEqual(80);
      }
      for (const id of ['potw-name', 'potw-points']) {
        const inner = (await card.getByTestId(id).boundingBox())!;
        expect(inner.x).toBeGreaterThanOrEqual(box.x);
        expect(inner.x + inner.width).toBeLessThanOrEqual(box.x + box.width + 0.5);
      }
      await expect(card.getByTestId('potw-name')).toHaveText(/\S/);
      await expect(card.getByTestId('potw-points')).toHaveText(/^-?\d+$/);
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('the arrows move one gameweek at a time and stop at the ends', async ({ page }) => {
      await page.goto(LEAGUE);
      await expect.poll(async () => (await potwShown(page)).length).toBe(5);
      const start = await potwShown(page);
      await visible(page, 'potw-prev').click();
      await expect.poll(() => potwShown(page)).toEqual(start.map((gw) => gw - 1));
      await expect(visible(page, 'potw-next')).toBeEnabled();
      await visible(page, 'potw-next').click();
      await expect.poll(() => potwShown(page)).toEqual(start);
      await expect(visible(page, 'potw-next')).toBeDisabled();
      // Back to the first gameweek: nothing before it.
      for (let gw = start[0]!; gw > 1; gw--) await visible(page, 'potw-prev').click();
      await expect.poll(() => potwShown(page)).toEqual([1, 2, 3, 4, 5]);
      await expect(visible(page, 'potw-prev')).toBeDisabled();
    });

    test('a scroll snaps to exactly one card', async ({ page }) => {
      await page.goto(LEAGUE);
      await expect.poll(async () => (await potwShown(page)).length).toBe(5);
      const start = await potwShown(page);
      const row = visible(page, 'potw-row');
      const step = await row.evaluate((el) => {
        const [a, b] = [...el.querySelectorAll('[data-testid^="potw-card-"]')].map(
          (c) => c.getBoundingClientRect().left,
        );
        return b! - a!;
      });
      const aligned = () =>
        row.evaluate((el, s) => Math.abs(el.scrollLeft / s - Math.round(el.scrollLeft / s)), step);
      // A long wheel flick back moves exactly one card, never between cards.
      await row.scrollIntoViewIfNeeded();
      const b = (await row.boundingBox())!;
      await wheel(page, b.x + b.width / 2, b.y + b.height / 2, -1.5 * step, 0);
      await expect.poll(() => potwShown(page)).toEqual(start.map((gw) => gw - 1));
      await expect.poll(aligned).toBeLessThan(0.02);
      await expect(visible(page, 'potw-next')).toBeEnabled();
      // A partial scroll forward settles on the next card.
      await row.evaluate((el, dx) => el.scrollBy({ left: dx, behavior: 'instant' }), 0.6 * step);
      await expect.poll(() => potwShown(page)).toEqual(start);
      await expect.poll(aligned).toBeLessThan(0.02);
      await expect(visible(page, 'potw-next')).toBeDisabled();
    });
  });
}
