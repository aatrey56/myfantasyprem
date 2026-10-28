import { expect, test } from './fixtures';

import { overflowingElements, signIn } from './helpers';

/** The Office League: a trade in a league vote, and the demo user is commissioner. */
const OFFICE = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd0';

test.beforeEach(async ({ page }) => signIn(page));

for (const vp of [
  { name: 'phone', width: 375, height: 812 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`trade review (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('votes read Veto / Uphold, commissioner powers sit apart on the right', async ({
      page,
    }) => {
      await page.goto(`${OFFICE}/players/trades`);
      // One Active list: the trade in review sits among the open offers.
      const card = page
        .locator('[data-testid^="trade-trd_"]:visible')
        .filter({ has: page.getByTestId('trade-review') })
        .first();
      await expect(card.getByTestId('trade-review')).toContainText('1 uphold');
      await expect(card.getByTestId('trade-vote-veto')).toHaveText('Veto');
      await expect(card.getByTestId('trade-vote-uphold')).toHaveText('Uphold');
      await expect(card.getByText(/Vote veto|Vote uphold|allow/i)).toHaveCount(0);

      const panel = card.getByTestId('commissioner-actions');
      await expect(panel).toContainText('Commissioner');
      await expect(panel.getByTestId('trade-veto')).toHaveText('Veto');
      await expect(panel.getByTestId('trade-uphold')).toHaveText('Uphold');
      // The panel hugs the card's right edge, right of the member votes.
      const cardBox = (await card.boundingBox())!;
      const panelBox = (await panel.boundingBox())!;
      const votes = (await card.getByTestId('trade-vote-uphold').boundingBox())!;
      if (vp.width < 768) {
        // Phone: the panel spans the card's content width, below the votes.
        const content = (await card.getByTestId('trade-actions').boundingBox())!;
        expect(
          Math.abs(panelBox.width - (cardBox.width - 2 * (content.x - cardBox.x))),
        ).toBeLessThan(4);
        expect(Math.abs(panelBox.x - content.x)).toBeLessThan(2);
        expect(panelBox.y).toBeGreaterThan(votes.y + votes.height);
        const [a, b] = await Promise.all(
          ['trade-veto', 'trade-uphold'].map((t) => panel.getByTestId(t).boundingBox()),
        );
        expect(Math.abs(a!.width - b!.width)).toBeLessThan(2);
      } else {
        // Desktop: the panel hugs the card's right edge, right of the member votes.
        expect(cardBox.x + cardBox.width - (panelBox.x + panelBox.width)).toBeLessThan(40);
        expect(panelBox.x).toBeGreaterThan(votes.x + votes.width);
      }
      expect(await overflowingElements(page)).toEqual([]);

      await card.getByTestId('trade-vote-uphold').click();
      const dialog = page.getByTestId('trade-confirm');
      await expect(dialog).toContainText('Vote to uphold?');
      await dialog.getByTestId('confirm-ok').click();
      await expect(card.getByTestId('trade-review')).toContainText('2 uphold · you voted uphold');
      await expect(card.getByTestId('trade-vote-uphold')).toBeDisabled();
    });
  });
}
