import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn, useScenario } from './helpers';

/**
 * A trade offer the server refuses: the builder has already closed
 * (optimistic), the toast's "Edit offer" reopens it with the offer as it
 * was, less the player the refusal ruled out, and says why.
 */
test.beforeEach(async ({ page }) => signIn(page));

const visible = (page: Page, testId: string) => page.locator(`[data-testid="${testId}"]:visible`);
const visibleStarting = (page: Page, prefix: string) =>
  page.locator(`[data-testid^="${prefix}"]:visible`);

/** The position (GK, DEF, MID, FWD) of each row matching `prefix`, from its label, by id. */
const positions = (page: Page, prefix: string) =>
  visibleStarting(page, prefix).evaluateAll((rows, pre) => {
    const out: Record<string, string> = {};
    for (const row of rows) {
      const labels = [row, ...row.querySelectorAll('[aria-label]')].map(
        (e) => e.getAttribute('aria-label') ?? '',
      );
      const hit = labels.join(' | ').match(/Position (GK|DEF|MID|FWD)\b/);
      if (hit) out[row.getAttribute('data-testid')!.slice(pre.length)] = hit[1]!;
    }
    return out;
  }, prefix);

for (const vp of [
  { name: 'phone', width: 390, height: 844 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`refused trade offer (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('Edit offer reopens the builder pre-filled, without the player who left', async ({
      page,
    }) => {
      // Start from a rostered player's ⇄: the team and that player are chosen.
      await page.goto(`${LEAGUE}/players`);
      // Available off: owned players too.
      await visible(page, 'filter-available').click();
      await expect(visible(page, 'filter-available')).toHaveAttribute('aria-pressed', 'false');
      const tradeFor = visibleStarting(page, 'trade-for-').first();
      const wanted = (await tradeFor.getAttribute('data-testid'))!.replace('trade-for-', '');
      await tradeFor.click();
      await expect(visible(page, `get-${wanted}`)).toBeVisible();
      await expect(visibleStarting(page, 'give-plr_').first()).toBeVisible();
      const position = (await positions(page, 'get-'))[wanted]!;
      expect(position).toBeTruthy();
      const name = (await page
        .getByTestId(`get-toggle-${wanted}`)
        .getAttribute('aria-label'))!.replace(/^Remove (.+) from the trade$/, '$1');
      const offered = Object.entries(await positions(page, 'give-')).find(
        ([, pos]) => pos === position,
      )![0];
      await visible(page, `give-toggle-${offered}`).click();
      await visible(page, 'trade-next').click();
      await visible(page, 'trade-message').fill('Fancy it?');
      await useScenario(page, 'trade-refused');
      await visible(page, 'trade-send').click();

      // The builder closed at once; the refusal arrives as a toast with an action.
      await expect(page).toHaveURL(/\/players\/trades\?tab=outgoing/);
      const toast = page.getByTestId('toast');
      await expect(toast).toContainText('Not on their team.');
      await toast.getByTestId('toast-action').click();
      await expect(toast).toHaveCount(0);

      await expect(page).toHaveURL(/\/players\/trades\/new\?draft=1/);
      const notice = visible(page, 'trade-draft-notice');
      await expect(notice).toContainText('Your offer was refused');
      await expect(visible(page, `trade-draft-removed-${wanted}`)).toContainText(
        `${name} is no longer on`,
      );
      // My side kept, theirs without the player who left.
      await expect(visible(page, `give-toggle-${offered}`)).toHaveAccessibleName(/^Remove /);
      await expect(visible(page, `get-toggle-${wanted}`)).toHaveAccessibleName(/^Add /);

      // Pick another player in that position: the message is still there.
      const otherId = Object.entries(await positions(page, 'get-')).find(
        ([id, pos]) => pos === position && id !== wanted && !id.startsWith('toggle-'),
      )![0];
      await visible(page, `get-toggle-${otherId}`).click();
      await visible(page, 'trade-next').click();
      await expect(visible(page, 'trade-message')).toHaveValue('Fancy it?');

      // Sent again and taken.
      await useScenario(page, null);
      await visible(page, 'trade-send').click();
      await expect(page.getByTestId('toast')).toContainText('Offer sent');
    });
  });
}
