import { expect, test, type Locator, type Page } from './fixtures';

import { LEAGUE, overflowingElements, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) => page.locator(`[data-testid="${testId}"]:visible`);

/** Opens the card of the first free agent on the Players tab. */
async function openFreeAgent(page: Page) {
  await page.goto(`${LEAGUE}/players`);
  const add = page.locator('[data-testid^="move-"]:visible[aria-label^="Add "]').first();
  await expect(add).toBeVisible();
  const faId = (await add.getAttribute('data-testid'))!.replace('move-', '');
  await page.locator(`[data-testid="player-${faId}"]:visible`).click();
  const card = visible(page, 'player-card');
  await expect(card).toBeVisible();
  await expect(card.getByTestId('card-name')).toBeVisible();
  return card;
}

/** Waits for a phone sheet to finish sliding up (it fills the window to just under the top). */
const settled = (card: Locator) =>
  expect.poll(async () => (await card.boundingBox())!.y).toBeLessThan(20);

const headers = (page: Page) =>
  page.locator('[data-testid="player-card"]:visible [data-testid^="log-head-"]').allTextContents();

test.describe('player card (desktop)', () => {
  test.use({ viewport: { width: 1280, height: 900 } });

  test('shows the game log as a heat map with column groups', async ({ page }) => {
    const card = await openFreeAgent(page);
    expect((await card.boundingBox())!.width).toBeGreaterThan(800);
    await expect(card.getByTestId('card-add')).toHaveText('Sign');
    await expect(card.getByTestId('card-owner')).toContainText('Free agent');
    await expect(card.getByTestId('card-tiles')).not.toContainText('ICT');
    // One close on wide screens, in the hero (no floating one).
    await expect(card.getByLabel('Close', { exact: true })).toHaveCount(1);
    await expect(card.getByTestId('card-hero').getByTestId('sheet-close')).toBeVisible();

    await card.getByTestId('card-tab-log').click();
    await expect(card.getByTestId('card-log')).toBeVisible();
    for (const group of ['fantasy', 'attack', 'defence', 'discipline', 'ict']) {
      await expect(card.getByTestId(`log-group-${group}`)).toHaveCount(1);
    }
    // Every column, group by group (saves and penalties saved only for keepers).
    expect((await headers(page)).filter((h) => h !== 'SV' && h !== 'PS')).toEqual([
      ...['PTS', 'MIN', 'BON', 'BPS'],
      ...['G', 'A', 'xG', 'xA', 'xGI'],
      ...['CS', 'GC', 'xGC', 'DC', 'CBIT', 'REC', 'TKL'],
      ...['YC', 'RC'],
      ...['I', 'C', 'T', 'ICT'],
    ]);
    await expect(card.getByTestId('card-log')).toContainText('GW');
    await expect(card.getByTestId('card-log')).toContainText('OPP');
    // Expected stats read with two decimals, or a dash for a blank gameweek.
    for (const cell of await card.getByTestId('log-expectedGoals').all()) {
      await expect(cell).toHaveText(/^(\d+\.\d\d|–)$/);
    }
    const row = card.getByTestId('log-row-1');
    await expect(row).toBeVisible();
    await expect(row).toHaveAttribute('aria-expanded', 'false');
    // Expected stats read with two decimals; counts as whole numbers.
    await expect(row.getByTestId('log-expectedGoals')).toHaveText(/^\d+\.\d\d$/);
    await expect(row.getByTestId('log-cbit')).toHaveText(/^(\d+|–)$/);
    await row.click();
    await expect(card.getByTestId('log-detail-1')).toBeVisible();
    expect(await overflowingElements(page)).toEqual([]);
  });

  test('Sign opens the drop step', async ({ page }) => {
    const card = await openFreeAgent(page);
    await card.getByTestId('card-add').click();
    await expect(visible(page, 'drop-step')).toBeVisible();
    await expect(visible(page, 'drop-confirm')).toHaveText('Add and drop');
    // No hero on the drop step: its close sits above it.
    await expect(card.getByLabel('Close', { exact: true })).toHaveCount(1);
    await card.getByTestId('sheet-close').click();
    await expect(card).toBeHidden();
  });

  test('Escape closes the card', async ({ page }) => {
    const card = await openFreeAgent(page);
    await page.keyboard.press('Escape');
    await expect(card).toBeHidden();
  });
});

/** The outline colour of a drawn focus ring, or null with none. */
const ring = (el: Element) => {
  const s = getComputedStyle(el);
  return s.outlineStyle !== 'none' && parseFloat(s.outlineWidth) > 0 ? s.outlineColor : null;
};

