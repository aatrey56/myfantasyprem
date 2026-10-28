// Tests for test-postgres.mjs with a fake `docker` on PATH (it logs every call
// to a file), so no Docker is needed. Run with `npm run test:scripts`.
import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { after, test } from 'node:test';

const dir = mkdtempSync(join(tmpdir(), 'test-postgres-'));
after(() => rmSync(dir, { recursive: true, force: true }));

const mod = new URL('./test-postgres.mjs', import.meta.url).href;
let n = 0;

// Fake docker. FAKE_READY_AFTER = number of failing pg_isready calls first;
// FAKE_RUN_FAILS=1 makes `docker run` fail. Calls are appended to $LOG.
function fakeDocker() {
  const log = join(dir, `docker-${++n}.log`);
  const bin = join(dir, `bin-${n}`);
  spawnSync('mkdir', ['-p', bin]);
  writeFileSync(
    join(bin, 'docker'),
    `#!/bin/sh
echo "$*" >> "$LOG"
case "$1" in
  run) [ -n "$FAKE_RUN_FAILS" ] && { echo "no such image" >&2; exit 125; }; echo cid ;;
  exec)
    c=$(cat "$LOG.ready" 2>/dev/null || echo 0)
    echo $((c + 1)) > "$LOG.ready"
    [ "$c" -ge "\${FAKE_READY_AFTER:-0}" ] ;;
  port) echo "127.0.0.1:49321" ;;
  rm) ;;
esac
`,
  );
  chmodSync(join(bin, 'docker'), 0o755);
  return { log, bin };
}

const calls = (log) => {
  try {
    return readFileSync(log, 'utf8').trim().split('\n').filter(Boolean);
  } catch {
    return [];
  }
};

/** Runs `body` (an ES module snippet) in a child node with the fake docker first on PATH. */
function runScript(fake, body, env = {}) {
  return spawn(
    process.execPath,
    ['--input-type=module', '-e', `import * as pg from '${mod}';\n${body}`],
    {
      env: { ...process.env, PATH: `${fake.bin}:${process.env.PATH}`, LOG: fake.log, ...env },
      stdio: ['ignore', 'pipe', 'pipe'],
    },
  );
}

function finish(child) {
  return new Promise((resolve) => {
    let out = '';
    let err = '';
    child.stdout.on('data', (d) => (out += d));
    child.stderr.on('data', (d) => (err += d));
    child.on('exit', (code, signal) => resolve({ code, signal, out, err }));
  });
}

test('starts a private container on a free loopback port and removes it at exit', async () => {
  const fake = fakeDocker();
  const res = await finish(
    runScript(
      fake,
      `const db = await pg.startTestPostgres({ prefix: 'mfp-x', pollMs: 5 });
       console.log(JSON.stringify(db));`,
      { FAKE_READY_AFTER: '2' },
    ),
  );
  assert.equal(res.code, 0, res.err);
  const db = JSON.parse(res.out.split('\n').find((l) => l.startsWith('{')));
  assert.match(db.container, /^mfp-x-\d+-[0-9a-f]{6}$/);
  assert.equal(
    db.url,
    'postgres://myfantasyprem:myfantasyprem@127.0.0.1:49321/myfantasyprem?sslmode=disable',
  );

  const c = calls(fake.log);
  const run = c[0];
  assert.match(run, new RegExp(`^run -d --name ${db.container} `));
  assert.match(run, / -p 127\.0\.0\.1::5432 /, 'free host port chosen by Docker, loopback only');
  assert.match(run, / --tmpfs \/var\/lib\/postgresql\/data /);
  assert.match(run, / postgres:17 /);
  assert.doesNotMatch(run, /5433/);
  // Ready is checked over TCP, retried until it passes (2 failures, then ok).
  const ready = c.filter((l) => l.startsWith('exec'));
  assert.equal(ready.length, 3);
  for (const l of ready) assert.match(l, /pg_isready -h 127\.0\.0\.1 /);
  assert.deepEqual(c.slice(-1), [`rm -f -v ${db.container}`]);
  assert.equal(c.filter((l) => l.startsWith('rm')).length, 1);
});

test('two containers get different names', async () => {
  const fake = fakeDocker();
  const res = await finish(
    runScript(
      fake,
      `const a = await pg.startTestPostgres({ prefix: 'p', pollMs: 5 });
       const b = await pg.startTestPostgres({ prefix: 'p', pollMs: 5 });
       console.log(a.container !== b.container);
       a.remove(); a.remove();`,
    ),
  );
  assert.equal(res.code, 0, res.err);
  assert.match(res.out, /true/);
  // remove() is idempotent; the exit hook removes only what is left.
  assert.equal(calls(fake.log).filter((l) => l.startsWith('rm')).length, 2);
});

test('removes the container when docker run fails, and when Postgres never gets ready', async () => {
  let fake = fakeDocker();
  let res = await finish(
    runScript(
      fake,
      `await pg.startTestPostgres({ prefix: 'f', pollMs: 5 }).catch((e) => console.log('ERR ' + e.message));`,
      { FAKE_RUN_FAILS: '1' },
    ),
  );
  assert.match(res.out, /ERR docker run failed: no such image/);
  assert.equal(calls(fake.log).filter((l) => l.startsWith('rm -f -v f-')).length, 1);

  fake = fakeDocker();
  res = await finish(
    runScript(
      fake,
      `await pg.startTestPostgres({ prefix: 'f', pollMs: 5, readyTimeoutMs: 50 }).catch((e) => console.log('ERR ' + e.message));`,
      { FAKE_READY_AFTER: '100000' },
    ),
  );
  assert.match(res.out, /ERR postgres not ready/);
  assert.equal(calls(fake.log).filter((l) => l.startsWith('rm -f -v f-')).length, 1);
});

test('removes the container when the process fails after starting it', async () => {
  const fake = fakeDocker();
  const res = await finish(
    runScript(
      fake,
      `await pg.startTestPostgres({ prefix: 'x', pollMs: 5 });
       process.exit(7);`,
    ),
  );
  assert.equal(res.code, 7);
  assert.equal(calls(fake.log).filter((l) => l.startsWith('rm -f -v x-')).length, 1);
});

for (const [sig, code] of [
  ['SIGINT', 130],
  ['SIGTERM', 143],
  ['SIGHUP', 129],
]) {
  test(`${sig} runs the shutdown hooks, then removes the container`, async () => {
    const fake = fakeDocker();
    const child = runScript(
      fake,
      `pg.onShutdown(() => console.log('hook ran'));
       await pg.startTestPostgres({ prefix: 's', pollMs: 5 });
       console.log('ready');
       setInterval(() => {}, 1000);`,
    );
    const done = finish(child);
    await new Promise((resolve) => {
      child.stdout.on('data', (d) => /ready/.test(String(d)) && resolve());
    });
    child.kill(sig);
    const res = await done;
    assert.equal(res.code, code);
    assert.match(res.out, /hook ran/);
    assert.equal(calls(fake.log).filter((l) => l.startsWith('rm -f -v s-')).length, 1);
  });
}
