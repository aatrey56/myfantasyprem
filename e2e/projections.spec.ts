import { expect, test, type Page } from './fixtures';

import { LEAGUE, pageOverflow, signIn } from './helpers';

// Fixture difficulty, Expected points, win chance and start/sit accuracy
// (docs/adr/0014-projections.md) on the mocks: gameweek 7 is live, 1-6 are
// completed, 8 is next (with a double and a blank).
//
// Expected and the win chance are hidden unless the build sets
// EXPO_PUBLIC_SHOW_PROJECTIONS=true (owner, 2026-10-17). The mock build
// leaves it off, so the tests of the shown UI skip; Jest covers the switch
// both ways (components/projections/TeamProjectionBar.test.tsx). To run them,
// build the mocks and run Playwright with the variable set to "true".
const SHOWN = process.env.EXPO_PUBLIC_SHOW_PROJECTIONS === 'true';
const HIDDEN = 'Expected and the win chance are hidden (EXPO_PUBLIC_SHOW_PROJECTIONS is off)';

test.beforeEach(async ({ page }) => signIn(page));

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);

const CHIP = /^\((\d|1\d|20)(st|nd|rd|th)\)$/;
const PERCENT = /^(\d{1,3}%|<1%|>99%)$/;
const pct = (s: string) => (s === '<1%' ? 0 : s === '>99%' ? 100 : Number(s.replace('%', '')));

