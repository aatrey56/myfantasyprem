// Writes 390px-wide dark screenshots of the Players tab, the trades view and
// the player card to docs/screenshots/wave-a/<name>-<phase>-390.png.
// Usage: npm run build:web:mocks && node scripts/screenshots-wave-a.mjs after
// (`before` was taken from the commit this wave started from).
import { spawn } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from '@playwright/test';

const phase = process.argv[2] ?? 'after';
const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
const port = Number(process.env.SHOTS_PORT ?? 4391);
const outDir = join(root, 'docs', 'screenshots', 'wave-a');
mkdirSync(outDir, { recursive: true });

const SESSION = {
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
const LEAGUE = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hdy';
const OFFICE = '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd0';
const vis = (id) => `[data-testid="${id}"]:visible`;

/** Each shot: a path, then steps run in the page before the screenshot. */
const SHOTS = [
  { name: 'players', path: `${LEAGUE}/players`, steps: [['wait', 'player-list']] },
  ...(phase === 'before'
    ? []
    : [
        {
          name: 'players-trend',
          path: `${LEAGUE}/players`,
          steps: [
            ['click', 'open-trend'],
            ['wait', 'player-list'],
          ],
        },
        {
          name: 'players-leaders',
          path: `${LEAGUE}/players`,
          steps: [
            ['click', 'open-leaders'],
            ['wait', 'player-list'],
          ],
        },
      ]),
  {
    name: 'trades',
    path: `${OFFICE}/players/trades`,
    steps: [
      [phase === 'before' ? 'wait' : 'wait', phase === 'before' ? 'trades-incoming' : 'trade-list'],
    ],
  },
  {
    name: 'player-card',
    path: `${LEAGUE}/players`,
    steps: [
      ['wait', 'player-list'],
      ['clickFirst', '[data-testid^="player-plr_"]'],
      ['wait', 'card-tiles'],
    ],
  },
  {
    name: 'player-card-own',
    path: `${LEAGUE}/players`,
    steps:
      phase === 'before'
        ? [
            ['click', 'status-rostered'],
            ['wait', 'player-list'],
            ['clickText', 'Your team'],
            ['wait', 'card-mine'],
          ]
        : [
            ['click', 'open-filters'],
            ['click', 'status-rostered'],
            ['wait', 'player-list'],
            ['clickText', 'Your team'],
            ['wait', 'card-drop'],
          ],
  },
];

const server = spawn(
  process.execPath,
  [join(root, 'apps/app/scripts/serve-dist.mjs'), join(root, 'apps/app/dist-mocks'), String(port)],
  { stdio: 'ignore' },
);

try {
  await new Promise((r) => setTimeout(r, 1000));
  const browser = await chromium.launch();
  for (const shot of SHOTS) {
    const page = await browser.newPage({
      viewport: { width: 390, height: 844 },
      colorScheme: 'dark',
    });
    await page.addInitScript(
      (s) => localStorage.setItem('mfp.session', JSON.stringify(s)),
      SESSION,
    );
    await page.goto(`http://localhost:${port}${shot.path}`);
    try {
      for (const [kind, arg] of shot.steps) {
        if (kind === 'wait') await page.locator(vis(arg)).first().waitFor({ timeout: 15000 });
        if (kind === 'click') await page.locator(vis(arg)).first().click();
        if (kind === 'clickFirst') await page.locator(`${arg}:visible`).first().click();
        if (kind === 'clickText') {
          await page
            .locator('[data-testid^="player-plr_"]:visible', { hasText: arg })
            .first()
            .click();
        }
      }
    } catch (e) {
      console.error(`${shot.name}: ${e.message.split('\n')[0]}`);
    }
    await page.waitForTimeout(800);
    await page.screenshot({ path: join(outDir, `${shot.name}-${phase}-390.png`) });
    await page.close();
  }
  await browser.close();
} finally {
  server.kill();
}
