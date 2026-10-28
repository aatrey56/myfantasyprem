// Runs every Go test, including the Postgres integration suites, against a
// throwaway Postgres: its own postgres:17 container (data in tmpfs) on a
// Docker-chosen free host port, removed afterwards even on failure or
// SIGINT/SIGTERM (scripts/test-postgres.mjs). Safe to run concurrently from
// several worktrees.
// Usage: npm run test:go:db. Needs Docker. With TEST_DATABASE_URL already
// set (CI's service container), it uses that server instead.
import { spawn } from 'node:child_process';

import { onShutdown, startTestPostgres } from './test-postgres.mjs';

let child = null;
let db = null;

function fail(msg, code = 1) {
  console.error(`go-db-tests: ${msg}`);
  db?.remove();
  process.exit(code);
}

let url = process.env.TEST_DATABASE_URL;
if (!url) {
  try {
    db = await startTestPostgres({ prefix: 'mfp-go-db-test' });
  } catch (e) {
    fail(e.message);
  }
  url = db.url;
}
// Registered after the database's cleanup, so it runs first: go stops, then the container goes.
onShutdown(() => child?.kill('SIGTERM'));

// httpapi alone can take ~350s under load; go test's default is 10m.
const args = ['test', '-count=1', '-timeout', '20m', './...'];
console.log(`> go ${args.join(' ')}`);
child = spawn('go', args, {
  cwd: 'server',
  stdio: 'inherit',
  env: { ...process.env, TEST_DATABASE_URL: url },
});
child.on('error', (e) => fail(`go: ${e.message}`));
child.on('exit', (status, signal) => {
  db?.remove();
  if (signal) {
    // go test printed nothing about it: its output just stops after the
    // last package it reported. Usually another process killed it.
    console.error(
      `go-db-tests: go test was killed by ${signal} before it finished, so the ` +
        'packages after the last one listed did not report. Something outside the tests ' +
        'stopped it, for example `pkill -f "go test"` from another worktree or the OS ' +
        'under memory pressure. Rerun.',
    );
    process.exit(1);
  }
  process.exit(status ?? 0);
});
