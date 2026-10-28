import { expect, test, type Locator, type Page } from './fixtures';

import { DRAFT_LEAGUE, LEAGUE, signIn } from './helpers';

// Any player listed anywhere opens their card on a tap of the name (or row
// body), at phone width on a touch screen. A row's action buttons are
// separate hit targets: they do their job and never also open the card.
test.beforeEach(async ({ page }) => signIn(page));
test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) => page.locator(`[data-testid="${testId}"]:visible`);
const visibleStarting = (page: Page, prefix: string) =>
  page.locator(`[data-testid^="${prefix}"]:visible`);

/** The row's "Player card for X" button, and X. */
async function cardButton(row: Locator) {
  const button = row.getByRole('button', { name: /^Player card for / }).first();
  const label = (await button.getAttribute('aria-label'))!;
  return { button, name: label.replace(/^Player card for /, '') };
}

/** Taps the name: the card opens on that player; then closes it. */
async function expectNameOpensCard(page: Page, row: Locator) {
  const { button, name } = await cardButton(row);
  await button.tap();
  const card = visible(page, 'player-card');
  await expect(card).toBeVisible();
  await expect(card).toContainText(name);
  await visible(page, 'sheet-close').tap();
  await expect(visible(page, 'player-card')).toHaveCount(0);
  return name;
}

const noCard = (page: Page) => expect(visible(page, 'player-card')).toHaveCount(0);

test('draft room Players tab: the name opens the card; Queue + and Draft do not', async ({
  page,
}) => {
  await page.goto(`${DRAFT_LEAGUE}/draft`);
  await page.getByTestId('pane-players').tap();
  const row = visibleStarting(page, 'draft-player-plr_').first();
  await expect(row).toBeVisible();
  const id = (await row.getAttribute('data-testid'))!.replace('draft-player-', '');
  const name = await expectNameOpensCard(page, row);

  await page.getByTestId(`queue-toggle-${id}`).tap();
  await expect(page.getByTestId(`queue-toggle-${id}`)).toHaveAttribute(
    'aria-label',
    `Remove ${name} from queue`,
  );
  await noCard(page);

  await page.getByTestId(`draft-pick-${id}`).tap();
  await expect(page.getByTestId('toast')).toContainText(`You drafted ${name}`);
  await noCard(page);
});

test('draft room Queue tab: the name opens the card; reorder and remove do not', async ({
  page,
}) => {
  await page.goto(`${DRAFT_LEAGUE}/draft`);
  await page.getByTestId('pane-players').tap();
  const toggles = visibleStarting(page, 'queue-toggle-plr_');
  await expect(toggles.first()).toBeVisible();
  const first = (await toggles.nth(0).getAttribute('data-testid'))!.replace('queue-toggle-', '');
  const second = (await toggles.nth(1).getAttribute('data-testid'))!.replace('queue-toggle-', '');
  await toggles.nth(0).tap();
  await toggles.nth(1).tap();
  await page.getByTestId('pane-queue').tap();
  const items = visibleStarting(page, 'queue-item-');
  await expect(items).toHaveCount(2);

  await expectNameOpensCard(page, page.getByTestId(`queue-item-${first}`));

  await page.getByTestId(`queue-down-${first}`).tap();
  await expect(items.nth(0)).toHaveAttribute('data-testid', `queue-item-${second}`);
  await noCard(page);
  await page.getByTestId(`queue-remove-${first}`).tap();
  await expect(items).toHaveCount(1);
  await noCard(page);
});

test('draft room board and ticker: a pick opens the card', async ({ page }) => {
  await page.goto(`${DRAFT_LEAGUE}/draft`);
  await page.getByTestId('pane-board').tap();
  const pick = visibleStarting(page, 'board-pick-plr_').first();
  await expect(pick).toBeVisible();
  const name = (await pick.getAttribute('aria-label'))!.replace(/^Player card for /, '');
  await pick.tap();
  await expect(visible(page, 'player-card')).toContainText(name);
  await visible(page, 'sheet-close').tap();
  await noCard(page);

  const latest = page.getByTestId('ticker-latest');
  const tickerName = (await latest.getAttribute('aria-label'))!.replace(/^Player card for /, '');
  await latest.tap();
  await expect(visible(page, 'player-card')).toContainText(tickerName);
});

test('waivers: a claim names open their cards; moving a claim does not', async ({ page }) => {
  await page.goto(`${LEAGUE}/players/waivers`);
  const claims = visibleStarting(page, 'claim-wv_');
  await expect(claims).toHaveCount(2);
  const firstId = (await claims.nth(0).getAttribute('data-testid'))!.replace('claim-', '');
  await expectNameOpensCard(page, claims.nth(0));

  await page.getByTestId(`claim-down-${firstId}`).tap();
  await expect(claims.nth(1)).toHaveAttribute('data-testid', `claim-${firstId}`);
  await noCard(page);
});

test('trade builder: the name opens the card; the + adds to the trade and does not', async ({
  page,
}) => {
  await page.goto(`${LEAGUE}/players/trades`);
  await visible(page, 'new-trade').tap();
  await visibleStarting(page, 'trade-team-').first().tap();
  const row = visibleStarting(page, 'give-plr_').first();
  await expect(row).toBeVisible();
  const name = await expectNameOpensCard(page, row);

  const toggle = row.getByTestId(/^give-toggle-/);
  await expect(toggle).toHaveAttribute('aria-label', `Add ${name} to the trade`);
  await toggle.tap();
  await expect(toggle).toHaveAttribute('aria-label', `Remove ${name} from the trade`);
  await noCard(page);
});

test('matchup: the name opens the card; the points open the breakdown', async ({ page }) => {
  await page.goto(`${LEAGUE}/matchup`);
  const cell = visibleStarting(page, 'h2h-player-').first();
  await expect(cell).toBeVisible();
  await expectNameOpensCard(page, cell);
  await cell.getByTestId(/^h2h-breakdown-/).tap();
  await expect(visible(page, 'points-breakdown')).toBeVisible();
  await noCard(page);
});

test('activity: a player in a transaction opens their card', async ({ page }) => {
  await page.goto(`${LEAGUE}/activity`);
  const txn = visibleStarting(page, 'txn-txn_').first();
  await expect(txn).toBeVisible();
  await expectNameOpensCard(page, txn);
});

test('team page list view: a row opens the card; its position pill opens the swap sheet', async ({
  page,
}) => {
  await page.goto(`${LEAGUE}/team`);
  // The pitch is the default view; the list is one tap away.
  await page.getByTestId('view-list').click();
  const row = visibleStarting(page, 'lineup-row-').first();
  await expect(row).toBeVisible();
  // My lineup is editable: the row body opens the card, the pill starts a swap.
  const name = await expectNameOpensCard(page, row);
  await row.getByRole('button', { name: `Swap ${name}`, exact: true }).tap();
  await expect(visible(page, 'swap-sheet')).toBeVisible();
  await expect(visible(page, 'swap-selected')).toContainText(name);
  await noCard(page);
  await visible(page, 'sheet-close').tap();
  await expect(visible(page, 'swap-sheet')).toHaveCount(0);
});
