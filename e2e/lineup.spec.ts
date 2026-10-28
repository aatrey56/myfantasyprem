import { expect, test, type Locator, type Page } from './fixtures';

import { LEAGUE, pageOverflow, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

/** The pitch is the default view. */
const openPitch = (page: Page) => expect(page.getByTestId('pitch-view')).toBeVisible();

const XI = '[data-testid="pitch-xi"] [data-testid^="lineup-player-"]:visible:not([inert] *)';
/** The XI's goalkeeper line, at the bottom of the pitch. */
const GK = '[data-testid="pitch-line-GKP"] [data-testid^="lineup-player-"]:visible:not([inert] *)';
const BENCH = '[data-testid="bench-row"] [data-testid^="lineup-player-"]:visible:not([inert] *)';

const idOf = async (page: Page, selector: string, index: number) =>
  (await page.locator(selector).nth(index).getAttribute('data-testid'))!.replace(
    'lineup-player-',
    '',
  );
/** "Swap A. Gustavsen" → "A. Gustavsen". */
const nameOf = async (player: Locator) =>
  (await player.getAttribute('aria-label'))!.replace(/^Swap /, '');
const isSelected = (player: Locator) => expect(player).toHaveAttribute('aria-pressed', 'true');
const notSelected = (player: Locator) => expect(player).not.toHaveAttribute('aria-pressed', 'true');

for (const vp of [
  { name: 'phone', width: 375, height: 812 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`lineup (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('tapping two players swaps them: starting keeper for the backup', async ({ page }) => {
      await page.goto(`${LEAGUE}/team`);
      await expect(page.getByTestId('lineup-open')).toBeVisible();
      await openPitch(page);
      await expect(page.locator(XI)).toHaveCount(11);
      await expect(page.locator(BENCH)).toHaveCount(4);
      // No helper text, formation or counts: the pitch speaks for itself.
      await expect(
        page.getByText(/Tap a player|auto-sub order|backup goalkeeper|Formation|with…/),
      ).toHaveCount(0);
      await expect(page.getByTestId('formation')).toHaveCount(0);
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
      const keeper = await idOf(page, GK, 0);
      const backup = await idOf(page, BENCH, 0);
      const keeperBtn = page.getByTestId(`lineup-player-${keeper}`);
      await expect(keeperBtn).toHaveAttribute('aria-label', /^Swap .+/);
      await keeperBtn.click();
      await isSelected(keeperBtn);
      // Tapping the selected player again deselects.
      await keeperBtn.click();
      await notSelected(keeperBtn);
      await keeperBtn.click();
      await page.getByTestId(`lineup-player-${backup}`).click();
      await expect(page.getByTestId('toast')).toContainText('Lineup saved');
      expect(await idOf(page, GK, 0)).toBe(backup);
      expect(await idOf(page, BENCH, 0)).toBe(keeper);
      await notSelected(page.getByTestId(`lineup-player-${backup}`));
      await expect(page.getByTestId('lineup-errors')).toHaveCount(0);
    });

    test('an invalid swap is refused with the broken rule and keeps the lineup', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/team`);
      await openPitch(page);
      await expect(page.locator(XI)).toHaveCount(11);
      const keeper = await idOf(page, GK, 0);
      const outfield = await idOf(page, BENCH, 1);
      let saves = 0;
      page.on('request', (r) => {
        if (r.method() === 'PUT' && r.url().includes('/lineup')) saves++;
      });
      await page.getByTestId(`lineup-player-${keeper}`).click();
      // While a keeper is selected, outfield bench players are faded.
      await expect(page.getByTestId(`lineup-player-${outfield}`)).toHaveCSS('opacity', '0.5');
      await page.getByTestId(`lineup-player-${outfield}`).click();
      const errors = page.getByTestId('lineup-errors');
      await expect(errors).toBeVisible();
      await expect(errors).toContainText("That lineup isn't allowed");
      await expect(errors).toContainText(/goalkeeper/i);
      expect(saves).toBe(0);
      expect(await idOf(page, GK, 0)).toBe(keeper);
      await notSelected(page.getByTestId(`lineup-player-${keeper}`));
    });

    test('ⓘ opens the player card without selecting the player', async ({ page }) => {
      await page.goto(`${LEAGUE}/team`);
      await openPitch(page);
      await expect(page.locator(XI)).toHaveCount(11);
      const pitchPlayer = page.locator(XI).nth(5);
      const id = await idOf(page, XI, 5);
      const name = await nameOf(pitchPlayer);
      const info = page.getByRole('button', { name: `Player card for ${name}`, exact: true });
      await expect(info).toBeVisible();
      await info.click();
      const card = page.getByTestId('player-card');
      await expect(card).toBeVisible();
      await expect(card).toContainText(name);
      await notSelected(page.getByTestId(`lineup-player-${id}`));
      await page.getByTestId('sheet-close').click();
      await expect(card).toHaveCount(0);
      // Same on the bench.
      const benchName = await nameOf(page.locator(BENCH).nth(2));
      await page.getByRole('button', { name: `Player card for ${benchName}` }).click();
      await expect(page.getByTestId('player-card')).toContainText(benchName);
      await page.getByTestId('sheet-close').click();
      await expect(page.locator(`${BENCH}[aria-pressed="true"]`)).toHaveCount(0);
      await expect(page.locator(`${XI}[aria-pressed="true"]`)).toHaveCount(0);
    });
  });
}