for (const vp of [
  { name: 'phone', width: 390, height: 844 },
  { name: 'desktop', width: 1280, height: 800 },
]) {
  test.describe(`projections (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('hidden: no Expected or win chance on team, league or matchup, and no request', async ({
      page,
    }) => {
      test.skip(SHOWN, 'the projections switch is on');
      const requests: string[] = [];
      page.on('request', (r) => {
        if (r.url().includes('/projections')) requests.push(r.url());
      });
      await page.goto(`${LEAGUE}/team`);
      const card = visible(page, 'team-card');
      await expect(card.getByTestId('team-points')).toBeVisible();
      await page.getByTestId('view-list').click();
      // Fixture difficulty chips are not projections: they stay.
      await expect(
        visible(page, 'list-starters').getByTestId('fixture-difficulty').first(),
      ).toHaveText(CHIP);
      await expect(page.locator('[data-testid="team-projection"]')).toHaveCount(0);
      await expect(page.locator('[data-testid="team-win-chance"]')).toHaveCount(0);
      await expect(page.locator('[data-testid="player-expected"]')).toHaveCount(0);
      await expect(page.locator('[data-testid="lineup-expected"]')).toHaveCount(0);

      await page.goto(`${LEAGUE}/league`);
      await expect(
        page.locator('[data-testid^="matchup-row-mu_"]:visible:not([inert] *)').first(),
      ).toBeVisible();
      await expect(page.locator('[data-testid^="win-chance-mu_"]')).toHaveCount(0);

      await page.goto(`${LEAGUE}/matchup`);
      await expect(visible(page, 'gw-next').first()).toBeVisible();
      await expect(page.locator('[data-testid="matchup-projection"]')).toHaveCount(0);
      await expect(page.locator('[data-testid="win-chance-basis"]')).toHaveCount(0);
      await visible(page, 'gw-next').first().click();
      await expect(visible(page, 'matchup-preview')).toBeVisible();
      await expect(page.locator('[data-testid="win-chance-basis"]')).toHaveCount(0);
      expect(requests).toEqual([]);
    });

    test('hidden: the live matchup header and the gameweek cards show no percentages', async ({
      page,
    }) => {
      test.skip(SHOWN, 'the projections switch is on');
      await page.goto(`${LEAGUE}/matchup`);
      await expect(visible(page, 'score-header')).toBeVisible();
      // The only percentages are the season start/sit accuracy: no win chance.
      const header = visible(page, 'score-header');
      await expect(header.getByTestId('season-start-sit')).toBeVisible();
      const all = await header.getByText(/%/).count();
      // Start/sit accuracy and the points-share bar (a share of points scored, not a win chance).
      expect(all).toBe(
        (await header.getByTestId('season-start-sit').getByText(/%/).count()) +
          (await header.getByTestId('points-split').getByText(/%/).count()),
      );
      await page.goto(`${LEAGUE}/league`);
      const rows = page.locator('[data-testid^="matchup-row-mu_"]:visible:not([inert] *)');
      await expect(rows.first()).toBeVisible();
      await expect(rows.getByText(/%/)).toHaveCount(0);
    });

    test('team: Expected and the win chance on the card, Expected and chips on the rows', async ({
      page,
    }) => {
      test.skip(!SHOWN, HIDDEN);
      await page.goto(`${LEAGUE}/team`);
      const card = visible(page, 'team-card');
      await expect(card.getByTestId('team-expected')).toHaveText(
        /^Exp \d+\.\d · Opp \d+\.\d · GW 7$/,
      );
      const left = await card.getByTestId('team-win-chance-left').textContent();
      const right = await card.getByTestId('team-win-chance-right').textContent();
      expect(left).toMatch(PERCENT);
      expect(right).toMatch(PERCENT);
      // Two chances (and a draw) out of 100, each rounded.
      expect(pct(left!) + pct(right!)).toBeLessThanOrEqual(101);
      await expect(card.getByTestId('team-win-chance-basis')).toContainText(
        'Based on recent scores',
      );

      // Gameweek 8: each starter's Expected, and the XI's is their sum.
      await page.getByTestId('view-list').click();
      const starters = visible(page, 'list-starters');
      const rows = starters.getByTestId('player-expected');
      await expect(rows).toHaveCount(11);
      let tenths = 0;
      for (const t of await rows.allTextContents()) {
        expect(t).toMatch(/^Exp -?\d+\.\d$/);
        tenths += Math.round(Number(t.replace('Exp ', '')) * 10);
      }
      await expect(visible(page, 'lineup-expected')).toHaveText(`Exp ${(tenths / 10).toFixed(1)}`);

      // A blank gameweek is 0 Expected; fixtures still to play carry a rank chip.
      const lineupRows = page.locator('[data-testid^="lineup-row-"]:visible:not([inert] *)');
      const n = await lineupRows.count();
      let blanks = 0;
      let chips = 0;
      for (let i = 0; i < n; i++) {
        const row = lineupRows.nth(i);
        const fixtures = row.getByTestId('next-fixtures');
        if ((await fixtures.textContent()) === 'No fixture') {
          blanks++;
          await expect(row.getByTestId('player-expected')).toHaveText('Exp 0.0');
          continue;
        }
        for (const chip of await row.getByTestId('fixture-difficulty').allTextContents()) {
          expect(chip).toMatch(CHIP);
          chips++;
        }
      }
      expect(blanks).toBeGreaterThan(0);
      expect(chips).toBeGreaterThanOrEqual(n - blanks);
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
    });

    test('team: start/sit accuracy per completed gameweek', async ({ page }) => {
      await page.goto(`${LEAGUE}/team`);
      const card = visible(page, 'start-sit');
      await card.scrollIntoViewIfNeeded();
      await expect(card.getByTestId('start-sit-season')).toHaveText(/^\d{1,3}%$/);
      // Gameweeks 1-6 are completed; 7 is live and not counted.
      const weeks = card.locator('[data-testid^="start-sit-gw-"]');
      await expect(weeks).toHaveCount(6);
      await expect(card.getByTestId('start-sit-gw-7')).toHaveCount(0);
      let sum = 0;
      for (const label of await weeks.evaluateAll((els) =>
        els.map((e) => e.getAttribute('aria-label') ?? ''),
      )) {
        // "Gameweek 6: 52 of 61 possible, 85%": points never beat the
        // optimal XI, and the percent is points / optimal.
        const m = /^Gameweek \d+: (-?\d+) of (\d+) possible, (\d+)%$/.exec(label);
        expect(m, label).not.toBeNull();
        const [pts, opt, shown] = [Number(m![1]), Number(m![2]), Number(m![3])];
        expect(pts).toBeLessThanOrEqual(opt);
        expect(shown).toBe(Math.round((pts / opt) * 100));
        sum += pts / opt;
      }
      // The season figure is the mean of the gameweeks'.
      await expect(card.getByTestId('start-sit-season')).toHaveText(
        `${Math.round((sum / 6) * 100)}%`,
      );
    });

    test('league: win chance on every matchup card (when shown), start/sit table best first', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/league`);
      const cards = page.locator('[data-testid^="matchup-row-mu_"]:visible:not([inert] *)');
      await expect(cards.first()).toBeVisible();
      const count = await cards.count();
      // One win chance per card, each with its label.
      const basis = page.locator(
        '[data-testid^="win-chance-mu_"][data-testid$="-basis"]:visible:not([inert] *)',
      );
      await expect(basis).toHaveCount(SHOWN ? count : 0);
      for (let i = 0; i < (SHOWN ? count : 0); i++) {
        await expect(basis.nth(i)).toContainText('Based on recent scores');
      }

      const table = visible(page, 'league-start-sit');
      await table.scrollIntoViewIfNeeded();
      const rows = table.locator('[data-testid^="start-sit-row-"]');
      expect(await rows.count()).toBeGreaterThan(1);
      const seasons: number[] = [];
      for (const label of await rows.evaluateAll((els) =>
        els.map((e) => e.getAttribute('aria-label') ?? ''),
      )) {
        const m = /season (\d+)%/.exec(label);
        expect(m).not.toBeNull();
        seasons.push(Number(m![1]));
      }
      expect(seasons).toEqual([...seasons].sort((a, b) => b - a));
      expect(await pageOverflow(page)).toBeLessThanOrEqual(0);
    });

    test('matchup: Expected for both sides and the win chance under the header', async ({
      page,
    }) => {
      test.skip(!SHOWN, HIDDEN);
      await page.goto(`${LEAGUE}/matchup`);
      const strip = visible(page, 'matchup-projection');
      await expect(strip).toBeVisible();
      await expect(strip.getByTestId('expected-left')).toHaveText(/^\d+\.\d$/);
      await expect(strip.getByTestId('expected-right')).toHaveText(/^(\d+\.\d|–)$/);
      await expect(strip).toContainText('2-GW form × fixture');
      await expect(strip.getByTestId('win-chance-basis')).toContainText('Based on recent scores');
      // The next gameweek's pairing shows them before kickoff too.
      await visible(page, 'gw-next').first().click();
      const preview = visible(page, 'matchup-preview');
      await expect(preview).toBeVisible();
      await expect(preview.getByTestId('expected-left')).toHaveText(/^\d+\.\d$/);
      await expect(preview.getByTestId('win-chance-basis')).toContainText('Based on recent scores');
    });

    test('players: rank chips on the rows and on the card', async ({ page }) => {
      await page.goto(`${LEAGUE}/players`);
      const chips = page.locator('[data-testid="fixture-difficulty"]:visible:not([inert] *)');
      await expect(chips.first()).toBeVisible();
      for (const t of (await chips.allTextContents()).slice(0, 10)) expect(t).toMatch(CHIP);
      await page.locator('[data-testid^="player-plr_"]:visible:not([inert] *)').first().click();
      const card = visible(page, 'player-card');
      await expect(card).toBeVisible();
      const next = card.getByTestId('card-next');
      // A blank next gameweek has no next fixture; otherwise each shows its rank.
      if ((await next.count()) > 0) {
        await expect(next.getByTestId('fixture-difficulty').first()).toHaveText(CHIP);
      }
    });
  });
}
