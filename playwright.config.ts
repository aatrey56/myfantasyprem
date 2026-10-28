import { defineConfig, devices } from '@playwright/test';

// PW_PORT lets parallel checkouts run e2e side by side. Reusing an already
// running server is opt-in (PW_REUSE_SERVER=1): a stray server on the port
// may be serving another checkout's build, which gives false passes.
const PORT = Number(process.env.PW_PORT ?? 4173);

// Expects the mock-enabled web build in apps/app/dist-mocks: `npm run build:web:mocks`.
export default defineConfig({
  testDir: 'e2e',
  // Every request is answered by MSW on the page's own main thread, so a
  // cold screen load is several sequential round trips of mock computation.
  // The team screen paints its rows in about 2.5 s on an idle laptop but
  // took 6 to 15 s with 6 workers at load average 18 on 10 cores (other
  // worktrees' gates running): Playwright's 5 s expect default failed
  // player-cards' first assertion every time at 3+ workers. 15 s covers
  // that worst case; a genuinely wrong assertion still fails, only later.
  // The test timeout grows in step (the longest specs make ~6 such loads).
  timeout: 60_000,
  expect: { timeout: 15_000 },
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['github'], ['list']] : 'list',
  use: {
    baseURL: `http://localhost:${PORT}`,
    trace: 'retain-on-failure',
  },
  // Everything runs on chromium. Keyboard behaviour differs between engines, so
  // the import shortcut and club menu specs also run on webkit (Safari); so do
  // the branded images, which are native lazy <img> elements.
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    {
      name: 'webkit',
      use: { ...devices['Desktop Safari'] },
      testMatch: /(import-shortcut|club-menu|branded-assets)\.spec\.ts$/,
    },
  ],
  webServer: {
    command: `node apps/app/scripts/serve-dist.mjs apps/app/dist-mocks ${PORT}`,
    url: `http://localhost:${PORT}`,
    reuseExistingServer: process.env.PW_REUSE_SERVER === '1',
  },
});
