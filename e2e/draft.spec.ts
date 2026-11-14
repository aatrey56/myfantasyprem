import { expect, test } from './fixtures';

import {
  DRAFT_LEAGUE,
  DUO_LEAGUE,
  PREDRAFT_LEAGUE,
  pageOverflow,
  signIn,
  startWithScenario,
  wheel,
} from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

test.describe('draft room, desktop 1280', () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test('make a pick and see it on the board', async ({ page }) => {
    await page.goto(`${DRAFT_LEAGUE}/draft`);
    await expect(page.getByTestId('draft-clock-title')).toHaveText("You're on the clock");
    await expect(page.getByTestId('pick-timer')).toHaveText(/^\d:\d\d$/);
    // Board and players side by side.
    await expect(page.getByTestId('draft-board')).toBeVisible();
    await expect(page.getByTestId('draft-players')).toBeVisible();
    await expect(page.getByTestId('board-current')).toBeVisible();
    expect(await pageOverflow(page)).toBeLessThanOrEqual(0);

    const pickButton = page
      .locator('[data-testid^="draft-pick-plr_"]:not([aria-disabled="true"])')
      .first();
    const playerId = (await pickButton.getAttribute('data-testid'))!.replace('draft-pick-', '');
    await pickButton.click();

    await expect(page.getByTestId(`board-pick-${playerId}`)).toBeVisible();
    await expect(page.getByTestId('toast')).toContainText('You drafted');
    await expect(page.getByTestId('ticker-latest')).toBeVisible();
    // The clock moves on to the next team, and the player leaves the list.
    await expect(page.getByTestId('draft-clock-title')).not.toHaveText("You're on the clock");
    await expect(page.getByTestId(`draft-player-${playerId}`)).toHaveCount(0);
  });

  test('queue players, reorder them and turn on autopick', async ({ page }) => {
    await page.goto(`${DRAFT_LEAGUE}/draft`);
    const toggles = page.locator('[data-testid^="queue-toggle-plr_"]');
    await expect(toggles.first()).toBeVisible();
    const first = (await toggles.nth(0).getAttribute('data-testid'))!.replace('queue-toggle-', '');
    const second = (await toggles.nth(1).getAttribute('data-testid'))!.replace('queue-toggle-', '');
    await toggles.nth(0).click();
    await toggles.nth(1).click();
    await page.getByTestId('pane-queue').click();
    const items = page.locator('[data-testid^="queue-item-"]');
    await expect(items).toHaveCount(2);
    await expect(items.nth(0)).toHaveAttribute('data-testid', `queue-item-${first}`);
    await page.getByTestId(`queue-up-${second}`).click();
    await expect(items.nth(0)).toHaveAttribute('data-testid', `queue-item-${second}`);
    await page.getByTestId(`queue-remove-${first}`).click();
    await expect(items).toHaveCount(1);
    await page.getByTestId('autopick-switch').click();
    await expect(page.getByTestId('autopick-indicator')).toBeVisible();
  });

  test('position filters and search narrow the list', async ({ page }) => {
    await page.goto(`${DRAFT_LEAGUE}/draft`);
    await page.getByTestId('filter-GKP').click();
    const rows = page.locator('[data-testid^="draft-player-plr_"]');
    await expect(rows.first()).toBeVisible();
    for (const label of (await rows.allTextContents()).slice(0, 10)) {
      expect(label).toContain('GK');
    }
    await page.getByTestId('draft-search').fill('zzzz-nobody');
    await expect(page.getByTestId('draft-players-empty')).toBeVisible();
  });
});

test.describe('draft room, phone 375', () => {
  test.use({ viewport: { width: 375, height: 812 } });

  test('tabs switch between board, players and queue; a pick lands on the board', async ({
    page,
  }) => {
    await page.goto(`${DRAFT_LEAGUE}/draft`);
    await expect(page.getByTestId('draft-board')).toBeVisible();
    expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
    await page.getByTestId('pane-players').click();
    const pickButton = page
      .locator('[data-testid^="draft-pick-plr_"]:not([aria-disabled="true"])')
      .first();
    await expect(pickButton).toBeVisible();
    expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
    const playerId = (await pickButton.getAttribute('data-testid'))!.replace('draft-pick-', '');
    await pickButton.click();
    await expect(page.getByTestId('toast')).toContainText('You drafted');
    await page.getByTestId('pane-board').click();
    await expect(page.getByTestId(`board-pick-${playerId}`)).toBeAttached();
  });
});

