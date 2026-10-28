// Tests for go-db-tests.mjs: run it with a fake `go` on PATH, so no Go,
// Docker or Postgres is needed. Run with `npm run test:scripts`.
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { after, test } from 'node:test';

const bin = mkdtempSync(join(tmpdir(), 'go-db-tests-'));
after(() => rmSync(bin, { recursive: true, force: true }));

// run executes the script with `go` replaced by a shell script body.
function run(body) {
  writeFileSync(join(bin, 'go'), `#!/bin/sh\n${body}\n`);
  chmodSync(join(bin, 'go'), 0o755);
  return spawnSync(process.execPath, ['scripts/go-db-tests.mjs'], {
    encoding: 'utf8',
    env: {
      ...process.env,
      PATH: `${bin}:${process.env.PATH}`,
      TEST_DATABASE_URL: 'postgres://fake',
    },
  });
}

test('passes go test exit code through', () => {
  const res = run('echo "ok  	pkg/draft	1.0s"; exit 3');
  assert.equal(res.status, 3);
  assert.doesNotMatch(res.stderr, /signal/);
});

test('says so when go test is killed by a signal', () => {
  // Like `pkill -f "go test"` from another shell: the output just stops
  // after the last package go printed, with no FAIL line.
  for (const sig of ['KILL', 'TERM']) {
    const res = run(`echo "ok  	pkg/draft	1.0s"; kill -${sig} $$`);
    assert.equal(res.status, 1, sig);
    assert.match(res.stdout, /pkg\/draft/);
    assert.match(res.stderr, new RegExp(`go test was killed by SIG${sig}`), sig);
  }
});

test('starts its own container for go test and removes it afterwards', () => {
  // Fake docker + go: go prints the URL it was given. No TEST_DATABASE_URL set.
  const log = join(bin, 'docker.log');
  writeFileSync(
    join(bin, 'docker'),
    `#!/bin/sh
echo "$*" >> "${log}"
[ "$1" = port ] && echo "127.0.0.1:49500"
exit 0
`,
  );
  chmodSync(join(bin, 'docker'), 0o755);
  writeFileSync(join(bin, 'go'), '#!/bin/sh\necho "url=$TEST_DATABASE_URL"\nexit 4\n');
  chmodSync(join(bin, 'go'), 0o755);
  const env = { ...process.env, PATH: `${bin}:${process.env.PATH}` };
  delete env.TEST_DATABASE_URL;
  const res = spawnSync(process.execPath, ['scripts/go-db-tests.mjs'], { encoding: 'utf8', env });
  assert.equal(res.status, 4);
  assert.match(res.stdout, /url=postgres:\/\/\S+@127\.0\.0\.1:49500\//);
  const calls = readFileSync(log, 'utf8').trim().split('\n');
  assert.match(calls[0], /^run -d --name mfp-go-db-test-/);
  assert.match(calls.at(-1), /^rm -f -v mfp-go-db-test-/);
});
