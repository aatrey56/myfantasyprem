// Local stack for running the app against the real server:
//   1. Postgres from docker-compose (service db), waiting until healthy;
//   2. migrations and the demo seed (server/cmd/seed);
//   3. the API on :8080 with dev magic links and the poller on the demo
//      source (it never calls FPL).
// Then run `npm run dev:web` in another terminal and sign in as
// demo@myfantasyprem.test (the magic link is printed in this terminal).
// Usage: npm run dev:stack. Ctrl-C stops the API; `docker compose stop db`
// stops Postgres. Override DATABASE_URL to use another database.
import { spawn, spawnSync } from 'node:child_process';

const DATABASE_URL =
  process.env.DATABASE_URL ??
  'postgres://myfantasyprem:myfantasyprem@localhost:5432/myfantasyprem?sslmode=disable';

function step(cmd, args, opts = {}) {
  console.log(`> ${cmd} ${args.join(' ')}`);
  const res = spawnSync(cmd, args, { stdio: 'inherit', ...opts });
  if (res.error) {
    console.error(`dev-stack: ${cmd}: ${res.error.message}`);
    process.exit(1);
  }
  if (res.status !== 0) process.exit(res.status ?? 1);
}

const env = { ...process.env, DATABASE_URL };

if (!process.env.DATABASE_URL) {
  step('docker', ['compose', 'up', '-d', '--wait', 'db']);
}
step('go', ['run', './cmd/seed'], { cwd: 'server', env });

console.log('> go run ./cmd/api (poller on the demo source, dev magic links)');
const api = spawn('go', ['run', './cmd/api'], {
  cwd: 'server',
  stdio: 'inherit',
  env: {
    ...env,
    AUTH_DEV_MAGIC_LINKS: 'true',
    POLLER_ENABLED: 'true',
    POLLER_SOURCE: 'demo',
    MIGRATE_ON_START: 'true',
    METRICS_ADDR: process.env.METRICS_ADDR ?? '127.0.0.1:9090',
  },
});
for (const sig of ['SIGINT', 'SIGTERM']) process.on(sig, () => api.kill(sig));
api.on('exit', (code) => process.exit(code ?? 0));
