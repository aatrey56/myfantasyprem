import { expect, test, type Locator, type Page } from './fixtures';

import { LEAGUE, overflowingElements, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);

/** Opens the card of the first free agent on the Players tab. */
async function openFreeAgent(page: Page) {
  await page.goto(`${LEAGUE}/players`);
  const add = page
    .locator('[data-testid^="move-"]:visible:not([inert] *)[aria-label^="Add "]')
    .first();
  await expect(add).toBeVisible();
  const faId = (await add.getAttribute('data-testid'))!.replace('move-', '');
  await page.locator(`[data-testid="player-${faId}"]:visible:not([inert] *)`).click();
  const card = visible(page, 'player-card');
  await expect(card).toBeVisible();
  await expect(card.getByTestId('card-name')).toBeVisible();
  return card;
}

/** Waits for a phone sheet to finish sliding up (it fills the window to just under the top). */
const settled = (card: Locator) =>
  expect.poll(async () => (await card.boundingBox())!.y).toBeLessThan(20);

const DATE = /^\d{1,2} [A-Z][a-z]{2}$/;
const WEEKDAY = /^(Mon|Tue|Wed|Thu|Fri|Sat|Sun)$/;

/** The Summary's By gameweek cards (not the day and date inside each). */
const gwCards = (card: Locator) => card.getByTestId(/^gw-card-\d+$/);

/**
 * The game log's pinned GW, OPP and DATE headings sit side by side, whole and
 * level, with each row's date centred under the DATE heading.
 */
async function expectDateColumn(card: Locator) {
  const box = async (l: Locator) => (await l.boundingBox())!;
  const head = card.getByTestId('log-date-head');
  await expect(head).toHaveText(/^date$/i);
  const pinned = await box(card.getByTestId('log-pinned-head'));
  const h = await box(head);
  // Inside the pinned block, which holds GW, OPP and DATE in that order.
  expect(h.x).toBeGreaterThanOrEqual(pinned.x);
  expect(h.x + h.width).toBeLessThanOrEqual(pinned.x + pinned.width);
  const gw = await box(card.getByText('GW', { exact: true }).first());
  const oppHead = await box(card.getByText('OPP', { exact: true }).first());
  expect(gw.x).toBeLessThan(oppHead.x);
  expect(oppHead.x + oppHead.width).toBeLessThanOrEqual(h.x + 1);
  // The heading's text is whole: it needs no more room than it has.
  expect(
    await head.evaluate((el) =>
      [el, ...el.querySelectorAll('*')].every((n) => n.scrollWidth <= n.clientWidth + 1),
    ),
  ).toBe(true);
  const dates = card.locator('[data-testid^="log-date-"]:not([data-testid="log-date-head"])');
  await expect(dates.first()).toBeVisible();
  for (const cell of await dates.all()) {
    const c = await box(cell);
    expect(Math.abs(c.x + c.width / 2 - (h.x + h.width / 2))).toBeLessThanOrEqual(1);
    for (const t of (await cell.locator('div').allTextContents()).filter(Boolean)) {
      expect(t).toMatch(/^(\d{1,2} [A-Z][a-z]{2}|–)+$/);
    }
  }
}

/** Each next-fixture kickoff reads `Sat 22 Nov, 12:30` whole, on one line inside the card. */
async function expectDatedKickoff(next: Locator) {
  const card = (await next.boundingBox())!;
  const kickoffs = next.locator('[data-testid^="card-next-kickoff-"]');
  await expect(kickoffs.first()).toBeVisible();
  for (const k of await kickoffs.all()) {
    await expect(k).toHaveText(
      /^(Mon|Tue|Wed|Thu|Fri|Sat|Sun) \d{1,2} [A-Z][a-z]{2}, \d\d:\d\d$|^(Moved|TBC)$/,
    );
    const b = (await k.boundingBox())!;
    expect(b.height).toBeLessThan(24);
    expect(b.x).toBeGreaterThanOrEqual(card.x);
    expect(b.x + b.width).toBeLessThanOrEqual(card.x + card.width);
  }
}

