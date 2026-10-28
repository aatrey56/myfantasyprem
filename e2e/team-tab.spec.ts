import { expect, test, type Locator } from './fixtures';

import { LEAGUE, overflowingElements, pageOverflow, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

/** A mock team whose locked gameweek-7 lineup has a doubtful starter with news. */
const FLAGGED_TEAM = `${LEAGUE}/team/tm_01k7m2xq800000000000000004`;

const box = async (l: Locator) => (await l.boundingBox())!;
const centreY = (b: { y: number; height: number }) => b.y + b.height / 2;

for (const vp of [
  { name: 'phone', width: 390, height: 844 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`team tab (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('the team card sits under the tabs: ranks, current points, four quick links', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/team`);
      const card = page.locator('[data-testid="team-card"]:visible');
      await expect(card).toBeVisible();
      // Directly under the league tabs (the screen padding only): no title, eyebrow or back link.
      const tabs = await box(page.getByRole('tablist').first());
      expect((await box(card)).y - (tabs.y + tabs.height)).toBeLessThanOrEqual(28);
      await expect(card.getByTestId('back-link')).toHaveCount(0);
      await expect(card.getByTestId('team-record')).toHaveText(
        /^\d+-\d+-\d+ · \d+(st|nd|rd|th) · PF \d+(st|nd|rd|th) · PA \d+(st|nd|rd|th)$/,
      );
      // Gameweek 7 is live in the mocks: its points. Expected and the win
      // chance are hidden by default (e2e/projections.spec.ts).
      await expect(card.getByTestId('team-points')).toHaveText(/^\d+$/);
      await expect(card.getByTestId('team-live')).toContainText('Live');
      // Schedule, Trade (with the incoming count), Moves, News; deadlines live on Status.
      for (const id of ['schedule', 'trades', 'moves', 'news']) {
        await expect(card.getByTestId(`team-action-${id}`)).toBeVisible();
      }
      await expect(card.getByTestId('team-action-trades-badge')).toHaveText('1');
      await expect(card.getByTestId('team-action-deadlines')).toHaveCount(0);
      await expect(card).not.toContainText('Deadlines');
      // The four links share one line, icon beside label.
      const ys = await Promise.all(
        ['schedule', 'trades', 'moves', 'news'].map(async (id) =>
          centreY(await box(card.getByTestId(`team-action-${id}`))),
        ),
      );
      expect(Math.max(...ys) - Math.min(...ys)).toBeLessThanOrEqual(1);
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
    });

    test('one header row: Starters, the view toggle and the stepper; the lock time in the hint', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/team`);
      const heading = page.getByTestId('lineup-heading');
      await expect(heading).toHaveText('Starters');
      const row = [
        await box(heading),
        await box(page.getByTestId('view-list')),
        await box(page.getByTestId('view-pitch')),
        await box(page.getByTestId('gw-switcher')),
      ];
      const centres = row.map(centreY);
      expect(Math.max(...centres) - Math.min(...centres)).toBeLessThanOrEqual(4);
      // Left to right: heading, list, pitch, stepper.
      expect(row[0]!.x).toBeLessThan(row[1]!.x);
      expect(row[1]!.x).toBeLessThan(row[2]!.x);
      expect(row[2]!.x).toBeLessThan(row[3]!.x);
      // The pitch is the default, as a small icon toggle.
      await expect(page.getByTestId('view-pitch')).toHaveAttribute('aria-checked', 'true');
      await expect(page.getByTestId('view-list')).toHaveAttribute('aria-checked', 'false');
      await expect(page.getByTestId('pitch-view')).toBeVisible();
      expect(row[1]!.height).toBeLessThanOrEqual(36);
      await page.getByTestId('view-list').click();
      await expect(page.getByTestId('view-list')).toHaveAttribute('aria-checked', 'true');
      const header = page.getByTestId('lineup-header');
      await expect(header).toContainText('Tap a position to change your lineup');
      await expect(header.getByTestId('lineup-open')).toHaveText(/^Locks \w{3} /);
      // The roster starts high: the card, one header row and a hint above it.
      const first = await box(page.locator('[data-testid^="lineup-row-"]').first());
      expect(first.y).toBeLessThan(vp.name === 'phone' ? 430 : 440);
      await page.getByTestId('view-pitch').click();
      await expect(page.getByTestId('pitch-view')).toBeVisible();
      await expect(page.getByTestId('view-pitch')).toHaveAttribute('aria-checked', 'true');
      await page.getByTestId('view-list').click();
      await expect(page.getByTestId('list-view')).toBeVisible();
    });

    test('roster rows: no box or dividers, the season line, about 90pt tall', async ({ page }) => {
      await page.goto(`${LEAGUE}/team`);
      await page.getByTestId('view-list').click();
      const starters = page.getByTestId('list-starters');
      await expect(starters.locator('[data-testid^="lineup-row-"]')).toHaveCount(11);
      const style = await starters.evaluate((el) => getComputedStyle(el));
      expect(style.borderTopWidth).toBe('0px');
      expect(style.backgroundColor).toBe('rgba(0, 0, 0, 0)');
      const rows = page.locator('[data-testid^="lineup-row-"]');
      const n = await rows.count();
      for (let i = 0; i < n; i++) {
        const r = rows.nth(i);
        expect(await r.evaluate((el) => getComputedStyle(el).borderTopWidth)).toBe('0px');
        await expect(r.getByTestId('player-season')).toHaveText(
          /^Form (\d+\.\d|–) · \d+ pts · Played \d+\/\d+$/,
        );
        const fixtures = await r.locator('[data-testid="next-fixtures"] > *').count();
        // One fixture (or none): about 90pt. A double gameweek adds a line.
        const h = (await box(r)).height;
        if (fixtures <= 1) {
          expect(h).toBeGreaterThanOrEqual(76);
          expect(h).toBeLessThanOrEqual(96);
        }
      }
      // Gameweek 8 has a double and a blank in the mocks; both still read right.
      await expect(
        page.locator('[data-testid="next-fixtures"]', { hasText: 'No fixture' }),
      ).not.toHaveCount(0);
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('a flagged player: status tag and news icon on the fixture line; News lists him', async ({
      page,
    }) => {
      await page.goto(FLAGGED_TEAM);
      await page.getByTestId('view-list').click();
      await expect(page.getByTestId('list-view')).toBeVisible();
      const tag = page.locator('[data-testid="player-status"]').first();
      await expect(tag).toHaveText(/^(\d{1,3}%|OUT|SUSP|DOUBT)$/);
      const row = page.locator('[data-testid^="lineup-row-"]', { has: tag });
      await expect(row.getByTestId('player-news-icon')).toBeAttached();
      const name = (await row
        .getByRole('button', { name: /^Player card for / })
        .getAttribute('aria-label'))!.replace(/^Player card for /, '');
      // The news icon is part of the row, which opens the card with the news.
      await row.getByRole('button', { name: /^Player card for / }).click();
      await expect(page.getByTestId('player-card')).toContainText(name);
      await page.getByTestId('sheet-close').click();
      await page.getByTestId('team-action-news').click();
      const sheet = page.getByTestId('news-sheet');
      await expect(sheet).toBeVisible();
      await expect(sheet.locator('[data-testid^="news-row-"]')).not.toHaveCount(0);
      await expect(sheet).toContainText(name);
      await expect(sheet.getByTestId('news-tag').first()).toHaveText(await tag.innerText());
      await sheet
        .getByTestId(/^news-row-/)
        .first()
        .click();
      await expect(page.getByTestId('news-sheet')).toHaveCount(0);
      await expect(page.getByTestId('player-card')).toBeVisible();
    });
  });
}

test('my squad has no news: the News sheet says so', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${LEAGUE}/team`);
  await page.getByTestId('team-action-news').click();
  await expect(page.getByTestId('news-empty')).toContainText('All your players are available.');
  await page.getByTestId('sheet-close').click();
  await expect(page.getByTestId('news-sheet')).toHaveCount(0);
});
