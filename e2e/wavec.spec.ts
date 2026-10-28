import { expect, test, type Page } from './fixtures';

import { LEAGUE, overflowingElements, signIn, startWithScenario, useScenario } from './helpers';

/** A mock team whose lineup has a flagged player with news (as team-tab.spec). */
const FLAGGED_TEAM = `${LEAGUE}/team/tm_01k7m2xq800000000000000004`;

test.beforeEach(async ({ page }) => signIn(page));

/** Tabs keep earlier screens mounted but hidden: only match what is on screen. */
const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);
const visibleStarting = (page: Page, prefix: string) =>
  page.locator(`[data-testid^="${prefix}"]:visible:not([inert] *)`);

/** Opens the card of the first player in my lineup list and returns his id. */
async function openMyFirstCard(page: Page): Promise<string> {
  const row = visibleStarting(page, 'lineup-row-').first();
  await expect(row).toBeVisible();
  const id = (await row.getAttribute('data-testid'))!.replace('lineup-row-', '');
  await row.getByRole('button', { name: /^Player card for / }).click();
  await expect(page.getByTestId('player-card')).toBeVisible();
  return id;
}

for (const vp of [
  { name: 'phone', width: 390, height: 844 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`trade block, news and tags (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('the card puts my player on the block; the table lists him under my team', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/team`);
      await page.getByTestId('view-list').click();
      const id = await openMyFirstCard(page);
      const card = page.getByTestId('player-card');
      await expect(card.getByTestId('card-block')).toContainText('Trade block');
      await card.getByTestId('card-block').click();
      await expect(card.getByTestId('card-block')).toContainText('On the block');
      await card.getByTestId('card-block-note').fill('Open to offers');
      await card.getByTestId('card-block-want-MID').click();
      await card.getByTestId('card-block-save').click();
      // Saved: the editor holds the saved entry, so Save has nothing left to send.
      await expect(card.getByTestId('card-block-save')).toBeDisabled();
      await page.getByTestId('sheet-close').click();
      // The lineup row now carries the BLOCK chip.
      await expect(visible(page, `lineup-row-${id}`).getByTestId('row-block')).toBeVisible();

      // Trades -> See all: my team's group holds him, with his note and wants.
      await visible(page, 'team-action-trades').click();
      await visible(page, 'trade-block-all').click();
      const table = visible(page, 'trade-block-table');
      await expect(table).toBeVisible();
      const row = table.getByTestId(`block-row-${id}`);
      await expect(row).toContainText('Wants MID');
      await expect(row).toContainText('Open to offers');
      await expect(table.getByTestId(`block-remove-${id}`)).toBeVisible();
      // Other teams' players have a trade button instead.
      await expect(table.locator('[data-testid^="block-trade-"]')).toHaveCount(3);
      expect(await overflowingElements(page)).toEqual([]);

      // Taking him off empties my group.
      await table.getByTestId(`block-remove-${id}`).click();
      await expect(table.getByTestId(`block-row-${id}`)).toHaveCount(0);
    });

    test("the carousel shows other teams' players; a card opens the trade builder with him asked for", async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/players/trades`);
      const carousel = visible(page, 'trade-block-carousel');
      await expect(carousel).toBeVisible();
      const cards = carousel.locator('[data-testid^="block-card-"]');
      await expect(cards).toHaveCount(3);
      await expect(cards.first()).toContainText(/Wants|Open to anything/);
      const id = (await cards.first().getAttribute('data-testid'))!.replace('block-card-', '');
      await cards.first().click();
      await expect(page).toHaveURL(
        new RegExp(`/players/trades/new\\?teamId=tm_[^&]+&playerId=${id}`),
      );
      await expect(visible(page, `get-toggle-${id}`)).toHaveAttribute(
        'aria-label',
        /^Remove .* from the trade$/,
      );
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('news: a time on the card and on the Team tab, newest first', async ({ page }) => {
      await page.goto(FLAGGED_TEAM);
      await page.getByTestId('view-list').click();
      const tag = page.locator('[data-testid="player-status"]:visible:not([inert] *)').first();
      await expect(tag).toHaveText(/^(\d{1,3}%|OUT|SUSP|DOUBT)$/);
      const row = page.locator('[data-testid^="lineup-row-"]:visible:not([inert] *)', { has: tag });
      await row.getByRole('button', { name: /^Player card for / }).click();
      const card = page.getByTestId('player-card');
      await expect(card.getByTestId('card-status')).toBeVisible();
      await expect(card.getByTestId('card-status-time')).toHaveText(
        / · (just now|\d+[mh] ago|\w{3} \d+ \w{3})$/,
      );
      await page.getByTestId('sheet-close').click();

      await visible(page, 'team-action-news').click();
      const sheet = page.getByTestId('news-sheet');
      await expect(sheet.locator('[data-testid^="news-row-"]')).not.toHaveCount(0);
      const times = await sheet.getByTestId('news-time').allTextContents();
      expect(times.length).toBeGreaterThan(0);
      // Newest first: "Nm ago" and "Nh ago" never grow down the list.
      const minutes = times.map((t) => {
        const m = /^(\d+)([mh]) ago$/.exec(t);
        return m ? Number(m[1]) * (m[2] === 'h' ? 60 : 1) : t === 'just now' ? 0 : Infinity;
      });
      expect([...minutes].sort((a, b) => a - b)).toEqual(minutes);
    });

    test('news: a failed request shows an error with Try again, never "available"', async ({
      page,
    }) => {
      test.slow(); // three retries with backoff (1 + 2 + 4 s) before the error shows
      await startWithScenario(page, 'news-503');
      await page.goto(FLAGGED_TEAM);
      await visible(page, 'team-action-news').click();
      const sheet = page.getByTestId('news-sheet');
      const error = sheet.getByTestId('query-error');
      await expect(error).toBeVisible({ timeout: 20_000 });
      await expect(sheet.getByTestId('news-empty')).toHaveCount(0);
      await expect(sheet).not.toContainText('All your players are available.');
      await expect(sheet).not.toContainText('Every player in this squad is available.');
      // The server is back: Try again lists the news.
      await useScenario(page, null);
      await error.getByTestId('retry').click();
      await expect(sheet.locator('[data-testid^="news-row-"]')).not.toHaveCount(0);
      await expect(sheet.getByTestId('query-error')).toHaveCount(0);
    });

    test('tags: tag a player on his card, see it on his row and filter by it', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      const first = visibleStarting(page, 'player-plr_').first();
      await expect(first).toBeVisible();
      const id = (await first.getAttribute('data-testid'))!.replace('player-', '');
      await first.getByRole('button', { name: /^Player card for / }).click();
      const card = page.getByTestId('player-card');
      await card.getByTestId('card-tag-target').click();
      await expect(card.getByTestId('card-tag-target')).toHaveAttribute('aria-checked', 'true');
      await page.getByTestId('sheet-close').click();
      await expect(visible(page, `player-${id}`).getByTestId('row-tag')).toHaveText('🎯');

      // The 🎯 filter shows only him; another tag shows nobody.
      await visible(page, 'filter-tag-target').click();
      await expect(visibleStarting(page, 'player-plr_')).toHaveCount(1);
      await expect(visible(page, `player-${id}`)).toBeVisible();
      await visible(page, 'filter-tag-hot').click();
      await expect(visible(page, 'players-empty')).toContainText('No players tagged Hot');
      expect(await overflowingElements(page)).toEqual([]);
    });
  });
}
