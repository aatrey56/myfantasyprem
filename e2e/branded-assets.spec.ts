import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn, startWithScenario } from './helpers';

// Branded images (club shirts, crests, player photos) come from /v1/assets;
// the mocks answer with small fixture PNGs. The fixtures guard fails any
// request outside localhost.

test.beforeEach(async ({ page }) => signIn(page));

const PITCH = '[data-testid="pitch-xi"]:visible';
const branded = (page: Page, scope: string, kind: string) =>
  page.locator(`${scope} [data-testid="branded-image"][data-kind="${kind}"]`);

/**
 * Every matched <img> finished loading with real pixels. They load lazily,
 * and WebKit (unlike Chromium) leave an image a screen below the
 * fold unloaded, so each is scrolled into view and waited for in turn:
 * scrolling them all first could carry an image back out of view before
 * the engine's lazy-load check saw it, and its request would never start.
 */
async function expectLoaded(page: Page, selector: string) {
  await expect(page.locator(selector).first()).toBeVisible();
  for (const img of await page.locator(selector).all()) {
    await img.scrollIntoViewIfNeeded();
    await expect
      .poll(() =>
        img.evaluate(
          (i) =>
            (i as HTMLImageElement).complete &&
            (i as HTMLImageElement).naturalWidth > 0 &&
            i.getAttribute('data-state') === 'loaded',
        ),
      )
      .toBe(true);
  }
}

/** The team screen's pitch view (the default; tapped in case the list is showing). */
async function openPitch(page: Page) {
  // The lineup has loaded (pitch or list) before the toggle is used.
  await expect(
    page
      .locator('[data-testid^="lineup-player-"]:visible, [data-testid^="lineup-row-"]:visible')
      .first(),
  ).toBeVisible();
  await page.getByTestId('view-pitch').click();
  await expect(page.locator(PITCH)).toBeVisible();
}

/** Counts the page's /v1/assets requests. */
function assetRequests(page: Page) {
  const urls: string[] = [];
  page.on('request', (r) => {
    if (new URL(r.url()).pathname.startsWith('/v1/assets/')) urls.push(r.url());
  });
  return urls;
}

