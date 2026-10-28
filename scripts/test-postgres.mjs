// A private, throwaway Postgres for test scripts (go-db-tests.mjs,
// e2e-server.mjs): its own postgres:17 container with a unique name, data in
// tmpfs, on a Docker-chosen free port bound to 127.0.0.1, so several
// worktrees can run at once. Needs Docker on PATH.
//
//   const db = await startTestPostgres({ prefix: 'mfp-go-db-test' });
//   db.url        postgres://... for the container
//   db.remove()   removes the container now (also done at exit, on failure,
//                 and on SIGINT/SIGTERM/SIGHUP)
//
// onShutdown(fn) registers extra cleanup (stop child processes, ...) that
// runs before the container is removed when a signal or exit arrives.
import { spawnSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';

export const PG_USER = 'myfantasyprem';
const READY_TIMEOUT_MS = 90_000;
const SIGNALS = { SIGHUP: 1, SIGINT: 2, SIGTERM: 15 };

function docker(args, opts = {}) {
  return spawnSync('docker', args, { encoding: 'utf8', ...opts });
}

const hooks = [];
let installed = false;
let ran = false;

function runHooks() {
  if (ran) return;
  ran = true;
  for (const fn of hooks.splice(0).reverse()) {
    try {
      fn();
    } catch (e) {
      console.error(`test-postgres: cleanup: ${e.message}`);
    }
  }
}

/** Runs fn once when the process exits or gets SIGINT/SIGTERM/SIGHUP (latest registered first). */
export function onShutdown(fn) {
  hooks.push(fn);
  if (installed) return;
  installed = true;
  for (const [sig, n] of Object.entries(SIGNALS)) {
    process.on(sig, () => {
      runHooks();
      process.exit(128 + n);
    });
  }
  process.on('exit', runHooks);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Starts the container and waits until Postgres accepts TCP connections.
 * Throws (after removing the container) when it cannot.
 * readyTimeoutMs and pollMs are for tests.
 */
export async function startTestPostgres({
  prefix,
  readyTimeoutMs = READY_TIMEOUT_MS,
  pollMs = 500,
}) {
  const container = `${prefix}-${process.pid}-${randomBytes(3).toString('hex')}`;
  let removed = false;
  const remove = () => {
    if (removed) return;
    removed = true;
    docker(['rm', '-f', '-v', container], { stdio: 'ignore' });
    console.log(`> removed ${container}`);
  };
  // Registered before `docker run`: a signal that lands mid-start still cleans up.
  onShutdown(remove);

  try {
    console.log(`> docker run ${container} (postgres:17, tmpfs)`);
    const run = docker([
      'run',
      '-d',
      '--name',
      container,
      '-e',
      `POSTGRES_USER=${PG_USER}`,
      '-e',
      `POSTGRES_PASSWORD=${PG_USER}`,
      '-e',
      `POSTGRES_DB=${PG_USER}`,
      '-p',
      '127.0.0.1::5432',
      '--tmpfs',
      '/var/lib/postgresql/data',
      'postgres:17',
      'postgres',
      '-c',
      'fsync=off',
      '-c',
      'full_page_writes=off',
      '-c',
      'synchronous_commit=off',
    ]);
    if (run.error || run.status !== 0) {
      throw new Error(`docker run failed: ${run.error?.message ?? run.stderr.trim()}`);
    }

    // -h 127.0.0.1: the init-time server only listens on the unix socket, so a
    // TCP check means the real server is up.
    const deadline = Date.now() + readyTimeoutMs;
    for (;;) {
      const r = docker(['exec', container, 'pg_isready', '-h', '127.0.0.1', '-U', PG_USER]);
      if (r.status === 0) break;
      if (Date.now() > deadline) {
        throw new Error(`postgres not ready after ${readyTimeoutMs / 1000}s`);
      }
      await sleep(pollMs);
    }

    const port = docker(['port', container, '5432/tcp']);
    const m = /127\.0\.0\.1:(\d+)/.exec(port.stdout ?? '');
    if (!m) throw new Error(`cannot read host port: ${port.stdout} ${port.stderr}`);
    console.log(`> postgres ready on 127.0.0.1:${m[1]}`);
    return {
      container,
      remove,
      url: `postgres://${PG_USER}:${PG_USER}@127.0.0.1:${m[1]}/${PG_USER}?sslmode=disable`,
    };
  } catch (e) {
    remove();
    throw e;
  }
}