const headers = (page: Page) =>
  page
    .locator('[data-testid="player-card"]:visible:not([inert] *) [data-testid^="log-head-"]')
    .allTextContents();

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

  test('dates each game log row, right of the opponent, and the Summary cards and next fixture', async ({
    page,
  }) => {
    const card = await openFreeAgent(page);
    // Summary: the next fixture has its date (`Sat 22 Nov, 12:30`), not just the weekday.
    const next = card.getByTestId('card-next');
    if ((await next.count()) > 0) {
      await expectDatedKickoff(next);
    }
    // Each By gameweek card: weekday top right, date bottom right.
    const first = gwCards(card).first();
    await expect(first).toBeVisible();
    const id = (await first.getAttribute('data-testid'))!.replace('gw-card-', '');
    const day = card.getByTestId(`gw-card-${id}-day`);
    const date = card.getByTestId(`gw-card-${id}-date`);
    await expect(day).toHaveText(WEEKDAY);
    await expect(date).toHaveText(DATE);
    const [c, d, t] = [
      await first.boundingBox(),
      await day.boundingBox(),
      await date.boundingBox(),
    ];
    expect(d!.x + d!.width).toBeGreaterThan(c!.x + c!.width / 2);
    expect(t!.x + t!.width).toBeGreaterThan(c!.x + c!.width / 2);
    expect(t!.y).toBeGreaterThan(d!.y + d!.height);
    expect(d!.x + d!.width).toBeLessThanOrEqual(c!.x + c!.width);
    expect(t!.x + t!.width).toBeLessThanOrEqual(c!.x + c!.width);
    // One line each: the existing content is not pushed around.
    expect(d!.height).toBeLessThan(24);
    expect(t!.height).toBeLessThan(24);
    // The same date reads in the game log.
    const cardDate = await date.textContent();
    await card.getByTestId('card-tab-log').click();
    await expectDateColumn(card);
    await expect(card.getByTestId(`log-date-${id}`)).toHaveText(cardDate!);
    expect(await overflowingElements(page)).toEqual([]);
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
    const drag = async (dy: number, steps = 8) => {
      const g = (await grabber.boundingBox())!;
      const x = g.x + g.width / 2;
      const y = g.y + g.height / 2;
      await page.mouse.move(x, y);
      await page.mouse.down();
      await page.mouse.move(x, y + dy / 2, { steps });
      await page.mouse.move(x, y + dy, { steps });
      return async () => page.mouse.up();
    };
    // The sheet follows the pointer, then springs back: a quick short pull,
    // held still, is no fling (the release must not reuse the pull's speed).
    const release = await drag(60, 4);
    await expect.poll(async () => (await card.boundingBox())!.y).toBeGreaterThan(rest + 40);
    await page.waitForTimeout(200);
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

  test('a By gameweek card opens the game log at its row and highlights it', async ({ page }) => {
    const card = await openFreeAgent(page);
    const target = gwCards(card).nth(1);
    await expect(target).toBeVisible();
    const id = (await target.getAttribute('data-testid'))!.replace('gw-card-', '');
    await expect(target).toHaveAttribute('role', 'button');
    await expect(target).toHaveAttribute('aria-label', new RegExp(`^Gameweek ${id}: `));
    await target.click();
    await expect(card.getByTestId('card-log')).toBeVisible();
    const row = card.getByTestId(`log-row-${id}`);
    await expect(row).toHaveAttribute('aria-current', 'true');
    await expect(row).toBeInViewport();
    // Only a moment: the highlight goes, the row stays.
    await expect(row).not.toHaveAttribute('aria-current', 'true');
    await expect(row).toBeVisible();
    // Opening the log by hand later points at nothing.
    await card.getByTestId('card-tab-overview').click();
    await card.getByTestId('card-tab-log').click();
    await expect(card.getByTestId('card-log')).toBeVisible();
    await expect(card.locator('[aria-current="true"]')).toHaveCount(0);
    // Back on the Summary, another tap points at its own row.
    await card.getByTestId('card-tab-overview').click();
    const firstId = (await gwCards(card).first().getAttribute('data-testid'))!.replace(
      'gw-card-',
      '',
    );
    await gwCards(card).first().click();
    await expect(card.getByTestId(`log-row-${firstId}`)).toHaveAttribute('aria-current', 'true');
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
            if (
              /^log-(group-|head-(?!$))/.test(id) ||
              /^log-(?!row|blank|next|pinned|date)/.test(id)
            ) {
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

  test('the game log dates fit beside GW and OPP, and the gameweek cards fit their dates', async ({
    page,
  }) => {
    const card = await openFreeAgent(page);
    await settled(card);
    if ((await card.getByTestId('card-next').count()) > 0) {
      await expectDatedKickoff(card.getByTestId('card-next'));
    }
    const last = gwCards(card).last();
    await last.scrollIntoViewIfNeeded();
    const id = (await last.getAttribute('data-testid'))!.replace('gw-card-', '');
    for (const part of ['day', 'date']) {
      const text = card.getByTestId(`gw-card-${id}-${part}`);
      const [c, t] = [await last.boundingBox(), await text.boundingBox()];
      expect(t!.x).toBeGreaterThanOrEqual(c!.x);
      expect(t!.x + t!.width).toBeLessThanOrEqual(c!.x + c!.width);
      expect(t!.height).toBeLessThan(24);
    }
    await card.getByTestId('card-tab-log').click();
    await expectDateColumn(card);
    // The pinned columns leave most of the width to the stats beside them.
    const log = (await card.getByTestId('card-log').boundingBox())!;
    const pinned = (await card.getByTestId('log-pinned-head').boundingBox())!;
    expect(pinned.x + pinned.width).toBeLessThan(log.x + log.width * 0.6);
    expect(await overflowingElements(page)).toEqual([]);
  });

  test('tapping a gameweek card on a phone scrolls that row into view', async ({ page }) => {
    // A short window, so the newest row is below the fold of a log opened at the top.
    await page.setViewportSize({ width: 375, height: 520 });
    const card = await openFreeAgent(page);
    await settled(card);
    const target = gwCards(card).last();
    await expect(target).toBeAttached();
    const id = (await target.getAttribute('data-testid'))!.replace('gw-card-', '');
    // A click without Playwright's own scroll-to-target: the sheet stays at the top.
    await target.dispatchEvent('click');
    const row = card.getByTestId(`log-row-${id}`);
    await expect(row).toHaveAttribute('aria-current', 'true');
    await expect(row).toBeInViewport();
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
