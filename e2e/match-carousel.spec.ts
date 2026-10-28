import { expect, test, type Page } from './fixtures';

import { LEAGUE, overflowingElements, signIn } from './helpers';

// Match tab header: Sleeper's carousel of every matchup in the gameweek,
// mine first. Moving it switches the head to head below in place.

test.beforeEach(async ({ page }) => signIn(page));

const starterIds = (page: Page) =>
  page
    .locator('[data-testid="h2h-starters"]:visible [data-testid^="open-card-"]')
    .evaluateAll((els) => els.map((e) => e.getAttribute('data-testid')));

/** The names on the header now on screen. */
const headerNames = (page: Page) =>
  Promise.all([
    page.getByTestId('score-left-name').innerText(),
    page.getByTestId('score-right-name').innerText(),
  ]);

async function selectedDot(page: Page) {
  const dots = page.getByTestId('carousel-dots').getByRole('button');
  const n = await dots.count();
  const on: number[] = [];
  for (let i = 0; i < n; i++)
    if ((await dots.nth(i).getAttribute('aria-current')) === 'true') on.push(i);
  return { n, on };
}

test.describe('phone', () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test('swipe to the 2nd matchup, dots follow, a list card jumps back', async ({ page }) => {
    await page.goto(`${LEAGUE}/matchup`);
    await expect(page.getByTestId('matchup-carousel')).toBeVisible();
    await expect(page.locator('[data-testid="h2h-starters"]:visible')).toBeVisible();
    const before = await starterIds(page);
    const mine = await headerNames(page);
    const dots = await selectedDot(page);
    expect(dots.n).toBeGreaterThan(1);
    expect(dots.on).toEqual([0]);
    await expect(page.getByTestId('carousel-dot-0')).toHaveAccessibleName(/your matchup/);
    // No arrows on a phone: swipe.
    await expect(page.getByTestId('carousel-next')).toHaveCount(0);

    // A swipe: the scroller moves one page (scroll snapping lands it).
    const scroller = page.getByTestId('matchup-carousel-scroller');
    await scroller.evaluate((el) => el.scrollBy({ left: el.clientWidth, behavior: 'instant' }));
    await expect.poll(async () => (await selectedDot(page)).on).toEqual([1]);
    const second = await page.getByTestId('carousel-dot-1').getAttribute('aria-label');
    const names = await headerNames(page);
    expect(names).not.toEqual(mine);
    expect(second).toContain(`${names[0]} versus ${names[1]}`);
    await expect.poll(() => starterIds(page)).not.toEqual(before);
    await expect(page).toHaveURL(new RegExp(`${LEAGUE}/matchup$`));
    expect(await overflowingElements(page)).toEqual([]);

    // A card in the gameweek's list selects in place and scrolls to the top.
    const list = page.getByTestId('matchup-list');
    const firstCard = list.locator('[data-testid^="matchup-row-"]').filter({
      hasText: mine[0],
    });
    await firstCard.first().scrollIntoViewIfNeeded();
    await firstCard.first().click();
    await expect.poll(async () => (await selectedDot(page)).on).toEqual([0]);
    await expect.poll(() => headerNames(page)).toEqual(mine);
    await expect.poll(() => starterIds(page)).toEqual(before);
    await expect(page).toHaveURL(new RegExp(`${LEAGUE}/matchup$`));
    await expect(page.getByTestId('matchup-carousel')).toBeInViewport();
  });

  test('a list card for another matchup jumps the carousel to it', async ({ page }) => {
    await page.goto(`${LEAGUE}/matchup`);
    await expect(page.getByTestId('carousel-dot-2')).toBeVisible();
    const third = (await page.getByTestId('carousel-dot-2').getAttribute('aria-label'))!;
    const [, left] = /: (.+) versus /.exec(third)!;
    await page
      .getByTestId('matchup-list')
      .locator('[data-testid^="matchup-row-"]')
      .filter({ hasText: left })
      .first()
      .click();
    await expect.poll(async () => (await selectedDot(page)).on).toEqual([2]);
    await expect(page.getByTestId('score-left-name')).toHaveText(left);
    await expect(page.getByTestId('matchup-carousel')).toBeInViewport();
  });
});

test.describe('desktop', () => {
  test.use({ viewport: { width: 1440, height: 900 } });

  test('arrows, dots and arrow keys move the carousel', async ({ page }) => {
    await page.goto(`${LEAGUE}/matchup`);
    const prev = page.getByTestId('carousel-prev');
    const next = page.getByTestId('carousel-next');
    await expect(next).toBeVisible();
    await expect(prev).toBeDisabled();
    const mine = await headerNames(page);
    await next.click();
    await expect.poll(async () => (await selectedDot(page)).on).toEqual([1]);
    await expect.poll(() => headerNames(page)).not.toEqual(mine);
    await expect(page.getByTestId('carousel-page-1')).toBeInViewport({ ratio: 0.9 });
    await prev.click();
    await expect.poll(async () => (await selectedDot(page)).on).toEqual([0]);
    await page.getByTestId('carousel-dot-2').click();
    await expect.poll(async () => (await selectedDot(page)).on).toEqual([2]);
    await expect(page.getByTestId('carousel-page-2')).toBeInViewport({ ratio: 0.9 });
    // Keyboard: focus the carousel, arrow left.
    await page.getByLabel(/^Matchups, \d+ of \d+/).focus();
    await page.keyboard.press('ArrowLeft');
    await expect.poll(async () => (await selectedDot(page)).on).toEqual([1]);
    // The season strip, no win chance.
    await expect(page.getByTestId('score-header').getByTestId('season-start-sit')).toBeVisible();
    await expect(page.getByTestId('score-header').getByTestId('season-avg')).toHaveText(/\d+\.\d/);
  });

  test('a gameweek step opens on my matchup', async ({ page }) => {
    await page.goto(`${LEAGUE}/matchup`);
    await page.getByTestId('carousel-next').click();
    await expect.poll(async () => (await selectedDot(page)).on).toEqual([1]);
    await page.locator('[data-testid="gw-prev"]:visible').click();
    await expect.poll(async () => (await selectedDot(page)).on).toEqual([0]);
  });
});
