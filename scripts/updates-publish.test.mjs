import assert from 'node:assert/strict';
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';

import { pruneUpdates, writeUpdate } from './updates-publish.mjs';

/** A minimal `expo export --platform ios` output. */
function fakeExport() {
  const dir = mkdtempSync(join(tmpdir(), 'mfp-export-'));
  mkdirSync(join(dir, '_expo/static/js/ios'), { recursive: true });
  mkdirSync(join(dir, 'assets'));
  writeFileSync(join(dir, '_expo/static/js/ios/entry-abc.hbc'), 'bundle');
  writeFileSync(join(dir, 'assets/0f1e2d'), 'png');
  writeFileSync(
    join(dir, 'metadata.json'),
    JSON.stringify({
      version: 0,
      bundler: 'metro',
      fileMetadata: {
        ios: {
          bundle: '_expo/static/js/ios/entry-abc.hbc',
          assets: [{ path: 'assets/0f1e2d', ext: 'png' }],
        },
      },
    }),
  );
  return dir;
}

test('writeUpdate lays out the update the Go server reads', () => {
  const dir = mkdtempSync(join(tmpdir(), 'mfp-updates-'));
  const id = '0b5c3d2e-1f4a-4b6c-8d7e-9fa0b1c2d3e4';
  const out = writeUpdate({
    dir,
    exportDir: fakeExport(),
    runtimeVersion: 'c7cb618a',
    expoConfig: { name: 'MyFantasyPrem' },
    id,
    now: new Date('2026-10-17T09:00:00Z'),
  });
  assert.equal(out, join(dir, 'c7cb618a', id));
  assert.deepEqual(JSON.parse(readFileSync(join(out, 'update.json'), 'utf8')), {
    id,
    createdAt: '2026-10-17T09:00:00.000Z',
    runtimeVersion: 'c7cb618a',
  });
  assert.deepEqual(JSON.parse(readFileSync(join(out, 'expoConfig.json'), 'utf8')), {
    name: 'MyFantasyPrem',
  });
  assert.equal(readFileSync(join(out, '_expo/static/js/ios/entry-abc.hbc'), 'utf8'), 'bundle');
  assert.equal(readFileSync(join(out, 'assets/0f1e2d'), 'utf8'), 'png');
  assert.ok(existsSync(join(out, 'metadata.json')));
  // No temporary folder left behind.
  assert.deepEqual(readdirSync(join(dir, 'c7cb618a')), [id]);
});

test('writeUpdate refuses unsafe runtime versions and exports without an iOS bundle', () => {
  const dir = mkdtempSync(join(tmpdir(), 'mfp-updates-'));
  for (const runtimeVersion of ['../x', '..', '', 'a/b']) {
    assert.throws(() =>
      writeUpdate({ dir, exportDir: fakeExport(), runtimeVersion, expoConfig: {} }),
    );
  }
  const web = fakeExport();
  writeFileSync(
    join(web, 'metadata.json'),
    JSON.stringify({ fileMetadata: { web: { bundle: 'x' } } }),
  );
  assert.throws(
    () => writeUpdate({ dir, exportDir: web, runtimeVersion: 'r1', expoConfig: {} }),
    /no iOS bundle/,
  );
});

test('pruneUpdates keeps the newest N of a runtime version only', () => {
  const dir = mkdtempSync(join(tmpdir(), 'mfp-updates-'));
  const exportDir = fakeExport();
  const ids = [1, 2, 3, 4].map((n) => {
    const id = `0000000${n}-0000-4000-8000-000000000000`;
    writeUpdate({
      dir,
      exportDir,
      runtimeVersion: 'r1',
      expoConfig: {},
      id,
      now: new Date(Date.UTC(2026, 9, n)),
    });
    return id;
  });
  const other = '00000009-0000-4000-8000-000000000000';
  writeUpdate({
    dir,
    exportDir,
    runtimeVersion: 'r0',
    expoConfig: {},
    id: other,
    now: new Date(Date.UTC(2026, 0, 1)),
  });

  assert.deepEqual(pruneUpdates(dir, 'r1', 2).sort(), [ids[0], ids[1]]);
  assert.deepEqual(readdirSync(join(dir, 'r1')).sort(), [ids[2], ids[3]]);
  assert.deepEqual(readdirSync(join(dir, 'r0')), [other]);
  assert.deepEqual(pruneUpdates(dir, 'missing', 2), []);
});