test.describe('player card (phone 390)', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('overview labels the points column and reads minutes as appearances', async ({ page }) => {
    const card = await openFreeAgent(page);
    const season = card.getByTestId('card-season');
    await expect(season).toBeVisible();
    await expect(card.getByTestId('card-season-head')).toHaveText(/Pts$/);
    const apps = card.getByTestId('season-row-minutes');
    await expect(apps).toHaveText(/^Appearances\s*\d+ min · \d+ starts?\s*\d+\s*\+\d+$/);
    await expect(apps).toHaveAttribute(
      'aria-label',
      /^Appearances: \d+, \d+ minutes, \d+ starts?, \d+ points$/,
    );
    await expect(card).not.toContainText('Minutes played');
    await expect(card.getByTestId('card-strip')).toBeVisible();
    // One close on phones: the round button over the bottom-right corner.
    await expect(card.getByLabel('Close', { exact: true })).toHaveCount(1);
    await expect(card.getByTestId('card-hero').getByTestId('sheet-close')).toHaveCount(0);
    // Measured once the card has slid all the way up.
    await settled(card);
    const box = (await card.boundingBox())!;
    const close = (await card.getByTestId('sheet-close').boundingBox())!;
    expect(close.width).toBe(56);
    expect(box.x + box.width - (close.x + close.width)).toBeLessThan(32);
    expect(box.y + box.height - (close.y + close.height)).toBeLessThan(32);
    // No season and refresh-time footer.
    await expect(card).not.toContainText('updated');
    await expect(card).not.toContainText(/Season \d{4}-\d\d/);
    expect(await overflowingElements(page)).toEqual([]);
  });

  test('the close button shows a focus ring for the keyboard only', async ({ page }) => {
    await openFreeAgent(page);
    const close = visible(page, 'sheet-close');
    // The sheet moves focus to Close when it opens (on a phone, after a tap):
    // that is not keyboard focus, so no ring.
    await close.evaluate((el: HTMLElement) => el.focus());
    await expect(close).toBeFocused();
    expect(await close.evaluate(ring)).toBeNull();
    // Keyboard focus draws the amber ring.
    await page.keyboard.press('Tab');
    await page.keyboard.press('Shift+Tab');
    await expect(close).toBeFocused();
    await expect.poll(() => close.evaluate(ring)).toBe('rgb(255, 176, 32)');
  });

  test('opens full height and closes by the round button', async ({ page }) => {
    const card = await openFreeAgent(page);
    // Full height: from just under the top of the window to its bottom.
    await settled(card);
    const box = (await card.boundingBox())!;
    expect(box.y + box.height).toBeCloseTo(844, 0);
    await card.getByTestId('sheet-close').click();
    await expect(card).toBeHidden();
  });

  test('Escape closes the card', async ({ page }) => {
    const card = await openFreeAgent(page);
    await page.keyboard.press('Escape');
    await expect(card).toBeHidden();
  });

  test('a mouse drag on the grabber springs back when short and closes when long', async ({
    page,
  }) => {
    const card = await openFreeAgent(page);
    const grabber = card.getByTestId('sheet-grabber');
    await settled(card);
    const rest = (await card.boundingBox())!.y;
    const drag = async (dy: number) => {
      const g = (await grabber.boundingBox())!;
      const x = g.x + g.width / 2;
      const y = g.y + g.height / 2;
      await page.mouse.move(x, y);
      await page.mouse.down();
      await page.mouse.move(x, y + dy / 2, { steps: 8 });
      await page.mouse.move(x, y + dy, { steps: 8 });
      return async () => page.mouse.up();
    };
    // The sheet follows the pointer, then springs back.
    const release = await drag(60);
    await expect.poll(async () => (await card.boundingBox())!.y).toBeGreaterThan(rest + 40);
    await release();
    await expect.poll(async () => (await card.boundingBox())!.y).toBeLessThan(rest + 1);
    await expect(card).toBeVisible();
    // A long pull closes it.
    await (
      await drag(400)
    )();
    await expect(card).toBeHidden();
  });

  test('the drop step keeps the one round close', async ({ page }) => {
    const card = await openFreeAgent(page);
    await card.getByTestId('card-add').click();
    await expect(visible(page, 'drop-step')).toBeVisible();
    await expect(card.getByLabel('Close', { exact: true })).toHaveCount(1);
    expect((await card.getByTestId('sheet-close').boundingBox())!.width).toBe(56);
    await card.getByTestId('sheet-close').click();
    await expect(card).toBeHidden();
  });
});

