// Writes screenshots (375 and 1280 wide, dark) to docs/screenshots/:
// league home, the leagues hub, the draft room, the team lineup, the live
// matchup, players, a player card, the trades inbox and review, activity
// and deadlines.
// Usage: npm run screenshots   (rebuilds the web app with mocks first)
import { spawn } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from '@playwright/test';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
const port = 4174;
const outDir = join(root, 'docs', 'screenshots');
mkdirSync(outDir, { recursive: true });

// The mock demo user, signed in (the MSW mocks accept any token).
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

const SHOTS = [
  { name: 'status', path: '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hdy', ready: 'totw-pitch' },
  { name: 'league-home', path: '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hdy/league', ready: 'loaded' },
  { name: 'hub', path: '/leagues', ready: 'league-list' },
  {
    name: 'draft-room',
    path: '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd1/draft',
    ready: 'draft-clock',
  },
  { name: 'team', path: '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hdy/team', ready: 'pitch-xi' },
  {
    name: 'matchup',
    path: '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hdy/matchup',
    ready: 'h2h-starters',
  },
  { name: 'players', path: '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hdy/players', ready: 'player-list' },
  {
    name: 'player-card',
    path: '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hdy/players',
    ready: 'player-list',
    click: '[data-testid^="player-plr_"]',
    then: 'card-tiles',
  },
  // The Office League: offers both ways and a trade in a league vote.
  {
    name: 'trades',
    path: '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd0/players/trades',
    ready: 'trades-incoming',
  },
  // The same league's trade in review: league vote and commissioner powers.
  {
    name: 'trades-review',
    path: '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hd0/players/trades',
    ready: 'trades-incoming',
    click: '[data-testid="bucket-review"]',
    then: 'commissioner-actions',
  },
  {
    name: 'activity',
    path: '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hdy/activity',
    ready: 'activity-feed',
  },
  {
    name: 'deadlines',
    path: '/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hdy/activity/deadlines',
    ready: 'deadline-next',
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
  for (const [size, width, height] of [
    ['375', 375, 812],
    ['1280', 1280, 800],
  ]) {
    for (const shot of SHOTS) {
      const page = await browser.newPage({ viewport: { width, height }, colorScheme: 'dark' });
      await page.addInitScript(
        (s) => localStorage.setItem('mfp.session', JSON.stringify(s)),
        SESSION,
      );
      await page.goto(`http://localhost:${port}${shot.path}`);
      await page.getByTestId(shot.ready).first().waitFor();
      if (shot.click) {
        await page.locator(`${shot.click}:visible`).first().click();
        await page.locator(`[data-testid="${shot.then}"]:visible`).first().waitFor();
      }
      // Let fonts, icons and the first poll settle.
      await page.waitForTimeout(800);
      await page.screenshot({ path: join(outDir, `${shot.name}-${size}.png`) });
      await page.close();
    }
  }
  await browser.close();
} finally {
  server.kill();
}