test.describe('draft room, phone 390', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('the players controls are a solid header: scrolled rows never show through', async ({
    page,
  }) => {
    await page.goto(`${DRAFT_LEAGUE}/draft`);
    await page.getByTestId('pane-players').click();
    const controls = page.getByTestId('draft-players-controls');
    const list = page.getByTestId('draft-players');
    await expect(page.locator('[data-testid^="draft-player-plr_"]').first()).toBeVisible();

    // Scroll the list well past the controls (the phone room scrolls as one page).
    const lb = (await list.boundingBox())!;
    await wheel(page, lb.x + lb.width / 2, Math.min(lb.y + lb.height / 2, 800), 0, 600);
    await expect
      .poll(() =>
        list.evaluate((el) => {
          let max = 0;
          for (let n: Element | null = el; n; n = n.parentElement) max = Math.max(max, n.scrollTop);
          return max;
        }),
      )
      .toBeGreaterThan(200);

    const box = (await controls.boundingBox())!;
    const listBox = (await list.boundingBox())!;
    // The header spans the whole panel width.
    expect(box.x).toBeLessThanOrEqual(listBox.x + 1);
    expect(box.x + box.width).toBeGreaterThanOrEqual(listBox.x + listBox.width - 1);
    // Every point inside the header hits header content painted on an opaque
    // background: no list row is hit, and none can show through.
    const strays = await controls.evaluate((header) => {
      const opaque = (el: Element) => {
        const m = getComputedStyle(el).backgroundColor.match(/[\d.]+/g) ?? [];
        return m.length === 3 || (m.length === 4 && Number(m[3]) === 1);
      };
      const r = header.getBoundingClientRect();
      const out: string[] = [];
      for (let i = 1; i < 12; i++) {
        for (let j = 1; j < 8; j++) {
          const x = r.left + (r.width * i) / 12;
          const y = r.top + (r.height * j) / 8;
          const hit = document.elementFromPoint(x, y);
          let painted = false;
          for (let n = hit; n && header.contains(n); n = n.parentElement) {
            if (opaque(n)) painted = true;
          }
          if (!hit || !header.contains(hit) || !painted) {
            const what = hit?.closest('[data-testid]')?.getAttribute('data-testid');
            out.push(
              `${Math.round(x)},${Math.round(y)}: ${what}${painted ? '' : ' (see-through)'}`,
            );
          }
        }
      }
      return out;
    });
    expect(strays).toEqual([]);
  });

  test('the page scrolls; the tabs, clock and players controls stick at the top', async ({
    page,
  }) => {
    await page.goto(`${DRAFT_LEAGUE}/draft`);
    await page.getByTestId('pane-players').click();
    const rows = page.locator('[data-testid^="draft-player-plr_"]');
    await expect(rows.first()).toBeVisible();
    const tabs = page.getByRole('tablist', { name: 'Draft room panels' });
    const top = async () => (await tabs.boundingBox())!.y;
    expect(await top()).toBeGreaterThan(100);

    // The header and the ticker scroll away; the tabs stop at the top.
    await wheel(page, 195, 700, 0, 500);
    await expect.poll(top).toBeLessThanOrEqual(12);
    await expect(page.getByTestId('draft-back')).not.toBeInViewport();
    // The page is the only scroller: no inner box around the rows.
    expect(
      await page
        .getByTestId('draft-players')
        .evaluate((el) =>
          [el, ...el.querySelectorAll('*')].some(
            (n) =>
              n.scrollHeight > n.clientHeight + 1 &&
              /auto|scroll/.test(getComputedStyle(n).overflowY),
          ),
        ),
    ).toBe(false);

    // More scrolling moves the rows, not the sticky parts.
    const clockY = (await page.getByTestId('pick-timer').boundingBox())!.y;
    const firstRow = await rows.first().evaluate((el) => el.getBoundingClientRect().top);
    await wheel(page, 195, 700, 0, 600);
    await expect
      .poll(() => rows.first().evaluate((el) => el.getBoundingClientRect().top))
      .toBeLessThan(firstRow - 300);
    expect(await top()).toBeLessThanOrEqual(12);
    expect((await page.getByTestId('pick-timer').boundingBox())!.y).toBeCloseTo(clockY, 0);
    await expect(page.getByTestId('draft-clock-title')).toHaveText("You're on the clock");

    // At least 8 whole rows on screen below the sticky parts, none behind them.
    const sticky = (await page.getByTestId('draft-players-controls').boundingBox())!;
    const below = sticky.y + sticky.height;
    const boxes = await rows.evaluateAll((els) =>
      els.map((el) => {
        const r = el.getBoundingClientRect();
        return { top: r.top, bottom: r.bottom };
      }),
    );
    const whole = boxes.filter((b) => b.top >= below - 1 && b.bottom <= 844 + 1);
    expect(whole.length).toBeGreaterThanOrEqual(8);
    // A point just under the controls lands on a row; one inside them never does.
    const hit = (y: number) =>
      page.evaluate(
        (yy) =>
          document.elementFromPoint(195, yy)?.closest('[data-testid]')?.getAttribute('data-testid'),
        y,
      );
    // (A row's body is its "Player card" button, `open-card-…`.)
    expect(await hit(below + 20)).toMatch(/^draft-(player|pick)|^queue-toggle|^open-card-/);
    for (const y of [sticky.y + 4, sticky.y + sticky.height / 2, below - 4]) {
      expect(await hit(y)).not.toMatch(/^draft-player-|^open-card-/);
    }

    // Another tab starts at its top, the tabs still stuck.
    await page.getByTestId('pane-roster').click();
    await expect(page.getByTestId('draft-roster')).toBeVisible();
    await expect.poll(top).toBeLessThanOrEqual(12);
    const roster = (await page.getByTestId('draft-roster').boundingBox())!;
    expect(roster.y).toBeGreaterThan((await page.getByTestId('pick-timer').boundingBox())!.y);
    expect(roster.y).toBeLessThan(150);
  });

  test('the Queue tab shows its count on one line, untruncated', async ({ page }) => {
    await page.goto(`${DRAFT_LEAGUE}/draft`);
    await page.getByTestId('pane-players').click();
    const toggles = page.locator('[data-testid^="queue-toggle-plr_"]');
    await expect(toggles.first()).toBeVisible();
    // A two-digit queue: the widest count a phone tab has to fit.
    for (let i = 0; i < 12; i++) {
      const toggle = toggles.nth(i);
      await toggle.click();
      await expect(toggle).toHaveAttribute('aria-label', /^Remove .* from queue$/);
    }
    const tab = page.getByTestId('pane-queue');
    await expect(tab).toContainText('12');
    const label = tab.getByText('Queue', { exact: true });
    await expect(label).toBeVisible();
    const fit = await tab.evaluate((el) =>
      [...el.querySelectorAll<HTMLElement>('*')]
        .filter((n) => n.childElementCount === 0 && n.textContent)
        .map((n) => ({
          text: n.textContent,
          truncated: n.scrollWidth > n.clientWidth + 1,
          lines: Math.round(
            n.getBoundingClientRect().height / parseFloat(getComputedStyle(n).lineHeight),
          ),
          inside: n.getBoundingClientRect().right <= el.getBoundingClientRect().right + 1,
        })),
    );
    expect(fit.map((f) => f.text).join(' ')).toMatch(/^Queue\b.*12$/);
    for (const f of fit) expect(f).toMatchObject({ truncated: false, lines: 1, inside: true });
  });
});

