import { expect, test, type Page } from './fixtures';

import { signIn } from './helpers';

// An FPL Draft league imported (ADR 0012), on the mocks
// (apps/app/src/mocks/imported.ts): import it, see the status, share a claim
// link, claim a team as someone else, play it like any league (sign, waivers,
// trades), see a trade pending on FPL, and, as commissioner, re-sync it from
// FPL or check it against FPL.
const IMPORTED = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd5';
const IMPORTED_NAME = 'Thursday Night Draft';
const MISMATCH = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd6';
const CLAIM_LEAGUE = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd7';
const PENDING = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd9';
const CLAIM_CODE = 'SEATCHKX42';
const CLAIM_TEAM = 'Away End Allstars';

/** Tabs keep earlier screens mounted but hidden: only ever match what is on screen. */
const visible = (page: Page, testId: string) =>
  page.locator(`[data-testid="${testId}"]:visible:not([inert] *)`);

test.describe('signed in', () => {
  test.beforeEach(async ({ page }) => signIn(page));

  test('import a league: preview, pick my team, progress, the league opens playable', async ({
    page,
  }) => {
    await page.goto('/import');
    await page.getByTestId('league-input').fill('https://draft.premierleague.com/league/12345');
    await page.getByTestId('find-button').click();
    await expect(page.getByTestId('preview-name')).toHaveText(IMPORTED_NAME);
    await expect(page.getByTestId('preview-facts')).toHaveText('8 teams · gameweek 7');
    await expect(page.getByTestId('import-button')).toBeDisabled();
    await page.getByTestId('seat-option-1').click();
    await page.getByTestId('import-button').click();
    await expect(page.getByTestId('import-status')).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`${IMPORTED}$`), { timeout: 10_000 });
    // The FPL status lives in settings only, never on the league's tabs.
    await expect(page.getByText(/Imported from FPL|Matches FPL/)).toHaveCount(0);
  });

  test('on a server without FPL player data the preview says so before a team is picked', async ({
    page,
  }) => {
    await page.goto('/import');
    // The mocks answer this id as a server on the demo world would.
    await page.getByTestId('league-input').fill('https://draft.premierleague.com/league/409409');
    await page.getByTestId('find-button').click();
    await expect(page.getByTestId('preview-name')).toHaveText(IMPORTED_NAME);
    await expect(page.getByTestId('preview-not-importable')).toHaveText(
      "This server isn't connected to live player data, so FPL Draft leagues can't be imported here.",
    );
    await expect(page.getByTestId('seat-list')).toHaveCount(0);
    await expect(page.getByTestId('import-button')).toBeDisabled();
    await expect(page.getByText(/still loading|Try again/)).toHaveCount(0);
  });

  test('the commissioner sees the check against FPL: matching, or the mismatches listed', async ({
    page,
  }) => {
    await page.goto(`${IMPORTED}/settings`);
    await expect(page.getByTestId('sync-panel')).toContainText('Imported from FPL · last synced');
    await expect(page.getByTestId('verification-status')).toHaveText('Matches FPL');
    await expect(page.getByTestId('verification-item')).toHaveCount(0);

    await page.goto(`${MISMATCH}/settings`);
    await expect(page.getByTestId('verification-status')).toHaveText('2 mismatches');
    const items = page.getByTestId('verification-item');
    await expect(items).toHaveCount(2);
    await expect(items.first()).toContainText('GW5 · Corner Flag Crew · Points');
    await expect(items.first()).toContainText('Ours 61 · FPL 63');
    // Informational: no switch-over, and nothing waits on the mismatches.
    await expect(page.getByText(/switch/i)).toHaveCount(0);
    await expect(page.getByTestId('resync')).toBeEnabled();
    await page.getByTestId('check-sync').click();
    await expect(page.getByTestId('verification-item')).toHaveCount(2);
    await expect(page.getByTestId('sync-panel-pending')).toHaveCount(0, { timeout: 10_000 });
  });

  test('the automatic checks list a final mismatch apart from what FPL is still updating', async ({
    page,
  }) => {
    await page.goto(`${MISMATCH}/settings`);
    await expect(page.getByTestId('shadow-status')).toHaveText('1 mismatch');
    await expect(page.getByTestId('shadow-check')).toContainText('after bonus, GW7');
    const mismatch = page.getByTestId('shadow-mismatches').getByTestId('shadow-item');
    await expect(mismatch).toHaveCount(1);
    await expect(mismatch).toContainText('Player points');
    await expect(mismatch).toContainText('Ours 6 · FPL 7');
    await expect(mismatch).toContainText('Saka: ours 6, FPL 7.');
    const pending = page.getByTestId('shadow-pending').getByTestId('shadow-item');
    await expect(pending).toHaveCount(1);
    await expect(pending).toContainText('FPL updating');

    // A league whose automatic checks have not run shows none.
    await page.goto(`${IMPORTED}/settings`);
    await expect(page.getByTestId('sync-panel')).toBeVisible();
    await expect(page.getByTestId('shadow-check')).toHaveCount(0);
  });

  test('copy a claim link for an unclaimed team, and issue a new one', async ({
    page,
    context,
    browserName,
  }) => {
    // Only chromium can grant clipboard-read; WebKit refuses readText() outside
    // a real paste. There the test reads back what the app wrote through the
    // real navigator.clipboard.writeText, after it resolved.
    if (browserName === 'chromium') {
      await context.grantPermissions(['clipboard-read', 'clipboard-write']);
    } else {
      await page.addInitScript(() => {
        const clip = navigator.clipboard;
        const write = clip.writeText.bind(clip);
        clip.writeText = (text: string) =>
          write(text).then(() => {
            (window as unknown as { __copied: string }).__copied = text;
          });
      });
    }
    const copied = (chromium: boolean) =>
      chromium
        ? navigator.clipboard.readText()
        : (window as unknown as { __copied: string }).__copied;
    await page.goto(`${IMPORTED}/settings`);
    const links = page.getByTestId('claim-links');
    await expect(links).toContainText('not claimed yet');
    await expect(links).toContainText('claimed by Priya N.');
    const url = (await links.getByTestId('claim-link-url').first().textContent())!;
    expect(url).toMatch(/\/join\/[2-9A-HJKMNP-TV-Z]{10}$/);
    await links.getByTestId('claim-copy').first().click();
    await expect(page.getByTestId('toast')).toContainText('Claim link copied');
    expect(await page.evaluate(copied, browserName === 'chromium')).toBe(url);

    await links.getByTestId('claim-resend').first().click();
    await page.getByRole('button', { name: 'Make a new link' }).click();
    await expect(page.getByTestId('toast')).toContainText('New claim link ready');
    await expect(links.getByTestId('claim-link-url').first()).not.toHaveText(url);
  });

  test('plays like any league: sign a free agent from the player card', async ({ page }) => {
    await page.goto(`${IMPORTED}/players`);
    await expect(page.getByText(/Synced from FPL|switch-over|Make this change in FPL/)).toHaveCount(
      0,
    );
    const add = page
      .locator('[data-testid^="move-"]:visible:not([inert] *)[aria-label^="Add "]')
      .first();
    await expect(add).toBeVisible();
    const faId = (await add.getAttribute('data-testid'))!.replace('move-', '');
    await page.locator(`[data-testid="player-${faId}"]:visible:not([inert] *)`).click();
    const card = visible(page, 'player-card');
    await expect(card.getByTestId('card-add')).toHaveText('Sign');
    await card.getByTestId('card-add').click();
    await expect(visible(page, 'drop-step')).toBeVisible();
    await page.locator('[data-testid^="drop-plr_"]:visible:not([inert] *)').first().click();
    await visible(page, 'drop-confirm').click();
    await expect(page.getByTestId('toast')).toContainText(/^Added .+, dropped .+/);
  });

  test('a trade accepted on FPL shows as pending there, and a re-sync keeps it', async ({
    page,
  }) => {
    await page.goto(PENDING);

    // The whole league sees it in review, both teams and all players, with no actions.
    await page.goto(`${PENDING}/players/trades`);
    const card = visible(page, 'trade-list')
      .locator('[data-testid^="trade-trd_"]')
      .filter({ has: page.getByTestId('trade-pending-fpl') });
    await expect(card).toHaveCount(1);
    await expect(card.getByTestId('trade-status')).toHaveText('Pending on FPL');
    await expect(card).toContainText('Wet Tuesday Wanderers and Cup Run Collective');
    await expect(card.getByTestId('trade-pending-fpl')).toContainText(
      'waiting for FPL to process or veto it',
    );
    await expect(card.getByTestId('trade-actions')).toHaveCount(0);
    await expect(card.getByTestId('commissioner-actions')).toHaveCount(0);

    // The commissioner's check: pending on FPL, not a mismatch.
    await page
      .locator(
        '[data-testid="top-bar-settings"]:visible:not([inert] *), [data-testid="nav-settings"]:visible:not([inert] *)',
      )
      .first()
      .click();
    await expect(page).toHaveURL(new RegExp(`${PENDING}/settings$`));
    await expect(page.getByTestId('verification-status')).toHaveText(
      'Matches FPL · 1 trade pending on FPL',
    );
    const pending = page.getByTestId('verification-pending-item');
    await expect(pending).toHaveCount(1);
    await expect(pending).toContainText('Pending on FPL');
    await expect(pending).toContainText('GW8 · Wet Tuesday Wanderers · Pending FPL trade');
    await expect(page.getByTestId('verification-item')).toHaveCount(0);

    // A re-sync cancels only what was made here: FPL's pending trade stays.
    await page.getByTestId('resync').click();
    await page.getByTestId('resync-confirm').getByTestId('confirm-ok').click();
    await expect(page.getByTestId('sync-panel-note')).toContainText(/^Re-synced from FPL\./, {
      timeout: 10_000,
    });
    await expect(page.getByTestId('verification-pending-item')).toHaveCount(1);
  });

  test('plays like any league: a waiver claim and a trade offer can be made', async ({ page }) => {
    await page.goto(`${IMPORTED}/players`);
    await visible(page, 'view-menu').click();
    await visible(page, 'view-pos-MID').click();
    await expect(visible(page, 'view-sheet')).toHaveCount(0);
    const claim = page
      .locator('[data-testid^="move-"]:visible:not([inert] *)[aria-label^="Claim "]')
      .first();
    await expect(claim).toHaveAccessibleName(/^Claim /);
    await claim.click();
    await page.locator('[data-testid^="drop-plr_"]:visible:not([inert] *)').last().click();
    await visible(page, 'drop-confirm').click();
    await expect(page.getByTestId('toast')).toContainText('Claim placed');
    await expect(page).toHaveURL(/\/players\/waivers$/);

    await page.goto(`${IMPORTED}/players/trades`);
    await expect(visible(page, 'new-trade')).toBeEnabled();
    await visible(page, 'new-trade').click();
    await page.locator('[data-testid^="trade-team-"]:visible:not([inert] *)').first().click();
    // The + beside a row adds the player (the row itself opens the card).
    await page
      .locator('[data-testid^="give-plr_"]:visible:not([inert] *)')
      .filter({ hasText: 'MID' })
      .first()
      .getByTestId(/^give-toggle-/)
      .click();
    await page
      .locator('[data-testid^="get-plr_"]:visible:not([inert] *)')
      .filter({ hasText: 'MID' })
      .first()
      .getByTestId(/^get-toggle-/)
      .click();
    await expect(visible(page, 'trade-check')).toContainText('a legal trade');
    await visible(page, 'trade-next').click();
    await visible(page, 'trade-message').fill('Fancy a swap?');
    await visible(page, 'trade-send').click();
    await expect(page.getByTestId('toast')).toContainText('Offer sent');
    await expect(page).toHaveURL(/\/players\/trades\?tab=outgoing$/);
  });

  test('lineups and league settings are open to edit', async ({ page }) => {
    await page.goto(`${IMPORTED}/team`);
    await expect(page.getByText(/Synced from FPL|Make this change in FPL/)).toHaveCount(0);
    await expect(visible(page, 'lineup-synced')).toHaveCount(0);
    await page.goto(`${IMPORTED}/settings`);
    await expect(page.getByTestId('settings-synced')).toHaveCount(0);
    await expect(page.getByTestId('save-settings')).toBeVisible();
    await expect(page.getByTestId('settings-name')).toBeEditable();
  });

  test('re-sync from FPL: warning first, then the notice for the league', async ({ page }) => {
    await page.goto(IMPORTED);
    // In-app navigation: a reload would reset the mocks.
    await page
      .locator(
        '[data-testid="top-bar-settings"]:visible:not([inert] *), [data-testid="nav-settings"]:visible:not([inert] *)',
      )
      .first()
      .click();
    await expect(page).toHaveURL(new RegExp(`${IMPORTED}/settings$`));
    await page.getByTestId('resync').click();
    const dialog = page.getByTestId('resync-confirm');
    await expect(dialog).toContainText('Re-sync from FPL?');
    await expect(dialog).toContainText(
      /^Re-sync from FPL\?This replaces all moves made here since .+ with FPL's current state: squads, waiver order, trades, waiver claims and lineups \(team names and settings too\)\. Open trades made here and pending claims are cancelled\. Scores and standings are recomputed\./,
    );
    // Cancelling changes nothing.
    await dialog.getByTestId('confirm-cancel').click();
    await expect(dialog).toHaveCount(0);
    await expect(page.getByTestId('sync-panel-note')).toHaveCount(0);

    await page.getByTestId('resync').click();
    await page.getByTestId('resync-confirm').getByTestId('confirm-ok').click();
    await expect(page.getByTestId('toast')).toContainText('Re-sync started');
    // What it cancelled shows in settings only.
    await expect(page.getByTestId('sync-panel-note')).toContainText(
      /^Re-synced from FPL\. .+ (was|were) cancelled\.$/,
      { timeout: 10_000 },
    );
    await expect(page.getByTestId('sync-panel-pending')).toHaveCount(0);
    await expect(page.getByTestId('resync')).toBeEnabled();
    await page.goBack();
    await expect(page).toHaveURL(new RegExp(`${IMPORTED}$`));
    await expect(
      visible(page, 'transaction-phase').or(visible(page, 'league-tabs')).first(),
    ).toBeVisible();
    await expect(page.getByText(/Re-synced from FPL/)).toHaveCount(0);
  });
});