test.describe('player card (phone)', () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test('expands a game log row to every stat, defensive included', async ({ page }) => {
    const card = await openFreeAgent(page);
    await expect(card.getByTestId('card-add')).toHaveText('Sign');
    await expect(card.getByTestId('card-tiles')).not.toContainText('ICT');
    await card.getByTestId('card-tab-log').click();
    // The heat map scrolls sideways inside the card.
    await expect(card.getByTestId('card-log-scroll')).toBeVisible();

    const rows = card.locator('[data-testid^="log-row-"]');
    const newest = rows.first();
    // Every row starts collapsed.
    await expect(newest).toHaveAttribute('aria-expanded', 'false');
    await expect(card.locator('[data-testid^="log-detail-"]')).toHaveCount(0);
    await newest.click();
    await expect(newest).toHaveAttribute('aria-expanded', 'true');
    const row = rows.nth(2);
    const gw = (await row.getAttribute('data-testid'))!.replace('log-row-', '');
    await expect(row).toHaveAttribute('aria-expanded', 'false');
    await expect(card.getByTestId(`log-detail-${gw}`)).toHaveCount(0);

    await row.click();
    await expect(row).toHaveAttribute('aria-expanded', 'true');
    await expect(newest).toHaveAttribute('aria-expanded', 'false');
    const detail = card.getByTestId(`log-detail-${gw}`);
    await expect(detail).toBeVisible();
    await expect(detail.getByTestId('log-detail-scoring')).toContainText('Yellow cards');
    await expect(detail.getByTestId('log-detail-underlying')).toContainText('xGI');
    const defensive = detail.getByTestId('log-detail-defensive');
    await defensive.scrollIntoViewIfNeeded();
    await expect(defensive).toBeVisible();
    await expect(defensive).toContainText('Defensive');
    for (const label of ['CBIT', 'Recoveries', 'Tackles']) {
      await expect(defensive.getByLabel(new RegExp(`^${label}: (\\d+|–)$`))).toHaveCount(1);
    }

    await row.click();
    await expect(detail).toHaveCount(0);
    expect(await overflowingElements(page)).toEqual([]);
  });

  test('scrolled game log columns never show left of or under the pinned GW and OPP', async ({
    page,
  }) => {
    const card = await openFreeAgent(page);
    await settled(card);
    await expect(card.getByTestId('card-tags-help')).toContainText('Private labels only you');
    await card.getByTestId('card-tab-log').click();
    const scroll = card.getByTestId('card-log-scroll');
    await expect(scroll).toBeVisible();
    await scroll.evaluate((el) => {
      // The ScrollView's scrolling node on web.
      const node = [el, ...el.querySelectorAll('*')].find(
        (n) => n.scrollWidth > n.clientWidth + 1,
      ) as HTMLElement;
      node.scrollLeft = 160;
      node.dispatchEvent(new Event('scroll'));
    });
    const log = (await card.getByTestId('card-log').boundingBox())!;
    const pinned = (await card.getByTestId('log-pinned-head').boundingBox())!;
    // Opaque from the table's left edge to past OPP.
    await expect
      .poll(async () => (await card.getByTestId('log-pinned-head').boundingBox())!.x)
      .toBeLessThanOrEqual(log.x + 1.5);
    expect(pinned.width).toBeGreaterThan(100);
    // Whatever is at the left edge and under GW/OPP is pinned content, not a tile or header.
    const leaks = await page.evaluate(
      ({ x0, x1, top, bottom }) => {
        const out: string[] = [];
        for (let x = x0 + 2; x < x1 - 2; x += 8) {
          for (let y = top + 4; y < bottom - 4; y += 10) {
            const el = document.elementFromPoint(x, y);
            const id = el?.closest('[data-testid^="log-"]')?.getAttribute('data-testid') ?? '';
            if (/^log-(group-|head-(?!$))/.test(id) || /^log-(?!row|blank|next|pinned)/.test(id)) {
              out.push(`${id}@${x},${y}`);
            }
          }
        }
        return out;
      },
      {
        x0: log.x,
        x1: pinned.x + pinned.width,
        top: log.y,
        bottom: log.y + Math.min(log.height, 300),
      },
    );
    expect(leaks).toEqual([]);
  });

  test('the summary strip splits into three equal thirds', async ({ page }) => {
    const card = await openFreeAgent(page);
    const widths = await Promise.all(
      ['Rank', 'Pts / GW', 'Returns'].map(
        async (l) => (await card.getByTestId(`card-strip-${l}`).boundingBox())!.width,
      ),
    );
    expect(Math.max(...widths) - Math.min(...widths)).toBeLessThan(1);
  });
});