test.describe('draft room, phone 320', () => {
  test.use({ viewport: { width: 320, height: 700 } });

  test('a queued row and its controls fit the screen', async ({ page }) => {
    await page.goto(`${DRAFT_LEAGUE}/draft`);
    await page.getByTestId('pane-players').click();
    const toggles = page.locator('[data-testid^="queue-toggle-plr_"]');
    await expect(toggles.first()).toBeVisible();
    const id = (await toggles.nth(0).getAttribute('data-testid'))!.replace('queue-toggle-', '');
    await toggles.nth(0).click();
    await toggles.nth(1).click();
    await page.getByTestId('pane-queue').click();
    await expect(page.locator('[data-testid^="queue-item-"]')).toHaveCount(2);
    // Every control of the row sits fully inside the 320px viewport.
    for (const testId of [`queue-up-${id}`, `queue-down-${id}`, `queue-remove-${id}`]) {
      const box = await page.getByTestId(testId).boundingBox();
      expect(box).not.toBeNull();
      expect(box!.x).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width).toBeLessThanOrEqual(320);
    }
    const label = await page.getByTestId(`queue-up-${id}`).getAttribute('aria-label');
    const playerName = label!.replace(/^Move (.*) up$/, '$1');
    const row = page.getByTestId(`queue-item-${id}`);
    const rowBox = (await row.boundingBox())!;
    expect(rowBox.x + rowBox.width).toBeLessThanOrEqual(320);
    // Nothing in the row spills past it, and the name keeps readable room.
    const spill = await row.evaluate((el) => {
      const right = el.getBoundingClientRect().right + 1;
      return [...el.querySelectorAll<HTMLElement>('*')]
        .filter((c) => c.getBoundingClientRect().right > right)
        .map((c) => c.textContent);
    });
    expect(spill).toEqual([]);
    const name = row.getByText(playerName, { exact: true });
    expect((await name.boundingBox())!.width).toBeGreaterThanOrEqual(
      Math.min(80, await name.evaluate((el) => el.scrollWidth)) - 1, // sub-pixel
    );
    expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
  });
});

