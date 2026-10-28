// Runs the Playwright smoke suite in e2e-server/ against the real Go API
// (not the mocks). Needs Docker: it starts its own throwaway postgres:17
// (scripts/test-postgres.mjs: unique name, tmpfs, free port on 127.0.0.1) and
// removes it afterwards, also on failure and SIGINT/SIGTERM/SIGHUP. With
// TEST_DATABASE_URL (or DATABASE_URL) set it uses that database instead,
// which it may wipe.
//   1. resets the database and seeds the demo world (cmd/migrate, cmd/seed);
//   2. builds the web app against http://localhost:8091 into a temp dir;
//   3. starts the API on :8091 (CORS and APP_URL for PW_PORT (:4174), poller on the demo source, jobs,
//      sign-in emails logged, AUTH_DEV_MAGIC_LINKS) with its log copied to a file the
//      sign-in specs read codes from (E2E_API_LOG);
//   4. signs in the demo commissioner (cmd/admin session);
//   5. runs e2e-server/smoke.spec.ts (`playwright test -c playwright.server.config.ts`),
//      then stops the API;
//   6. the FPL import (docs/adr/0012): resets the database again, starts a fake FPL
//      Draft API serving the recorded league (scripts/fpl-fixture-server.mjs), starts
//      the API on it (FPL_BASE_URL, the poller on the fpl source), waits for the poller
//      to ingest the recorded gameweeks and runs e2e-server/fpl-import.spec.ts.
// The API's image proxy (BRANDED_ASSETS) fetches from the same fake server's
// /img/ stand-ins. FPL and the image hosts are never called. Usage: npm run test:e2e:server (E2E_API_PORT=8092 and
// PW_PORT=4175 for another API and web port). Not in .office/checks.
import { Buffer } from 'node:buffer';
import { spawn } from 'node:child_process';
import { createWriteStream, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { imageTemplates, startFixtureServer } from './fpl-fixture-server.mjs';
import { onShutdown, startTestPostgres } from './test-postgres.mjs';

// E2E_API_PORT moves the API off :8091 when another checkout's server holds it.
const API_PORT = Number(process.env.E2E_API_PORT ?? 8091);
// PW_PORT moves the web build's server off :4174 (playwright.server.config.ts).
const PW_PORT = Number(process.env.PW_PORT ?? 4174);
const APP_ORIGIN = `http://localhost:${PW_PORT}`;

/**
 * Child processes in their own process groups, so a signal to this script
 * stops them and their children (expo, playwright, go run) before the
 * container goes.
 */
const children = new Set();

function launch(cmd, args, opts) {
  const child = spawn(cmd, args, { detached: true, ...opts });
  children.add(child);
  child.once('exit', () => children.delete(child));
  return child;
}

/** Runs a command to completion; exits the script on failure. Resolves with its stdout. */
function step(cmd, args, opts = {}) {
  console.log(`> ${cmd} ${args.join(' ')}`);
  return new Promise((done) => {
    const child = launch(cmd, args, { stdio: ['ignore', 'pipe', 'inherit'], ...opts });
    const out = [];
    child.stdout.on('data', (chunk) => out.push(chunk));
    child.once('error', (e) => {
      console.error(`e2e-server: ${cmd}: ${e.message}`);
      process.exit(1);
    });
    child.once('close', (status) => {
      const stdout = Buffer.concat(out).toString();
      if (status !== 0) {
        process.stdout.write(stdout);
        process.exit(status ?? 1);
      }
      done(stdout);
    });
  });
}

let DATABASE_URL = process.env.TEST_DATABASE_URL ?? process.env.DATABASE_URL;
if (!DATABASE_URL) {
  try {
    DATABASE_URL = (await startTestPostgres({ prefix: 'mfp-e2e-server' })).url;
  } catch (e) {
    console.error(`e2e-server: ${e.message}`);
    process.exit(1);
  }
}

// Per-run scratch: the web build (its own dir, so concurrent runs do not clobber
// each other), the API binary and its log.
const tmp = mkdtempSync(join(tmpdir(), 'mfp-e2e-'));
const dist = join(tmp, 'dist');
onShutdown(() => rmSync(tmp, { recursive: true, force: true }));

// Registered last, so it runs first: children stop, the scratch dir goes, then the container.
onShutdown(() => {
  for (const c of children) {
    try {
      process.kill(-c.pid, 'SIGTERM');
    } catch {
      // already gone
    }
  }
});

const env = { ...process.env, DATABASE_URL };

// The fake FPL Draft API (and image hosts), for the whole run.
const fpl = await startFixtureServer();
onShutdown(() => fpl.close());
console.log(`> fake FPL Draft API on ${fpl.url}`);
await step('go', ['run', './cmd/migrate', 'down', '-to', '0'], { cwd: 'server', env });
await step('go', ['run', './cmd/seed'], { cwd: 'server', env });
await step('node', ['scripts/copy-msw-worker.mjs', '--remove', 'public'], { cwd: 'apps/app' });
await step('npx', ['expo', 'export', '--platform', 'web', '--clear', '--output-dir', dist], {
  cwd: 'apps/app',
  env: { ...process.env, EXPO_PUBLIC_API_BASE_URL: `http://localhost:${API_PORT}` },
});
const session = (
  await step('go', ['run', './cmd/admin', 'session', '-email', 'demo@myfantasyprem.test'], {
    cwd: 'server',
    env,
  })
).trim();

// A built binary, so stopping it stops the server (not just `go run`).
const bin = join(tmp, 'api');
await step('go', ['build', '-o', bin, './cmd/api'], { cwd: 'server' });

// The API's log, also shown here: e2e-server/signin.spec.ts reads sign-in codes from it.
const apiLog = join(tmp, 'api.log');
const logFile = createWriteStream(apiLog, { flags: 'a' });

/** Starts the API with extra env; resolves with a stop function that waits for it to exit. */
function startAPI(extra) {
  console.log(`> ${bin} (:${API_PORT})`);
  const api = launch(bin, [], {
    cwd: 'server',
    stdio: ['ignore', 'pipe', 'pipe'],
    env: {
      ...env,
      HTTP_ADDR: `:${API_PORT}`,
      CORS_ALLOWED_ORIGINS: APP_ORIGIN,
      // Invite and sign-in links open the build served on :4174.
      APP_URL: APP_ORIGIN,
      POLLER_ENABLED: 'true',
      // Sign-in emails go to the log (never in production).
      AUTH_DEV_MAGIC_LINKS: 'true',
      // A seat's claim link signs a signed-out friend in (docs/adr/0013).
      CLAIM_LINK_SIGNIN: 'true',
      // Branded images through the proxy, from the fake server only.
      BRANDED_ASSETS: 'true',
      ...imageTemplates(fpl.url),
      ...extra,
    },
  });
  for (const [stream, out] of [
    [api.stdout, process.stdout],
    [api.stderr, process.stderr],
  ]) {
    stream.on('data', (chunk) => {
      out.write(chunk);
      logFile.write(chunk);
    });
  }
  const exited = new Promise((done) => api.once('exit', done));
  return () => {
    api.kill('SIGINT');
    return exited;
  };
}

async function waitForAPI() {
  for (let i = 0; i < 120; i++) {
    try {
      const res = await fetch(`http://localhost:${API_PORT}/healthz`);
      if (res.ok) return;
    } catch {
      // not up yet
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error('the API did not start');
}

/** Waits until GET path answers 200 with the session's token. */
async function waitFor(path, session, tries = 240) {
  const { accessToken } = JSON.parse(session);
  for (let i = 0; i < tries; i++) {
    const res = await fetch(`http://localhost:${API_PORT}${path}`, {
      headers: { Authorization: `Bearer ${accessToken}` },
    }).catch(() => null);
    if (res?.ok) return;
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(`${path} never answered 200`);
}

/**
 * Runs a spec; resolves with its exit code. Asynchronous on purpose: the
 * fake FPL Draft API runs in this process, and a blocking spawnSync would
 * stop it answering while the spec runs (every upstream call timed out).
 */
function playwright(specs, extraEnv) {
  const child = launch(
    'npx',
    ['playwright', 'test', '-c', 'playwright.server.config.ts', ...[specs].flat()],
    {
      stdio: ['ignore', 'inherit', 'inherit'],
      env: {
        ...process.env,
        E2E_API: `http://localhost:${API_PORT}`,
        E2E_DIST: dist,
        PW_PORT: String(PW_PORT),
        ...extraEnv,
      },
    },
  );
  return new Promise((done) => child.once('exit', (status) => done(status ?? 1)));
}

let code = 1;
let stop = startAPI({ POLLER_SOURCE: 'demo' });
try {
  await waitForAPI();
  code = await playwright(['e2e-server/smoke.spec.ts', 'e2e-server/signin.spec.ts'], {
    E2E_SESSION: session,
    E2E_API_LOG: apiLog,
  });
} finally {
  await stop();
}

// The FPL import against a fake FPL Draft API, on a fresh database.
if (code === 0) {
  code = 1;
  await step('go', ['run', './cmd/migrate', 'down', '-to', '0'], { cwd: 'server', env });
  await step('go', ['run', './cmd/migrate', 'up'], { cwd: 'server', env });
  const importer = (
    await step(
      'go',
      [
        'run',
        './cmd/admin',
        'session',
        '-email',
        'importer@myfantasyprem.test',
        '-name',
        'Importer',
      ],
      { cwd: 'server', env },
    )
  ).trim();
  const friend = (
    await step(
      'go',
      ['run', './cmd/admin', 'session', '-email', 'friend@myfantasyprem.test', '-name', 'Friend'],
      { cwd: 'server', env },
    )
  ).trim();
  stop = startAPI({ POLLER_SOURCE: 'fpl', FPL_BASE_URL: fpl.url, JOBS_TICK: '2s' });
  try {
    await waitForAPI();
    // The poller ingests the recorded gameweeks (1-6) before an import can be verified.
    for (let gw = 1; gw <= 6; gw++) await waitFor(`/v1/gameweeks/${gw}/awards`, importer);
    code = await playwright('e2e-server/fpl-import.spec.ts', {
      E2E_FPL_SESSION: importer,
      E2E_FPL_FRIEND: friend,
    });
  } finally {
    await stop();
  }
}
process.exit(code);
