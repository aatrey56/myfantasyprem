import { defineConfig, devices } from '@playwright/test';

// PW_PORT moves the build's server off :4174 so runs can go side by side (with E2E_API_PORT).
const PORT = Number(process.env.PW_PORT ?? 4174);
// scripts/e2e-server.mjs exports the build into its own temp dir (E2E_DIST).
const DIST = process.env.E2E_DIST ?? 'apps/app/dist-server';

// The web app against the real Go server (not the mocks): run it with
// `npm run test:e2e:server`, which starts Postgres, the API and this
// build (scripts/e2e-server.mjs). Not part of .office/checks: it needs Docker.
export default defineConfig({
  testDir: 'e2e-server',
  // Per port: runs side by side must not share (and delete) each other's traces.
  outputDir: `test-results/server-${PORT}`,
  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  reporter: 'list',
  use: {
    ...devices['Desktop Chrome'],
    baseURL: `http://localhost:${PORT}`,
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
  webServer: {
    command: `node apps/app/scripts/serve-dist.mjs ${DIST} ${PORT}`,
    url: `http://localhost:${PORT}`,
    // A stray server on the port may be serving another run's build: fail instead.
    reuseExistingServer: false,
  },
});
