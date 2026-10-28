#!/usr/bin/env node
// A fake FPL Draft API for end-to-end tests: serves recorded JSON files
// the way provider/fpl's DirTransport reads them, so a real server with
// FPL_BASE_URL=http://localhost:<port>/api never reaches FPL
// (docs/adr/0012). GET /api/league/90001/details answers
// <dir>/league/90001/details.json; anything else is 404 like FPL's.
// It also stands in for the image hosts (BRANDED_ASSETS): GET /img/... .png
// answers a 1x1 PNG of our own, so the API's image proxy never reaches
// upstream either (imageTemplates gives the ASSETS_*_URLS settings).
//
//   node scripts/fpl-fixture-server.mjs [dir] [port]
//
// The default dir is the real (anonymised) capture in
// server/internal/provider/fpl/testdata/real-gw6.
import { Buffer } from 'node:buffer';
import { readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { join, normalize, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

export const DEFAULT_DIR = 'server/internal/provider/fpl/testdata/real-gw6';

/** A 1x1 grey PNG: the stand-in for every crest, shirt and photo. */
const PIXEL = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAAAAAA6fptVAAAACklEQVR4nGNoAAAAggCBd81ytgAAAABJRU5ErkJggg==',
  'base64',
);

/** The API's ASSETS_*_URLS settings pointing at a fixture server (its base URL ending in /api). */
export function imageTemplates(apiURL) {
  const img = apiURL.replace(/\/api$/, '/img');
  return {
    ASSETS_PLAYER_PHOTO_URLS: `${img}/photos/p{code}.png`,
    ASSETS_CLUB_CREST_URLS: `${img}/badges/t{code}.png`,
    ASSETS_CLUB_SHIRT_URLS: `${img}/shirts/shirt_{code}.png`,
    ASSETS_CLUB_GK_SHIRT_URLS: `${img}/shirts/shirt_{code}_1.png`,
  };
}

/** Starts the server; resolves with its base URL (ending in /api) and a close function. */
export function startFixtureServer(dir = DEFAULT_DIR, port = 0) {
  const root = resolve(dir);
  const server = createServer(async (req, res) => {
    const send = (status, body) => {
      res.writeHead(status, { 'Content-Type': 'application/json' });
      res.end(body);
    };
    if (req.method !== 'GET') return send(405, '{"detail":"Method not allowed."}');
    let path = decodeURIComponent(new URL(req.url ?? '/', 'http://x').pathname);
    if (/^\/img\/[a-z]+\/[a-z_]*[0-9]+(_1)?\.png$/.test(path)) {
      res.writeHead(200, { 'Content-Type': 'image/png' });
      return res.end(PIXEL);
    }
    if (path.startsWith('/api/')) path = path.slice('/api'.length);
    const file = normalize(join(root, `${path.replace(/\/+$/, '')}.json`));
    if (!file.startsWith(root + sep)) return send(404, '{"detail":"Not found."}');
    try {
      send(200, await readFile(file));
    } catch {
      send(404, '{"detail":"Not found."}');
    }
  });
  return new Promise((ok, fail) => {
    server.once('error', fail);
    server.listen(port, '127.0.0.1', () => {
      const { port: bound } = server.address();
      ok({
        url: `http://127.0.0.1:${bound}/api`,
        close: () => new Promise((done) => server.close(() => done())),
      });
    });
  });
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const [dir = DEFAULT_DIR, port = '0'] = process.argv.slice(2);
  const { url } = await startFixtureServer(dir, Number(port));
  console.log(`fake FPL Draft API on ${url} (from ${dir})`);
}
