import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';

import { latestCode, serveCode } from './dev-code.mjs';

const line = (code) =>
  `time=2026-10-14T23:43:00Z level=INFO msg="dev magic link: open the link, POST /v1/auth/email/verify with the token or type the code" link=http://localhost:8081/sign-in?token=abc token=abc code=${code} expires_at=2026-10-14T23:58:00Z`;

test('latestCode takes the last dev magic link line', () => {
  const log = [line('111111'), 'level=INFO msg=request path=/v1/me', line('222222'), ''].join('\n');
  assert.equal(latestCode(log), '222222');
});

test('latestCode is null without a dev magic link', () => {
  assert.equal(latestCode('level=INFO msg=request code=123456\n'), null);
});

test('serveCode serves the latest code and 404s before there is one', async () => {
  const log = join(mkdtempSync(join(tmpdir(), 'mfp-dev-code-')), 'api.log');
  writeFileSync(log, 'level=INFO msg=listening\n');
  const { url, close } = await serveCode(log);
  try {
    assert.equal((await fetch(url)).status, 404);
    writeFileSync(log, `${line('333333')}\n${line('444444')}\n`);
    const res = await fetch(url);
    assert.equal(res.status, 200);
    assert.equal(await res.text(), '444444');
    assert.equal((await fetch(url.replace('/code', '/other'))).status, 404);
  } finally {
    await close();
  }
});
