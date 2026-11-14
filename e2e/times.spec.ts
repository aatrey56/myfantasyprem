import { expect, test, type Page } from './fixtures';

import { LEAGUE, signIn } from './helpers';

// Dates and times follow apps/app/src/lib/time.ts. The mocks place every
// deadline and event relative to the page's clock, so with the clock at
// Tue 13 Oct 2026, 09:00 in London the text is exact: gameweek 8 locks six
// days on (Mon 19 Oct, 09:00), waivers a day earlier and trades two days
// earlier. Gameweek 9 is 7 x 24 hours later, after the clocks go back on
// 25 Oct: 08:00.
const NOW = new Date('2026-10-13T08:00:00Z');

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);

/** "in 4d 0h" a moment before the clock moves on, "in 3d 23h" after. */
const inDays = (d: number) => new RegExp(`in (${d}d 0h|${d - 1}d 23h)`);

test.use({ viewport: { width: 375, height: 812 } });

test.beforeEach(async ({ page }) => {
  await signIn(page);
  await page.clock.install({ time: NOW });
});

test.describe('in London', () => {
  test.use({ timezoneId: 'Europe/London' });

  test('deadlines show the day and time, and the time left', async ({ page }) => {
    await page.goto(`${LEAGUE}/activity/deadlines`);
    await expect(page.getByTestId('deadlines-zone')).toContainText('Europe/London');
    const next = page.getByTestId('deadline-next');
    await expect(next).toContainText('Sat 17 Oct, 09:00');
    await expect(page.getByRole('timer')).toHaveAccessibleName(
      new RegExp(`^Trade deadline for Gameweek 8 ${inDays(4).source}$`),
    );
    await expect(page.getByTestId('countdown')).toHaveText(/^(4d 0h|3d 23h)$/);
    const lock = page.getByTestId('deadline-gw-8-lock');
    await expect(lock).toContainText('Mon 19 Oct, 09:00');
    await expect(lock).toContainText(inDays(6));
    await expect(page.getByTestId('deadline-gw-8-waivers')).toContainText('Sun 18 Oct, 09:00');
  });

  test('the Status page lists deadlines the same way', async ({ page }) => {
    await page.goto(LEAGUE);
    const deadlines = visible(page, 'status-deadlines');
    await expect(visible(page, 'deadline-waivers')).toContainText('Sun 18 Oct, 09:00');
    await expect(visible(page, 'deadline-waivers')).toContainText(inDays(5));
    await expect(deadlines).toContainText('Mon 19 Oct, 09:00');
  });

  test('the lineup lock reads as the day and time in the hint line, never m:ss', async ({
    page,
  }) => {
    await page.goto(`${LEAGUE}/team`);
    // Six days out exactly it carries the date; a moment later just the day.
    await expect(visible(page, 'lineup-open')).toHaveText(/^Locks Mon (19 Oct, )?09:00$/);
    await expect(visible(page, 'lineup-open')).toHaveAccessibleName(
      'Lineup locks Mon 19 Oct, 09:00',
    );
  });

  test('the activity feed says how long ago, then the day', async ({ page }) => {
    await page.goto(`${LEAGUE}/activity`);
    const times = visible(page, 'activity-time');
    // Transactions every 5.5 hours back from now.
    await expect(times.first()).toHaveText('5h ago');
    await expect(times.nth(1)).toHaveText('11h ago');
    await expect(times.nth(4)).toHaveText('Mon 12 Oct');
  });

  test('trade offers say how long ago they were made', async ({ page }) => {
    await page.goto(`${LEAGUE}/players/trades`);
    const offered = page.locator('[data-testid="trade-offered-at"]:visible:not([inert] *)');
    await expect(offered.first()).toBeVisible();
    for (const text of await offered.allTextContents()) {
      expect(text).toMatch(/^(just now|\d+m ago|\d+h ago|(Mon|Tue|Wed|Thu|Fri|Sat|Sun) \d+ Oct)$/);
    }
  });
});

test.describe('in New York', () => {
  test.use({ timezoneId: 'America/New_York' });

  test('the same deadlines are in the device time zone', async ({ page }) => {
    await page.goto(`${LEAGUE}/activity/deadlines`);
    await expect(page.getByTestId('deadlines-zone')).toContainText('America/New_York');
    await expect(page.getByTestId('deadline-next')).toContainText('Sat 17 Oct, 04:00');
    await expect(page.getByTestId('deadline-gw-8-lock')).toContainText('Mon 19 Oct, 04:00');
  });
});

test.describe('in Honolulu', () => {
  test.use({ timezoneId: 'Pacific/Honolulu' });

  test('a deadline can fall on the day before', async ({ page }) => {
    await page.goto(`${LEAGUE}/activity/deadlines`);
    // 08:00 UTC on Saturday is 22:00 on Friday in Hawaii (UTC-10).
    await expect(page.getByTestId('deadline-next')).toContainText('Fri 16 Oct, 22:00');
    await expect(page.getByTestId('deadline-gw-8-lock')).toContainText('Sun 18 Oct, 22:00');
  });
});
