import { expect, test, type Page } from './fixtures';

import { expectLeagueTab, LEAGUE, overflowingElements, signIn } from './helpers';

// The MATCH and LEAGUE tabs in Sleeper's layout (docs/design/SLEEPER-OBSERVED.md):
// no page titles, the GW stepper on a section heading, borderless head-to-head
// rows, records with the current run, round avatars with top-three medals.

test.beforeEach(async ({ page }) => signIn(page));

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);
const visibleStarting = (page: Page, prefix: string) =>
  page.locator(`[data-testid^="${prefix}"]:visible:not([inert] *)`);

/** `Sam P. • 3-0-3 ▼2L (#6)` or mirrored `2-1-3 ▲1W (#7) • Tomasz K.` */
const RECORD = /\d+-\d+-\d+ [▲▼=]\d[WDL] \(#\d+\)/;

const box = async (page: Page, testId: string) =>
  (await visible(page, testId).first().boundingBox())!;

for (const vp of [
  { name: 'phone 390', width: 390, height: 844, nav: 'league-tabs' },
  { name: 'desktop 1280', width: 1280, height: 800, nav: 'top-nav' },
] as const) {
  test.describe(`Sleeper layout (${vp.name})`, () => {
    test.use({ viewport: { width: vp.width, height: vp.height } });

    test('MATCH: the hero sits right under the tabs, the stepper on Starters', async ({ page }) => {
      await page.goto(`${LEAGUE}/matchup`);
      await expect(visible(page, 'h2h-starters')).toBeVisible();
      // No page title, eyebrow or standalone stepper above the hero.
      await expect(page.getByRole('heading', { name: 'Matchup', exact: true })).toHaveCount(0);
      await expect(visible(page, 'eyebrow')).toHaveCount(0);
      const nav = (await page.getByTestId(vp.nav).boundingBox())!;
      const hero = await box(page, 'score-header');
      expect(hero.y - (nav.y + nav.height)).toBeLessThanOrEqual(40);
      // One stepper, on the right of the Starters heading and level with it.
      await expect(visible(page, 'gw-switcher')).toHaveCount(1);
      const heading = await box(page, 'starters-header');
      const stepper = await box(page, 'gw-switcher');
      expect(stepper.y).toBeGreaterThan(hero.y + hero.height);
      expect(stepper.x).toBeGreaterThan(heading.x + heading.width);
      expect(
        Math.abs(stepper.y + stepper.height / 2 - (heading.y + heading.height / 2)),
      ).toBeLessThan(8);
      for (const id of ['gw-prev', 'gw-next']) {
        const b = await box(page, id);
        expect(b.width).toBeGreaterThanOrEqual(44);
        expect(b.height).toBeGreaterThanOrEqual(44);
      }
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('MATCH: borderless 84pt rows, played dots, streaks and round avatars', async ({
      page,
    }) => {
      await page.goto(`${LEAGUE}/matchup`);
      const starters = visible(page, 'h2h-starters');
      await expect(starters).toBeVisible();
      // Rows float on the page: no card around them.
      const frame = await starters.evaluate((el) => {
        const s = getComputedStyle(el);
        return { border: s.borderTopWidth, background: s.backgroundColor };
      });
      expect(frame.border).toBe('0px');
      expect(frame.background).toBe('rgba(0, 0, 0, 0)');
      const rows = await starters.evaluate((el) =>
        [...el.children].map((c) => c.getBoundingClientRect().height),
      );
      expect(rows.length).toBeGreaterThanOrEqual(10);
      for (const h of rows) expect(h).toBeGreaterThanOrEqual(84);

      // A dot per starter under "yet to play (N)", filled once he has played.
      for (const side of ['left', 'right']) {
        const n = Number(
          /\((\d+)\)/.exec((await visible(page, `yet-to-play-${side}`).textContent())!)![1],
        );
        const dots = visible(page, `played-dots-${side}`);
        await expect(dots.locator('[data-testid^="dot-"]')).toHaveCount(11);
        await expect(dots.locator('[data-testid="dot-to-play"]')).toHaveCount(n);
        await expect(dots.locator('[data-testid="dot-played"]')).toHaveCount(11 - n);
      }

      // Record, current run and rank under each team name.
      const hero = visible(page, 'score-header');
      await expect(hero.getByTestId('subline-left')).toHaveText(RECORD);
      await expect(hero.getByTestId('subline-right')).toHaveText(RECORD);

      // Round 40pt team avatars.
      const avatar = hero.getByTestId('avatar-left');
      const a = (await avatar.boundingBox())!;
      expect(Math.round(a.width)).toBe(40);
      expect(Math.round(a.height)).toBe(40);
      const radius = await avatar.evaluate(
        (el) => getComputedStyle(el.firstElementChild!).borderTopLeftRadius,
      );
      expect(radius).toBe('20px');
    });

    test('MATCH: the stepper on Starters changes the gameweek', async ({ page }) => {
      await page.goto(`${LEAGUE}/matchup`);
      await expect(visible(page, 'gw-label')).toHaveText('GW 7');
      await visible(page, 'gw-prev').click();
      await expect(page).toHaveURL(/\/matchup\?gw=6$/);
      await expect(visible(page, 'gw-label')).toHaveText('GW 6');
      await expect(visible(page, 'score-state')).toHaveText('Final');
      await expect(visibleStarting(page, 'score-left-result')).toHaveCount(1);
      // Still on the Starters heading.
      const heading = await box(page, 'starters-header');
      const stepper = await box(page, 'gw-switcher');
      expect(
        Math.abs(stepper.y + stepper.height / 2 - (heading.y + heading.height / 2)),
      ).toBeLessThan(8);
    });

    test('LEAGUE: opens on the matchup cards with a stepper, then standings', async ({ page }) => {
      await page.goto(`${LEAGUE}/league`);
      await expectLeagueTab(page);
      // No page title and no Trades / Waivers / Deadlines / Draft results button row.
      await expect(page.getByTestId('league-name')).toHaveCount(0);
      for (const id of ['link-trades', 'link-waivers', 'link-deadlines']) {
        await expect(page.getByTestId(id)).toHaveCount(0);
      }
      const nav = (await page.getByTestId(vp.nav).boundingBox())!;
      const matchups = await box(page, 'league-matchups-header');
      expect(matchups.y - (nav.y + nav.height)).toBeLessThanOrEqual(40);
      const stepper = await box(page, 'gw-switcher');
      expect(stepper.x + stepper.width).toBeGreaterThan(vp.width / 2);
      expect(
        Math.abs(stepper.y + stepper.height / 2 - (matchups.y + matchups.height / 2)),
      ).toBeLessThan(8);
      const standings = await box(page, 'standings-header');
      expect(standings.y).toBeGreaterThan(matchups.y);

      // Every card: records with the run; mine first, with the accent border.
      const cards = visibleStarting(page, 'matchup-row-mu_');
      expect(await cards.count()).toBeGreaterThanOrEqual(4);
      await expect(cards.first()).toContainText('Sunday League Heroes');
      for (const sub of await cards.locator('[data-testid="subline-left"]').allTextContents()) {
        expect(sub).toMatch(RECORD);
      }
      // A medal on each of the top three's avatars.
      const medals = page.locator(
        '[data-testid="avatar-left-medal"]:visible:not([inert] *), [data-testid="avatar-right-medal"]:visible:not([inert] *)',
      );
      await expect(medals).toHaveCount(3);
      expect((await medals.allTextContents()).sort()).toEqual(['1', '2', '3']);
      if (vp.width >= 768) {
        // Two cards a row.
        const [a, b] = [(await cards.nth(0).boundingBox())!, (await cards.nth(1).boundingBox())!];
        expect(Math.round(a.y)).toBe(Math.round(b.y));
        expect(b.x).toBeGreaterThan(a.x + a.width - 1);
      }

      // The stepper: last gameweek's final results.
      await visible(page, 'gw-prev').click();
      await expect(visible(page, 'gw-label')).toHaveText('GW 6');
      await expect(visibleStarting(page, 'matchup-row-mu_').first()).toBeVisible();
      await expect(
        page
          .locator(
            '[data-testid^="matchup-row-"][data-testid$="-left-result"]:visible:not([inert] *)',
          )
          .first(),
      ).toBeVisible();
      expect(await overflowingElements(page)).toEqual([]);
    });

    test('LEAGUE: Activity and the draft results stay reachable at the foot', async ({ page }) => {
      await page.goto(`${LEAGUE}/league`);
      await expectLeagueTab(page);
      const standings = await box(page, 'standings-header');
      for (const [id, name] of [
        ['link-activity', 'Activity'],
        ['link-draft-results', 'Draft results'],
      ] as const) {
        const link = visible(page, id);
        await expect(link).toHaveAccessibleName(name);
        const b = (await link.boundingBox())!;
        expect(b.y).toBeGreaterThan(standings.y);
        expect(b.height).toBeGreaterThanOrEqual(44);
      }
      await visible(page, 'link-activity').click();
      await expect(page).toHaveURL(/\/activity\?from=league$/);
      await expect(visible(page, 'activity-feed')).toBeVisible();
    });
  });
}