for (const vp of [
  { name: 'phone', width: 375, height: 812 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`list and swap sheet (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    const PILL = '[data-testid^="lineup-swap-"]';
    const pillId = async (page: Page, section: string, index: number) =>
      (await page
        .getByTestId(section)
        .locator(PILL)
        .nth(index)
        .getAttribute('data-testid'))!.replace('lineup-swap-', '');

    test('the pitch is the default; in the list a position pill opens a sheet of legal swaps only', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/team`);
      await expect(page.getByTestId('pitch-view')).toBeVisible();
      await expect(page.getByTestId('list-view')).toHaveCount(0);
      await page.getByTestId('view-list').click();
      await expect(page.getByTestId('list-view')).toBeVisible();
      await expect(page.getByTestId('pitch-view')).toHaveCount(0);
      await expect(page.getByTestId('swap-hint')).toHaveText(
        'Tap a position to change your lineup',
      );
      await expect(page.getByTestId('list-starters').locator(PILL)).toHaveCount(11);
      await expect(page.getByTestId('list-bench').locator(PILL)).toHaveCount(4);
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
      // Starters run goalkeeper first; the backup keeper is first on the bench.
      const keeper = await pillId(page, 'list-starters', 0);
      const backup = await pillId(page, 'list-bench', 0);
      let saves = 0;
      page.on('request', (r) => {
        if (r.method() === 'PUT' && r.url().includes('/lineup')) saves++;
      });

      // Cancel: nothing changes.
      await page.getByTestId(`lineup-swap-${keeper}`).click();
      const sheet = page.getByTestId('swap-sheet');
      await expect(sheet).toBeVisible();
      await expect(sheet.getByTestId('swap-title')).toHaveText('Swap Player');
      await expect(sheet.getByTestId('swap-title')).toHaveCSS('text-align', 'center');
      await expect(sheet.getByTestId('swap-prompt')).toHaveText('⇅ Choose a player to swap');
      await expect(sheet.locator('[data-testid^="swap-candidate-"]')).toHaveCount(1);
      await expect(sheet.getByTestId(`swap-candidate-${backup}`)).toBeVisible();
      await sheet.getByTestId('sheet-close').click();
      await expect(sheet).toHaveCount(0);
      expect(saves).toBe(0);
      expect(await pillId(page, 'list-starters', 0)).toBe(keeper);

      // Pick the backup: swapped and saved.
      await page.getByTestId(`lineup-swap-${keeper}`).click();
      await page.getByTestId(`swap-candidate-${backup}`).click();
      await expect(page.getByTestId('toast')).toContainText('Lineup saved');
      await expect(page.getByTestId('swap-sheet')).toHaveCount(0);
      expect(await pillId(page, 'list-starters', 0)).toBe(backup);
      expect(await pillId(page, 'list-bench', 0)).toBe(keeper);
      await expect(page.getByTestId('lineup-errors')).toHaveCount(0);
    });

    test('the row opens the player card, not the swap sheet', async ({ page }) => {
      await page.goto(`${LEAGUE}/team`);
      await page.getByTestId('view-list').click();
      const row = page.locator('[data-testid^="lineup-row-"]').nth(3);
      const button = row.getByRole('button', { name: /^Player card for / });
      const name = (await button.getAttribute('aria-label'))!.replace(/^Player card for /, '');
      await button.click();
      await expect(page.getByTestId('player-card')).toContainText(name);
      await expect(page.getByTestId('swap-sheet')).toHaveCount(0);
      await page.getByTestId('sheet-close').click();
    });
  });
}