test.describe('draft room without the league', () => {
  test('shows the error with retry and a way back instead of loading forever', async ({ page }) => {
    await startWithScenario(page, 'league-404');
    await page.goto(`${DRAFT_LEAGUE}/draft`);
    await expect(page.getByTestId('query-error')).toBeVisible();
    await expect(page.getByTestId('query-error')).toContainText('League not found.');
    await expect(page.getByTestId('retry')).toBeVisible();
    await page.getByTestId('draft-back').click();
    await expect(page).toHaveURL(new RegExp(`${DRAFT_LEAGUE}$`));
  });
});

test('Start is enabled with 5 of 8 managers, and says what happens to the empty places', async ({
  page,
}) => {
  await page.goto(`${PREDRAFT_LEAGUE}/draft`);
  const start = page.getByTestId('draft-start');
  await expect(start).toBeVisible();
  await expect(start).toBeEnabled();
  await expect(page.getByTestId('draft-start-reason')).toHaveText(
    '5 of 8 teams have a manager. The 3 open seats are autopicked; anyone who joins later takes one over.',
  );
});

test('with 2 of 6 managers Start is enabled and the draft starts, open seats autopicking', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto(`${DUO_LEAGUE}/draft`);
  await expect(page.getByTestId('draft-start-reason')).toHaveText(
    '2 of 6 teams have a manager. The 4 open seats are autopicked; anyone who joins later takes one over.',
  );
  const start = page.getByTestId('draft-start');
  await expect(start).toBeEnabled();
  await start.click();
  await expect(page.getByTestId('draft-start')).toHaveCount(0);
  await expect(page.getByTestId('draft-clock-title')).toBeVisible();
  await expect(page.getByTestId('draft-board')).toContainText('Open seat 1');
  await expect(page.getByTestId('draft-board')).toContainText('Open seat 4');
  expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
});

test('Start is disabled, with the reason, while only the commissioner has a team', async ({
  page,
}) => {
  await page.goto('/leagues/new');
  await page.getByTestId('league-name-input').fill('Just Me League');
  await page.getByTestId('create-league-submit').click();
  await page.getByTestId('go-to-league').click();
  await expect(page.getByTestId('preseason-start-note')).toHaveText(
    'At least 2 teams need a manager before the draft can start.',
  );
  // In-app navigation: a reload would reset the mocks.
  await page.getByTestId('enter-draft-room').click();
  await expect(page.getByTestId('draft-start')).toBeDisabled();
  await expect(page.getByTestId('draft-start-reason')).toHaveText(
    'At least 2 teams need a manager before the draft can start.',
  );
});

test('a missed clock is announced in the room and the activity feed', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto(`${DRAFT_LEAGUE}/draft`);
  const banner = page.getByTestId('missed-clock-banner');
  await expect(banner).toContainText(
    /'s clock ran out: autopicked .+ \(best available by rank\)\./,
  );
  await page.getByTestId('pane-queue').click();
  const timeout = page.getByRole('switch', { name: 'Autopick when my clock runs out' });
  await expect(timeout).toBeChecked();
  await timeout.click();
  await expect(timeout).not.toBeChecked();
  await expect(page.getByTestId('draft-queue')).toContainText('next pick gets a full clock');
  await banner.getByRole('button', { name: 'Dismiss' }).click();
  await expect(banner).toHaveCount(0);

  await page.goto(DRAFT_LEAGUE.replace(/$/, '/activity'));
  const feed = page.getByTestId('activity-feed');
  await expect(feed).toContainText('Clock ran out');
  await expect(feed).toContainText(/Autopicked by rank · round \d+, pick \d+ overall/);
});
