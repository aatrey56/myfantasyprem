import { readFileSync, statSync } from 'node:fs';

import { expect, test, type Browser, type Page } from '@playwright/test';

// Email sign-in against the real API, signed out. The API runs with
// AUTH_DEV_MAGIC_LINKS=true and scripts/e2e-server.mjs copies its log to
// E2E_API_LOG, where the "dev magic link" lines give the link and the code
// (it is the only way to read a code without a mailbox).
const SESSION = process.env.E2E_SESSION ?? '';
const API = process.env.E2E_API ?? 'http://localhost:8091';
const LOG = process.env.E2E_API_LOG ?? '';

test.skip(!SESSION, 'run through npm run test:e2e:server');

async function signedOut(browser: Browser): Promise<Page> {
  const ctx = await browser.newContext({ baseURL: test.info().project.use.baseURL });
  return ctx.newPage();
}

const logSize = () => statSync(LOG).size;

/** The first sign-in email logged after offset: its link and code. */
async function mailed(offset: number): Promise<{ link: string; code: string }> {
  let found: { link: string; code: string } | null = null;
  await expect
    .poll(
      () => {
        const text = readFileSync(LOG, 'utf8').slice(offset);
        const line = text.split('\n').find((l) => l.includes('dev magic link'));
        const link = line?.match(/link="?([^"\s]+)/)?.[1];
        const code = line?.match(/code="?(\d{6})/)?.[1];
        found = link && code ? { link, code } : null;
        return found;
      },
      { message: `a dev magic link in ${LOG}` },
    )
    .not.toBeNull();
  return found!;
}

async function requestEmail(page: Page, email: string) {
  const offset = logSize();
  await page.getByTestId('sign-in-email').fill(email);
  await page.getByTestId('sign-in-email-submit').click();
  await expect(page.getByTestId('email-sent')).toContainText(email);
  return mailed(offset);
}

test('sign in with the emailed code', async ({ browser }) => {
  expect(LOG, 'E2E_API_LOG').not.toBe('');
  const page = await signedOut(browser);
  const email = `code-${Date.now()}@example.com`;
  await page.goto('/sign-in');
  const { code } = await requestEmail(page, email);
  // A wrong code first: refused, the boxes clear.
  const wrong = String((Number(code) + 1) % 1_000_000).padStart(6, '0');
  await page.getByLabel('Sign-in code, digit 1 of 6').fill(wrong);
  await expect(page.getByTestId('sign-in-code-error')).toContainText('wrong or has expired');
  await page.keyboard.type(code);
  await expect(page).toHaveURL(/\/leagues$/);
  const stored = await page.evaluate(() => localStorage.getItem('mfp.session'));
  const session = JSON.parse(stored ?? '{}') as { accessToken?: string };
  const me = await page.request.get(`${API}/v1/me`, {
    headers: { Authorization: `Bearer ${session.accessToken}` },
  });
  expect(me.status()).toBe(200);
  expect(((await me.json()) as { data: { email: string } }).data.email).toBe(email);
  await page.context().close();
});

test('an invite opened signed out signs in by code and lands on joining it', async ({
  browser,
  page,
}) => {
  // The demo commissioner makes a league and an invite.
  const { accessToken } = JSON.parse(SESSION) as { accessToken: string };
  const headers = { Authorization: `Bearer ${accessToken}` };
  const name = `Code invite ${Date.now()}`;
  const created = await page.request.post(`${API}/v1/leagues`, {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: { name, teamCount: 4, teamName: 'Host FC' },
  });
  expect(created.status()).toBe(201);
  const league = ((await created.json()) as { data: { league: { id: string } } }).data.league;
  const res = await page.request.post(`${API}/v1/leagues/${league.id}/invites`, {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: {},
  });
  const { url } = ((await res.json()) as { data: { url: string } }).data;
  const join = new URL(url).pathname;

  // A friend, signed out, opens it and signs in by email.
  const friend = await signedOut(browser);
  await friend.goto(join);
  await expect(friend.getByTestId('invite-league-name')).toHaveText(name);
  await friend.getByTestId('join-sign-in').click();
  const { link, code } = await requestEmail(friend, `friend-${Date.now()}@example.com`);
  // The link carries the invite, for opening it in another tab or browser.
  expect(new URL(link).searchParams.get('next')).toBe(join);
  await friend.getByLabel('Sign-in code, digit 1 of 6').fill(code);
  await expect(friend).toHaveURL(new RegExp(`${join}$`));
  await expect(friend.getByTestId('invite-league-name')).toHaveText(name);
  await expect(friend.locator('[data-testid="join-team-name"]:visible')).toHaveValue(/ FC$/);
  await friend.locator('[data-testid="join-submit"]:visible').click();
  await expect(friend).toHaveURL(new RegExp(`/leagues/${league.id}$`));
  await friend.context().close();
});

test('the emailed link opened in another browser lands on the invite', async ({
  browser,
  page,
}) => {
  const { accessToken } = JSON.parse(SESSION) as { accessToken: string };
  const headers = { Authorization: `Bearer ${accessToken}` };
  const name = `Link invite ${Date.now()}`;
  const created = await page.request.post(`${API}/v1/leagues`, {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: { name, teamCount: 4, teamName: 'Host FC' },
  });
  const league = ((await created.json()) as { data: { league: { id: string } } }).data.league;
  const res = await page.request.post(`${API}/v1/leagues/${league.id}/invites`, {
    headers: { ...headers, 'Idempotency-Key': crypto.randomUUID() },
    data: {},
  });
  const join = new URL(((await res.json()) as { data: { url: string } }).data.url).pathname;

  const friend = await signedOut(browser);
  await friend.goto(join);
  await friend.getByTestId('join-sign-in').click();
  const { link } = await requestEmail(friend, `linked-${Date.now()}@example.com`);
  const other = await signedOut(browser);
  await other.goto(link);
  await other.getByTestId('magic-link-continue').click();
  await expect(other).toHaveURL(new RegExp(`${join}$`));
  await expect(other.locator('[data-testid="join-submit"]:visible')).toBeVisible();
  await friend.context().close();
  await other.context().close();
});
