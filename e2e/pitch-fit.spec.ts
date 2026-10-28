import { expect, test, type Locator, type Page } from './fixtures';

import { LEAGUE, onScreen, pageOverflow, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

/** Mock teams 1–8 of the main league; their lineups cycle through the FPL formations. */
const TEAMS = Array.from(
  { length: 8 },
  (_, i) => `${LEAGUE}/team/tm_01k7m2xq80000000000000000${i + 1}`,
);
const WANTED = ['3-5-2', '4-4-2', '5-4-1', '4-5-1', '3-4-3', '5-3-2'];

type Box = { x: number; y: number; width: number; height: number };
const box = async (l: Locator) => (await l.boundingBox())!;
const overlaps = (a: Box, b: Box) =>
  a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
const inside = (a: Box, o: Box) =>
  a.x >= o.x - 0.5 &&
  a.y >= o.y - 0.5 &&
  a.x + a.width <= o.x + o.width + 0.5 &&
  a.y + a.height <= o.y + o.height + 0.5;

/** The bottom of the usable screen: the app bar's top, else the viewport's bottom. */
async function screenBottom(page: Page, vpHeight: number) {
  const bar = page.locator('[data-testid="app-tabs"]:visible:not([inert] *)');
  return (await bar.count()) ? (await box(bar)).y : vpHeight;
}

/** Scrolled to the top, the pitch and the bench sit above the app bar. */
async function expectPitchAndBenchFit(page: Page, vpHeight: number) {
  const scroller = page.locator('[data-testid="screen-scroll"]:visible:not([inert] *)');
  await scroller.evaluate((el) => {
    el.scrollTop = 0;
  });
  await expect.poll(() => scroller.evaluate((el) => el.scrollTop)).toBe(0);
  const bottom = await screenBottom(page, vpHeight);
  await expect
    .poll(async () => {
      const bench = await box(page.locator('[data-testid="bench"]:visible:not([inert] *)'));
      return bench.y + bench.height;
    })
    .toBeLessThanOrEqual(bottom);
  const pitch = await box(page.locator('[data-testid="pitch-xi"]:visible:not([inert] *)'));
  const bench = await box(page.locator('[data-testid="bench"]:visible:not([inert] *)'));
  expect(pitch.y + pitch.height).toBeLessThanOrEqual(bench.y);
  return pitch;
}

/** "D-M-F" from the players on each line of the visible pitch. */
async function formationOf(page: Page) {
  const n = (k: string) =>
    page
      .locator(
        `[data-testid="pitch-xi"]:visible:not([inert] *) [data-testid="pitch-line-${k}"] [data-testid^="lineup-player-"]`,
      )
      .count();
  return `${await n('DEF')}-${await n('MID')}-${await n('FWD')}`;
}

/** Every visible name chip on the pitch: inside it, apart from each other, not truncated. */
async function expectNamesClear(page: Page) {
  const pitch = await box(page.locator('[data-testid="pitch-xi"]:visible:not([inert] *)'));
  const names = page.locator(
    '[data-testid="pitch-xi"]:visible:not([inert] *) [data-testid="pitch-name"]',
  );
  await expect(names).toHaveCount(11);
  const boxes: Box[] = [];
  for (const name of await names.all()) {
    await expect(name).toBeVisible();
    boxes.push(await box(name));
  }
  for (const b of boxes) expect(inside(b, pitch)).toBe(true);
  for (let i = 0; i < boxes.length; i++)
    for (let j = i + 1; j < boxes.length; j++) expect(overlaps(boxes[i]!, boxes[j]!)).toBe(false);
  // Each line's players sit at equal steps across the pitch.
  for (const k of ['GKP', 'DEF', 'MID', 'FWD']) {
    const players = page.locator(
      `[data-testid="pitch-xi"]:visible:not([inert] *) [data-testid="pitch-line-${k}"] [data-testid^="lineup-player-"]`,
    );
    const xs = await Promise.all(
      (await players.all()).map(async (p) => {
        const b = await box(p);
        return b.x + b.width / 2 - pitch.x;
      }),
    );
    const step = pitch.width / xs.length;
    xs.forEach((x, i) => expect(Math.abs(x - (i + 0.5) * step)).toBeLessThanOrEqual(1.5));
  }
}

for (const vp of [
  { width: 375, height: 667 },
  { width: 390, height: 844 },
]) {
  test.describe(`pitch on a ${vp.width}x${vp.height} phone`, () => {
    test.use({ viewport: vp });

    test('my team opens on the pitch, and pitch plus bench fit without scrolling', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/team`);
      await expect(page.getByTestId('lineup-open')).toBeVisible();
      await expect(page.getByTestId('view-pitch')).toHaveAttribute('aria-checked', 'true');
      const pitchLoc = page.locator('[data-testid="pitch-xi"]:visible:not([inert] *)');
      await expect(pitchLoc).toBeVisible();
      const bottom = await screenBottom(page, vp.height);
      const pitch = await box(pitchLoc);
      const bench = await box(page.locator('[data-testid="bench"]:visible:not([inert] *)'));
      expect(pitch.y).toBeGreaterThanOrEqual(0);
      expect(pitch.y + pitch.height).toBeLessThanOrEqual(bench.y);
      expect(bench.y + bench.height).toBeLessThanOrEqual(bottom);
      expect(bench.x + bench.width).toBeLessThanOrEqual(vp.width);
      expect(await page.evaluate(() => window.scrollY)).toBe(0);
      // Four bench players in one row; every player a 44px tap target.
      const benchPlayers = page.locator(
        '[data-testid="bench-row"] [data-testid^="lineup-player-"]:visible:not([inert] *)',
      );
      await expect(benchPlayers).toHaveCount(4);
      const benchYs = new Set<number>();
      for (const p of await benchPlayers.all()) {
        const b = await box(p);
        benchYs.add(Math.round(b.y));
        expect(b.height).toBeGreaterThanOrEqual(44);
        expect(b.width).toBeGreaterThanOrEqual(44);
      }
      expect(benchYs.size).toBe(1);
      for (const p of await pitchLoc.locator('[data-testid^="lineup-player-"]').all()) {
        const b = await box(p);
        expect(b.height).toBeGreaterThanOrEqual(44);
        expect(b.width).toBeGreaterThanOrEqual(44);
      }
      // GK at the bottom, forwards at the top edge.
      const lineY = async (k: string) =>
        (
          await box(
            pitchLoc
              .locator(`[data-testid="pitch-line-${k}"] [data-testid^="lineup-player-"]`)
              .first(),
          )
        ).y;
      const [gk, def, mid, fwd] = [
        await lineY('GKP'),
        await lineY('DEF'),
        await lineY('MID'),
        await lineY('FWD'),
      ];
      expect(gk).toBeGreaterThan(def);
      expect(def).toBeGreaterThan(mid);
      expect(mid).toBeGreaterThan(fwd);
      expect(Math.abs(gk - def - (def - mid))).toBeLessThanOrEqual(1.5);
      expect(Math.abs(def - mid - (mid - fwd))).toBeLessThanOrEqual(1.5);
      expect(fwd - pitch.y).toBeLessThanOrEqual(12);
      await expectNamesClear(page);
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
      // Swapping still works: the keeper for the backup, both on screen.
      const keeper = pitchLoc
        .locator('[data-testid="pitch-line-GKP"] [data-testid^="lineup-player-"]')
        .first();
      const keeperId = await keeper.getAttribute('data-testid');
      await keeper.click();
      await benchPlayers.first().click();
      await expect(page.getByTestId('toast')).toContainText('Lineup saved');
      await expect(benchPlayers.first()).toHaveAttribute('data-testid', keeperId!);
    });

    test('an errors banner above the pitch shrinks it: the bench stays on screen', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/team`);
      const pitchLoc = page.locator('[data-testid="pitch-xi"]:visible:not([inert] *)');
      await expect(pitchLoc).toBeVisible();
      const before = await expectPitchAndBenchFit(page, vp.height);
      // Keeper for an outfield bench player: refused, the banner shows above the pitch.
      await pitchLoc
        .locator('[data-testid="pitch-line-GKP"] [data-testid^="lineup-player-"]')
        .first()
        .click();
      await page
        .locator('[data-testid="bench-row"] [data-testid^="lineup-player-"]:visible:not([inert] *)')
        .nth(1)
        .click();
      await expect(page.getByTestId('lineup-errors')).toBeVisible();
      // The pitch gives up what the banner pushed it down, down to its minimum
      // (a short phone); above the minimum the bench stays on screen.
      const shift = async () =>
        (await box(page.locator('[data-testid="pitch-xi"]:visible:not([inert] *)'))).y - before.y;
      await expect.poll(shift).toBeGreaterThan(40);
      const wanted = Math.max(196, before.height - (await shift()));
      await expect
        .poll(
          async () =>
            (await box(page.locator('[data-testid="pitch-xi"]:visible:not([inert] *)'))).height,
        )
        .toBeCloseTo(wanted, 0);
      if (wanted > 196) await expectPitchAndBenchFit(page, vp.height);
    });

    test('switching to the pitch after scrolling the list still fits pitch and bench', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/team`);
      await page.getByTestId('view-list').click();
      await expect(
        page.locator('[data-testid^="lineup-row-"]:visible:not([inert] *)').first(),
      ).toBeVisible();
      const scroller = page.locator('[data-testid="screen-scroll"]:visible:not([inert] *)');
      await expect
        .poll(() =>
          scroller.evaluate((el) => {
            el.scrollTop = 400;
            return el.scrollTop;
          }),
        )
        .toBeGreaterThan(100);
      // A plain click, so Playwright doesn't scroll the toggle back into view first.
      await page.getByTestId('view-pitch').evaluate((el) => (el as HTMLElement).click());
      await expect(page.locator('[data-testid="pitch-xi"]:visible:not([inert] *)')).toBeVisible();
      const pitch = await expectPitchAndBenchFit(page, vp.height);
      expect(pitch.y).toBeGreaterThanOrEqual(0);
    });

    test('every mock formation: 11 names clear of each other on the pitch', async ({ page }) => {
      test.setTimeout(120_000);
      const seen = new Set<string>();
      for (const url of TEAMS) {
        await page.goto(url);
        await expect(page.locator('[data-testid="pitch-xi"]:visible:not([inert] *)')).toBeVisible();
        await expect(
          page.locator(
            '[data-testid="pitch-xi"]:visible:not([inert] *) [data-testid^="lineup-player-"]',
          ),
        ).toHaveCount(11);
        seen.add(await formationOf(page));
        await expectNamesClear(page);
      }
      for (const f of WANTED) expect([...seen]).toContain(f);
    });
  });
}

test.describe('desktop', () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test('the half pitch and bench sit together, full size where there is room', async ({ page }) => {
    await page.goto(`${LEAGUE}/team`);
    const pitchLoc = page.locator('[data-testid="pitch-xi"]:visible:not([inert] *)');
    await expect(pitchLoc).toBeVisible();
    await expectNamesClear(page);
    const pitch = await box(pitchLoc);
    expect(pitch.width).toBeLessThanOrEqual(520);
    expect(pitch.height).toBeGreaterThanOrEqual(196);
    expect(pitch.height).toBeLessThanOrEqual(420);
    const bench = await box(page.locator('[data-testid="bench"]:visible:not([inert] *)'));
    expect(bench.y).toBeGreaterThan(pitch.y + pitch.height);
    expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
  });
});

test.describe('a 320x568 phone', () => {
  test.use({ viewport: { width: 320, height: 568 } });

  test('compact points: each badge inside its column and the pitch, none clipped', async ({
    page,
  }) => {
    await page.goto(`${LEAGUE}/team`);
    await expect(onScreen(page, 'gw-label')).toHaveText('GW 8');
    await onScreen(page, 'gw-prev').click();
    await expect(onScreen(page, 'gw-label')).toHaveText('GW 7');
    const pitchLoc = page.locator('[data-testid="pitch-xi"]:visible:not([inert] *)');
    await expect(pitchLoc.locator('[data-testid="pitch-points"]')).toHaveCount(11);
    const pitch = await box(pitchLoc);
    // Compact: the points sit on the shirt, beside the name chip's top.
    expect(pitch.height).toBeLessThan(420);
    for (const k of ['GKP', 'DEF', 'MID', 'FWD']) {
      const players = await pitchLoc
        .locator(`[data-testid="pitch-line-${k}"] [data-testid^="lineup-player-"]`)
        .all();
      const step = pitch.width / players.length;
      for (const [i, p] of players.entries()) {
        const badge = await box(p.getByTestId('pitch-points'));
        const col = { x: pitch.x + i * step, y: pitch.y, width: step, height: pitch.height };
        expect(inside(badge, col)).toBe(true);
        expect(inside(badge, pitch)).toBe(true);
      }
    }
  });
});

test.describe('a phone too short for the fitted pitch', () => {
  test.use({ viewport: { width: 320, height: 480 } });

  test('the pitch stops at its minimum height, compact names still clear', async ({ page }) => {
    await page.goto(`${LEAGUE}/team`);
    const pitchLoc = page.locator('[data-testid="pitch-xi"]:visible:not([inert] *)');
    await expect(pitchLoc.locator('[data-testid^="lineup-player-"]')).toHaveCount(11);
    await expect.poll(async () => (await box(pitchLoc)).height).toBeCloseTo(196, 0);
    // Compact boxes: no fixture line, names inside the pitch and apart.
    await expect(pitchLoc.getByTestId('pitch-detail')).toHaveCount(0);
    await expectNamesClear(page);
    expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
  });
});
