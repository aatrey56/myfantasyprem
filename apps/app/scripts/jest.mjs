// Runs Jest, adding --maxWorkers from JEST_WORKERS when it is set (CI runs
// on a 2-core runner, where Jest's default goes in band and hangs on timers
// some libraries leave behind). Plain `jest` otherwise. A Node wrapper, not
// shell syntax, so `npm test` works the same on Windows.
import { spawnSync } from 'node:child_process';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const jest = require.resolve('jest/bin/jest');
const args = process.argv.slice(2);
const workers = process.env.JEST_WORKERS;
if (workers && !args.some((a) => a.startsWith('--maxWorkers'))) {
  args.unshift(`--maxWorkers=${workers}`);
}
const run = spawnSync(process.execPath, [jest, ...args], { stdio: 'inherit' });
process.exit(run.status ?? 1);