test.describe('branded images on', () => {
  test('the pitch wears club shirts, the keeper his own', async ({ page }) => {
    await page.goto(`${LEAGUE}/team`);
    await openPitch(page);
    const shirts = `${PITCH} [data-testid="branded-image"][data-kind="shirt"]`;
    await expect(page.locator(shirts)).toHaveCount(11);
    await expectLoaded(page, shirts);
    const keeper = page.locator(
      `${PITCH} [data-testid="pitch-line-GKP"] [data-testid="branded-image"]`,
    );
    await expect(keeper).toHaveAttribute('src', /\/v1\/assets\/clubs\/[A-Z]{2,4}\/shirt\?gk=true$/);
    const outfield = page.locator(
      `${PITCH} [data-testid="pitch-line-FWD"] [data-testid="branded-image"]`,
    );
    await expect(outfield.first()).toHaveAttribute('src', /\/shirt$/);
    await expect(page.locator(`${PITCH} [data-testid="generic-shirt"]`)).toHaveCount(0);
    // The bench too.
    await expectLoaded(page, '[data-testid="bench-row"] [data-testid="branded-image"]');
  });

  test('player rows show club crests; the status page shows photos and crests', async ({
    page,
  }) => {
    await page.goto(`${LEAGUE}/players`);
    // Images load lazily: check the first row, which is on screen: the
    // player's photo, the club crest beside the club code and each
    // remaining opponent's crest in the fixture strip.
    const row = '[data-testid^="player-plr_"]:visible >> nth=0';
    await expect(
      page.locator(`${row} >> [data-testid="fixture-strip-item"]`).first(),
    ).toBeVisible();
    const fixtures = await page.locator(`${row} >> [data-testid="fixture-strip-item"]`).count();
    expect(fixtures).toBeGreaterThan(3);
    await expect(
      page.locator(`${row} >> [data-testid="branded-image"][data-kind="crest"]`),
    ).toHaveCount(1 + fixtures);
    await expect(
      page.locator(`${row} >> [data-testid="branded-image"][data-kind="photo"]`),
    ).toHaveCount(1);
    await expectLoaded(page, `${row} >> [data-testid="branded-image"]`);

    await page.goto(LEAGUE);
    const photos = branded(page, '[data-testid="potw"]:visible', 'photo');
    await expect(photos.first()).toBeVisible();
    await expect(photos.first()).toHaveAttribute(
      'src',
      /\/v1\/assets\/players\/plr_[0-9a-z]{26}\/photo$/,
    );
    await expectLoaded(page, '[data-testid="potw"]:visible [data-testid="branded-image"]');
    await expectLoaded(
      page,
      '[data-testid="totw"]:visible [data-testid="branded-image"][data-kind="shirt"]',
    );
  });

  test('roster rows, the swap sheet and the player card hero show photos', async ({ page }) => {
    await page.goto(`${LEAGUE}/team`);
    await page.getByTestId('view-list').click();
    // The list: 15 rows, each with the player's photo.
    const rows =
      '[data-testid="list-view"]:visible [data-testid="branded-image"][data-kind="photo"]';
    await expect(page.locator(rows)).toHaveCount(15);
    await expectLoaded(page, rows);
    await expect(
      page.locator('[data-testid="list-view"]:visible [data-testid="generic-shirt"]'),
    ).toHaveCount(0);
    // The swap sheet's rows too.
    await page.locator('[data-testid^="lineup-swap-"]').first().click();
    await expectLoaded(
      page,
      '[data-testid="swap-sheet"] [data-testid="branded-image"][data-kind="photo"]',
    );
    await page.getByTestId('swap-sheet').getByTestId('sheet-close').click();
    await expect(page.getByTestId('swap-sheet')).toHaveCount(0);
    // The player card hero: the photo, the club crest and both clubs' shirts for the next game.
    await page
      .locator('[data-testid^="lineup-row-"]')
      .first()
      .getByRole('button', { name: /^Player card for / })
      .click();
    const hero = '[data-testid="player-card"]:visible [data-testid="card-hero"]';
    await expect(branded(page, hero, 'photo')).toHaveCount(1);
    await expect(branded(page, hero, 'photo')).toHaveAttribute(
      'src',
      /\/v1\/assets\/players\/plr_[0-9a-z]{26}\/photo$/,
    );
    await expectLoaded(page, `${hero} [data-testid="branded-image"]`);
    await expect(branded(page, hero, 'crest')).toHaveCount(1);
  });

  test('matchup lineups show club crests', async ({ page }) => {
    await page.goto(`${LEAGUE}/matchup`);
    const crests =
      '[data-testid="h2h-starters"]:visible [data-testid="branded-image"][data-kind="crest"]';
    // One per starter, both sides.
    await expect(page.locator(crests)).toHaveCount(22);
    await expectLoaded(page, crests);
  });

  test('a missing image falls back to the generic shirt', async ({ page }) => {
    await startWithScenario(page, 'assets-missing');
    await page.goto(`${LEAGUE}/team`);
    await openPitch(page);
    await expect(page.locator(`${PITCH} [data-testid="generic-shirt"]`)).toHaveCount(11);
    await expect(page.locator(`${PITCH} [data-testid="branded-image"]`)).toHaveCount(0);
  });
});

test.describe('branded images off', () => {
  test('generic shirts everywhere and no image requests', async ({ page }) => {
    await startWithScenario(page, 'branded-off');
    const requests = assetRequests(page);
    await page.goto(`${LEAGUE}/team`);
    await page.getByTestId('view-list').click();
    await expect(
      page.locator('[data-testid="list-view"]:visible [data-testid="generic-shirt"]'),
    ).toHaveCount(15);
    await page
      .locator('[data-testid^="lineup-row-"]')
      .first()
      .getByRole('button', { name: /^Player card for / })
      .click();
    await expect(
      page.locator(
        '[data-testid="player-card"]:visible [data-testid="card-hero"] [data-testid="generic-shirt"]',
      ),
    ).toHaveCount(1);
    await page.locator('[data-testid="sheet-close"]:visible').click();
    await openPitch(page);
    await expect(page.locator(`${PITCH} [data-testid="generic-shirt"]`)).toHaveCount(11);
    await page.goto(`${LEAGUE}/matchup`);
    await expect(page.locator('[data-testid="h2h-starters"]:visible')).toBeVisible();
    await page.goto(`${LEAGUE}/players`);
    await expect(page.locator('[data-testid="generic-shirt"]:visible').first()).toBeVisible();
    await page.goto(LEAGUE);
    await expect(
      page.locator('[data-testid="potw"]:visible [data-testid="generic-shirt"]').first(),
    ).toBeVisible();
    await expect(page.locator('[data-testid="branded-image"]')).toHaveCount(0);
    expect(requests).toEqual([]);
  });
});