test('the swap sheet offers exactly the legal swaps for every player', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${LEAGUE}/team`);
  await page.getByTestId('view-list').click();
  const PILL = '[data-testid^="lineup-swap-"]';
  const read = (section: string) =>
    page
      .getByTestId(section)
      .locator(PILL)
      .evaluateAll((els) =>
        els.map((el) => ({
          id: el.getAttribute('data-testid')!.replace('lineup-swap-', ''),
          pos: el.textContent!.trim(),
        })),
      );
  await expect(page.getByTestId('list-starters').locator(PILL)).toHaveCount(11);
  const starters = await read('list-starters');
  const bench = await read('list-bench');
  // 1 GK, 3-5 DEF, 2-5 MID, 1-3 FWD; the backup keeper first on the bench.
  const BOUNDS: Record<string, [number, number]> = {
    GK: [1, 1],
    DEF: [3, 5],
    MID: [2, 5],
    FWD: [1, 3],
  };
  const legal = (xi: typeof starters, subs: typeof bench) =>
    Object.entries(BOUNDS).every(([p, [lo, hi]]) => {
      const n = xi.filter((e) => e.pos === p).length;
      return n >= lo && n <= hi;
    }) && subs[0]?.pos === 'GK';
  expect(legal(starters, bench)).toBe(true);
  for (const sel of [...starters, ...bench]) {
    const selOnBench = bench.includes(sel);
    const want = [...starters, ...bench]
      .filter((o) => o !== sel && (selOnBench || bench.includes(o)))
      .filter((o) => {
        const swap = (e: (typeof starters)[number]) => (e === sel ? o : e === o ? sel : e);
        return legal(starters.map(swap), bench.map(swap));
      })
      .map((o) => o.id)
      .sort();
    await page.getByTestId(`lineup-swap-${sel.id}`).click();
    const sheet = page.getByTestId('swap-sheet');
    if (want.length === 0) {
      // No sheet with nothing to choose: a toast says why.
      await expect(page.getByTestId('toast')).toContainText('No legal swaps for this player');
      await expect(sheet).toHaveCount(0);
      continue;
    }
    await expect(sheet.getByTestId('swap-selected')).toBeVisible();
    const offered = await sheet
      .locator('[data-testid^="swap-candidate-"]')
      .evaluateAll((els) =>
        els.map((el) => el.getAttribute('data-testid')!.replace('swap-candidate-', '')).sort(),
      );
    expect(offered, `swaps offered for ${sel.pos} ${sel.id}`).toEqual(want);
    await sheet.getByTestId('sheet-close').click();
    await expect(sheet).toHaveCount(0);
  }
});

test('swaps with the keyboard: two bench players change places', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto(`${LEAGUE}/team`);
  await openPitch(page);
  await expect(page.locator(BENCH)).toHaveCount(4);
  const [two, three] = [await idOf(page, BENCH, 1), await idOf(page, BENCH, 2)];
  await page.getByTestId(`lineup-player-${two}`).focus();
  await page.keyboard.press('Enter');
  await isSelected(page.getByTestId(`lineup-player-${two}`));
  // Tab passes through the player's ⓘ, then lands on the next bench player.
  await page.keyboard.press('Tab');
  await expect(page.getByTestId(`player-info-${two}`)).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(page.getByTestId(`lineup-player-${three}`)).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByTestId('toast')).toContainText('Lineup saved');
  expect(await idOf(page, BENCH, 1)).toBe(three);
  expect(await idOf(page, BENCH, 2)).toBe(two);
});

for (const vp of [
  { name: 'phone', width: 375, height: 812 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`pitch (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('a drawn half pitch attacking upward: keeper by our goal, forwards at the halfway line', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/team`);
      await openPitch(page);
      await expect(page.locator(XI)).toHaveCount(11);
      const svg = page.getByTestId('pitch-background').locator('svg');
      await expect(svg).toBeVisible();
      const pitch = (await page.getByTestId('pitch-xi').boundingBox())!;
      // Stroked rects in drawing order: touchlines, penalty area, six-yard box, goal.
      const rects = await svg.locator('rect[stroke]').evaluateAll((els) =>
        els.map((el) => {
          const r = el.getBoundingClientRect();
          return { top: r.top, bottom: r.bottom };
        }),
      );
      const [touch, penalty, six] = [rects[0]!, rects[1]!, rects[2]!];
      // Half a pitch: the halfway line is the top touchline, half the centre circle under it.
      expect(touch.top - pitch.y).toBeLessThanOrEqual(10);
      const circle = (await svg.locator('path[stroke]').first().boundingBox())!;
      // Within a stroke or two: the path and rect boxes measure strokes differently.
      expect(Math.abs(circle.y - touch.top)).toBeLessThanOrEqual(5);
      expect(circle.y + circle.height).toBeLessThan(penalty.top);
      const playerBox = async (p: string) =>
        (await page
          .locator(`[data-testid="pitch-line-${p}"] [data-testid^="lineup-player-"]`)
          .first()
          .boundingBox())!;
      const [fwd, mid, def, gk] = [
        await playerBox('FWD'),
        await playerBox('MID'),
        await playerBox('DEF'),
        await playerBox('GKP'),
      ];
      // The keeper in our box by the goal; the forwards up at the top edge.
      expect(gk.y).toBeGreaterThan(penalty.top);
      expect(gk.y + gk.height).toBeLessThanOrEqual(six.bottom + 1);
      expect(fwd.y - pitch.y).toBeLessThanOrEqual(12);
      const gaps = [mid.y - fwd.y, def.y - mid.y, gk.y - def.y];
      expect(Math.max(...gaps) - Math.min(...gaps)).toBeLessThanOrEqual(2);
      // The bench sits below the pitch, numbered 1–4.
      expect((await page.getByTestId('bench-row').boundingBox())!.y).toBeGreaterThan(
        pitch.y + pitch.height,
      );
      await expect(page.getByTestId('bench-row')).toContainText('1');
      await expect(page.getByTestId('pitch-view')).not.toContainText(/auto-sub order|· GK/i);
      // Names sit on dark chips, inside the pitch.
      const names = page.locator(`${XI.replace(':visible', '')} [data-testid="pitch-name"]`);
      const bg = await names
        .first()
        .evaluate((el) => getComputedStyle(el.parentElement!).backgroundColor);
      expect(bg).toBe('rgba(8, 10, 12, 0.82)');
      for (let i = 0; i < (await names.count()); i++) {
        const b = (await names.nth(i).boundingBox())!;
        expect(b.y).toBeGreaterThanOrEqual(pitch.y);
        expect(b.y + b.height).toBeLessThanOrEqual(pitch.y + pitch.height);
        expect(b.x).toBeGreaterThanOrEqual(pitch.x);
        expect(b.x + b.width).toBeLessThanOrEqual(pitch.x + pitch.width);
      }
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
    });
  });
}

test('past gameweeks are locked and show points', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto(`${LEAGUE}/team`);
  await expect(page.getByTestId('gw-label')).toHaveText('GW 8');
  await page.getByTestId('gw-prev').click();
  await expect(page.getByTestId('gw-label')).toHaveText('GW 7');
  await expect(page.getByTestId('lineup-locked')).toBeVisible();
  await expect(page.getByTestId('team-points')).toHaveText(/^\d+$/);
  await openPitch(page);
  // The pitch shows a points box under each name; players are not swap buttons.
  await expect(page.locator('[data-testid="pitch-points"]:visible:not([inert] *)')).toHaveCount(11);
  await expect(page.getByRole('button', { name: /^Swap / })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /^Player card for / })).toHaveCount(15);
  await page.getByTestId('view-list').click();
  await expect(page.locator('[data-testid="player-points"]:visible:not([inert] *)')).toHaveCount(
    15,
  );
  // "Starters" (in the header row) then "Bench", with no counts, no "auto-sub order" and no swap hint.
  await expect(page.getByTestId('lineup-heading')).toHaveText('Starters');
  const list = page.getByTestId('list-view');
  await expect(list).toContainText('Bench');
  await expect(list).not.toContainText(/Starters|Bench\s*\d|auto-sub order/);
  await expect(page.getByTestId('swap-hint')).toHaveCount(0);
  await expect(page.getByTestId('lineup-header')).not.toContainText(/Tap a position/);
  // Locked: rows only open the player card; no position pill is a swap button.
  await expect(page.locator('[data-testid^="lineup-row-"][role="button"]')).toHaveCount(0);
  await expect(page.locator('[data-testid^="lineup-swap-"]')).toHaveCount(0);
  await expect(page.getByRole('button', { name: /^Swap / })).toHaveCount(0);
});
