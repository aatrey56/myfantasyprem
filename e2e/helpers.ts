import { expect, type Page } from '@playwright/test';

/** The demo league in apps/app/src/mocks/data.ts. */
export const LEAGUE = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hdy';
export const LEAGUE_NAME = 'The Sunday Draft';
/** The mock league mid-draft, the demo user on the clock at pick 43. */
export const DRAFT_LEAGUE = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd1';
/** A mock league drafted mid-season: its first gameweek (8) has not started. */
export const LATE_LEAGUE = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd4';
/** A pre-draft league with 5 of 8 managers; the demo user is commissioner. */
export const PREDRAFT_LEAGUE = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd2';
/** A pre-draft league with 2 of 6 managers; the demo user is commissioner and can start it. */
export const DUO_LEAGUE = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd8';
/** A pre-draft league the demo user is not in, and its invite code. */
export const PUB_INVITE_CODE = 'DERBY7XQ';
export const PUB_LEAGUE_NAME = 'The Pub League';

/**
 * A stored session for the mock demo user (the MSW mocks accept any token).
 * Same shape the app writes to localStorage after signing in.
 */
export const MOCK_SESSION = {
  accessToken: 'mock-access-token',
  refreshToken: 'mock-refresh-token-0001',
  expiresAt: '2099-01-01T00:00:00Z',
  refreshExpiresAt: '2099-01-01T00:00:00Z',
  user: {
    id: 'usr_01k7m2xq8z0000000000000001',
    displayName: 'Sam P.',
    email: 'sam@example.com',
    avatarColor: '#2f6fed',
    authProviders: ['apple', 'email'],
    createdAt: '2026-07-01T09:00:00Z',
  },
};

/** Any 6-digit code but this one signs in on the mocks (apps/app/src/mocks/handlers.ts). */
export const MOCK_CODE = '420917';
export const MOCK_WRONG_CODE = '000000';
/** A magic-link token the mocks accept. */
export const MOCK_LINK_TOKEN = 'mock-magic-link-token-0001';

/** Asks for a sign-in email from the sign-in screen and types its code. */
export async function signInWithCode(page: Page, email = 'sam@example.com') {
  await page.getByTestId('sign-in-email').fill(email);
  await page.getByTestId('sign-in-email-submit').click();
  await page.getByLabel('Sign-in code, digit 1 of 6').fill(MOCK_CODE);
}

/** Starts the page signed in (sets the stored session before the app loads). */
export async function signIn(page: Page) {
  await page.addInitScript((session) => {
    localStorage.setItem('mfp.session', JSON.stringify(session));
  }, MOCK_SESSION);
}

/** Document-level horizontal overflow (scrolling panes like the draft board are fine). */
export async function pageOverflow(page: Page) {
  return page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
}

/** Failure overrides defined in apps/app/src/mocks/scenarios.ts. */
export type Scenario =
  | 'league-500'
  | 'league-404'
  | 'league-network-error'
  | 'league-season-over'
  | 'branded-off'
  | 'assets-missing'
  | 'lineup-save-refused'
  | 'free-agent-refused'
  | 'trade-refused'
  | 'status-rank-up'
  | 'news-503'
  | 'transactions-locked';

/** Applies a scenario from the first request on (set before the page loads). */
export async function startWithScenario(page: Page, scenario: Scenario) {
  await page.addInitScript((name) => localStorage.setItem('mfp.mockScenario', name), scenario);
}

/** Switches the running MSW worker to a scenario, or back to normal. */
export async function useScenario(page: Page, scenario: Scenario | null) {
  await page.evaluate((name) => {
    localStorage.removeItem('mfp.mockScenario');
    const mocks = window.__mfpMocks!;
    mocks.reset();
    if (name) mocks.use(name as never);
  }, scenario);
}

/**
 * Elements that poke out past the right edge of the viewport. Content inside
 * a horizontal scroller that itself fits is fine: it scrolls, not the page
 * (e.g. the Players filter chip row). So is content clipped by a container
 * that fits: the league pager's off-screen pages wait to the right.
 */
export async function overflowingElements(page: Page) {
  return page.evaluate(() => {
    const limit = window.innerWidth + 1;
    const inScroller = (el: HTMLElement) => {
      for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) {
        if (
          /auto|scroll|hidden|clip/.test(getComputedStyle(p).overflowX) &&
          p.getBoundingClientRect().right <= limit
        ) {
          return true;
        }
      }
      return false;
    };
    return [...document.querySelectorAll<HTMLElement>('body *')]
      .filter((el) => el.getBoundingClientRect().right > limit && el.offsetParent !== null)
      .filter((el) => !inScroller(el))
      .map((el) => `${el.tagName}[${el.getAttribute('data-testid') ?? el.className}]`)
      .slice(0, 5);
  });
}

declare global {
  interface Window {
    __mfpMocks?: { use: (name: Scenario) => void; reset: () => void };
  }
}

/**
 * The League tab has loaded its data: it has no page title (Sleeper's
 * layout), so check its Matchups heading and the league's name in the bar
 * above the tabs.
 */
export async function expectLeagueTab(page: Page) {
  await expect(page.locator('[data-testid="league-matchups-header"]:visible')).toBeVisible();
  await expect(page.locator('[data-testid="league-switcher-name"]:visible')).toHaveText(
    LEAGUE_NAME,
  );
}