test('claim a team with its link as another user: sign in, come back, claim', async ({ page }) => {
  await page.goto(`/join/${CLAIM_CODE}`);
  await expect(page.getByTestId('invite-league-name')).toHaveText('Away Days Draft');
  await expect(page.getByTestId('claim-team')).toContainText(CLAIM_TEAM);
  await page.getByTestId('join-sign-in').click();
  await expect(page).toHaveURL(/\/sign-in\?next=/);
  await page.getByTestId('sign-in-apple').click();
  await expect(page).toHaveURL(new RegExp(`/join/${CLAIM_CODE}$`));
  await page.getByTestId('claim-seat-button').click();
  await expect(page).toHaveURL(new RegExp(`${CLAIM_LEAGUE}/team$`));
  await expect(page.getByTestId('toast')).toContainText(`${CLAIM_TEAM} is your team now`);
  // Nothing about FPL on the league's tabs.
  await expect(page.getByText(/Imported from FPL|Matches FPL|FPL check/)).toHaveCount(0);
});

// Testing phase (docs/adr/0013): the claim link alone signs a friend in.
test('signed out, a claim link and a name claim the team and sign in', async ({ page }) => {
  await page.goto(`/join/${CLAIM_CODE}`);
  await expect(page.getByTestId('claim-heading')).toHaveText(
    `Claim ${CLAIM_TEAM} in Away Days Draft`,
  );
  // No email: a name is all it asks for.
  await expect(page.getByRole('textbox')).toHaveCount(1);
  await page.getByTestId('claim-sign-in-button').click();
  await expect(page.getByText('Your name must be 1 to 32 characters.')).toBeVisible();
  await page.getByTestId('claim-name').fill('Jordan K.');
  await page.getByTestId('claim-sign-in-button').click();
  await expect(page).toHaveURL(new RegExp(`${CLAIM_LEAGUE}/team$`));
  await expect(page.getByTestId('toast')).toContainText(`${CLAIM_TEAM} is your team now`);
  // Signed in, and the claimed seat is my team.
  await expect(page).toHaveTitle(/^My team · /);
  await expect(page.getByRole('heading', { level: 1, name: CLAIM_TEAM })).toBeVisible();
  // My lineup, so I can make swaps.
  await expect(page.getByRole('button', { name: /^Swap / }).first()).toBeVisible();
  const stored = await page.evaluate(() => localStorage.getItem('mfp.session'));
  expect(JSON.parse(stored!)).toMatchObject({ user: { displayName: 'Jordan K.' } });
  // The used link no longer claims anything.
  await page.goto(`/join/${CLAIM_CODE}`);
  await expect(page.getByTestId('claim-sign-in')).toHaveCount(0);
});
