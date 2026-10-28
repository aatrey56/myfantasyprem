// Tests for fpl-fixture-server.mjs on a temporary directory. Run with
// `npm run test:scripts`.
import assert from 'node:assert/strict';
import { Buffer } from 'node:buffer';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { after, test } from 'node:test';

import { imageTemplates, startFixtureServer } from './fpl-fixture-server.mjs';

const dir = mkdtempSync(join(tmpdir(), 'fpl-fixtures-'));
mkdirSync(join(dir, 'league', '7'), { recursive: true });
mkdirSync(join(dir, 'entry', '101', 'event'), { recursive: true });
writeFileSync(join(dir, 'league', '7', 'details.json'), '{"league":{"id":7}}');
writeFileSync(join(dir, 'entry', '101', 'event', '7.json'), '"No pick history"');
writeFileSync(join(dir, 'secret.txt.json'), '{}');
const server = await startFixtureServer(dir);
after(async () => {
  await server.close();
  rmSync(dir, { recursive: true, force: true });
});

test('serves a recorded file at its API path', async () => {
  const res = await fetch(`${server.url}/league/7/details`);
  assert.equal(res.status, 200);
  assert.equal(res.headers.get('content-type'), 'application/json');
  assert.deepEqual(await res.json(), { league: { id: 7 } });
});

test('serves a JSON string body as recorded', async () => {
  const res = await fetch(`${server.url}/entry/101/event/7`);
  assert.equal(await res.json(), 'No pick history');
});

test('answers 404 for anything not recorded, and outside the directory', async () => {
  assert.equal((await fetch(`${server.url}/league/8/details`)).status, 404);
  assert.equal((await fetch(`${server.url}/..%2F..%2Fetc%2Fpasswd`)).status, 404);
});

test('refuses writes', async () => {
  assert.equal((await fetch(`${server.url}/league/7/details`, { method: 'POST' })).status, 405);
});

test('stands in for the image hosts with a PNG of its own', async () => {
  const urls = imageTemplates(server.url);
  for (const template of Object.values(urls)) {
    const res = await fetch(template.replace('{code}', '3'));
    assert.equal(res.status, 200, template);
    assert.equal(res.headers.get('content-type'), 'image/png');
    const body = Buffer.from(await res.arrayBuffer());
    assert.equal(body.subarray(1, 4).toString(), 'PNG');
  }
  assert.equal((await fetch(urls.ASSETS_CLUB_CREST_URLS.replace('{code}', '../x'))).status, 404);
});
