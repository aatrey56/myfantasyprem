import { expect, test, type Page } from './fixtures';

import { LEAGUE, overflowingElements, signIn } from './helpers';

test.beforeEach(async ({ page }) => signIn(page));

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);
const visibleStarting = (page: Page, prefix: string) =>
  page.locator(`[data-testid^="${prefix}"]:visible:not([inert] *)`);

const VIEWPORTS = [
  { name: 'phone', width: 375, height: 812, nav: 'league-tabs' },
  { name: 'desktop', width: 1280, height: 800, nav: 'top-nav' },
] as const;

for (const vp of VIEWPORTS) {
  test.describe(`league play (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('a live matchup opens a points breakdown', async ({ page }) => {
      await page.goto(`${LEAGUE}/matchup`);
      await expect(visible(page, 'score-header')).toBeVisible();
      await expect(visible(page, 'score-state')).toHaveText('Live');
      // Starters both sides, GK first, then the bench numbered 1-4.
      await expect(visible(page, 'h2h-starters')).toBeVisible();
      await expect(visible(page, 'h2h-bench')).toBeVisible();
      // Plain headers: no formations, bench count or unused points.
      await expect(visible(page, 'starters-header')).toHaveText('Starters');
      await expect(visible(page, 'bench-header')).toHaveText('Bench');
      // Games still to finish: the auto-sub is projected.
      await expect(visible(page, 'autosub-summary').first()).toHaveText(
        /^Auto-subs? projected: .+ → .+/,
      );
      const player = visibleStarting(page, 'h2h-player-').first();
      const points = (await player.getByTestId('h2h-points').textContent())!;
      // The points open the breakdown (the name opens the player card).
      await player.getByTestId(/^h2h-breakdown-/).click();
      const sheet = visible(page, 'points-breakdown');
      await expect(sheet).toBeVisible();
      // A dash in the row means no game has kicked off yet: 0 points.
      await expect(sheet.getByTestId('breakdown-total')).toHaveText(points === '–' ? '0' : points);
      // A centred dialog on every width, not a bottom sheet.
      const box = (await sheet.boundingBox())!;
      const vp = page.viewportSize()!;
      expect(Math.abs(box.x + box.width / 2 - vp.width / 2)).toBeLessThan(2);
      expect(Math.abs(box.y + box.height / 2 - vp.height / 2)).toBeLessThan(40);
      await visible(page, 'sheet-close').click();
      await expect(visible(page, 'points-breakdown')).toHaveCount(0);
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('the league-average opponent is one card with its score', async ({ page }) => {
      await page.goto(`${LEAGUE}/matchup`);
      await visibleStarting(page, 'matchup-row-').filter({ hasText: 'League average' }).click();
      await expect(visible(page, 'average-opponent')).toContainText('odd number of teams');
      await expect(visible(page, 'score-right-name')).toHaveText('League average');
      await expect(visible(page, 'h2h-lineups')).toHaveCount(0);
      expect(await overflowingElements(page)).toEqual([]);
    });

    test("standings open another team's roster", async ({ page }) => {
      await page.goto(`${LEAGUE}/league`);
      const row = visibleStarting(page, 'standing-').filter({ hasNotText: 'Sam P.' }).first();
      // The row's label starts "1. Team name, Manager. ...".
      const name = /^\d+\. (.+?), /.exec((await row.getAttribute('aria-label'))!)![1]!;
      await row.click();
      await expect(page).toHaveURL(/\/team\/tm_[0-9a-z]{26}$/);
      await expect(page.getByRole('heading', { name })).toBeVisible();
      await expect(visible(page, 'propose-trade')).toBeVisible();
      await expect(visible(page, 'pitch-view')).toBeVisible();
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('adds a free agent, dropping a player of the same position', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      // Available is on by default: free agents and players on waivers.
      const add = page
        .locator('[data-testid^="move-"]:visible:not([inert] *)[aria-label^="Add "]')
        .first();
      await expect(add).toHaveAccessibleName(/^Add /);
      await add.click();
      await expect(visible(page, 'drop-step')).toBeVisible();
      await expect(visible(page, 'drop-confirm')).toBeDisabled();
      await visibleStarting(page, 'drop-plr_').first().click();
      await visible(page, 'drop-confirm').click();
      await expect(page.getByTestId('toast')).toContainText(/^Added .+, dropped .+/);
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('claims a player on waivers with a drop', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      await visible(page, 'view-menu').click();
      await visible(page, 'view-pos-MID').click();
      await expect(visible(page, 'view-sheet')).toHaveCount(0);
      const claim = page
        .locator('[data-testid^="move-"]:visible:not([inert] *)[aria-label^="Claim "]')
        .first();
      await expect(claim).toHaveAccessibleName(/^Claim /);
      await claim.click();
      await visibleStarting(page, 'drop-plr_').last().click();
      await visible(page, 'drop-confirm').click();
      await expect(page.getByTestId('toast')).toContainText('Claim placed');
      await expect(page).toHaveURL(/\/players\/waivers$/);
      // Two seeded claims plus this one, in my order.
      await expect(visibleStarting(page, 'claim-wv_')).toHaveCount(3);
      await expect(visible(page, 'waiver-next-run')).toContainText('Next waiver run');
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('a proposed trade is accepted and shows in the activity feed', async ({ page }) => {
      await page.goto(`${LEAGUE}/players/trades`);
      await visible(page, 'new-trade').click();
      await visibleStarting(page, 'trade-team-').first().click();
      // The + beside a row adds the player (the row itself opens the card).
      await visibleStarting(page, 'give-plr_')
        .filter({ hasText: 'MID' })
        .first()
        .getByTestId(/^give-toggle-/)
        .click();
      const wanted = visibleStarting(page, 'get-plr_').filter({ hasText: 'MID' }).first();
      await wanted.getByTestId(/^get-toggle-/).click();
      await expect(visible(page, 'trade-check')).toContainText('a legal trade');
      await visible(page, 'trade-next').click();
      await visible(page, 'trade-message').fill('Fancy a swap?');
      await visible(page, 'trade-send').click();
      await expect(page.getByTestId('toast')).toContainText('Offer sent');
      await expect(page).toHaveURL(/\/players\/trades\?tab=outgoing$/);
      await expect(visibleStarting(page, 'trade-trd_').first()).toContainText('Fancy a swap?');
      // The other manager (simulated in the mocks) accepts a few seconds later;
      // The Sunday Draft has no trade review, so it processes at once.
      await page.waitForTimeout(4_500);
      // Activity opens from the League tab.
      await page.getByTestId(vp.nav).getByText('League', { exact: true }).click();
      await visible(page, 'link-activity').click();
      await visible(page, 'filter-trade').click();
      const newest = visibleStarting(page, 'txn-txn_').first();
      await expect(newest).toContainText('Sunday League Heroes gets');
      await expect(newest.getByTestId('txn-trade-status')).toHaveText('Processed');
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('activity filters by kind', async ({ page }) => {
      await page.goto(`${LEAGUE}/activity`);
      await expect(visible(page, 'activity-feed')).toBeVisible();
      await visible(page, 'filter-trade').click();
      const trades = visibleStarting(page, 'txn-txn_');
      await expect(trades.first()).toBeVisible();
      for (const status of await visible(page, 'txn-trade-status').allTextContents()) {
        expect(['Processed', 'Vetoed']).toContain(status);
      }
      expect(await trades.count()).toBe(await visible(page, 'txn-trade-status').count());
      await visible(page, 'filter-waiver').click();
      await expect(visible(page, 'txn-trade-status')).toHaveCount(0);
      await expect(visibleStarting(page, 'txn-txn_').first()).toContainText(/Waiver|Failed/);
      expect(await overflowingElements(page)).toEqual([]);
    });
  });
}
