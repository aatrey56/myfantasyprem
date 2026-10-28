/**
 * The sign-in code from a development API's log (AUTH_DEV_MAGIC_LINKS=true
 * logs a "dev magic link" line with the code), served over HTTP so a Maestro
 * flow can type it: `npm run test:ios -- --api` starts it. Local only.
 */
import { readFileSync } from 'node:fs';
import { createServer } from 'node:http';

/** The code in the last "dev magic link" line of log, or null. */
export function latestCode(log) {
  const lines = log.split('\n').filter((l) => l.includes('dev magic link'));
  for (let i = lines.length - 1; i >= 0; i -= 1) {
    const code = lines[i].match(/\bcode="?(\d{6})\b/)?.[1];
    if (code) return code;
  }
  return null;
}

/**
 * Serves GET /code: the latest code in the file at logPath (text/plain), or
 * 404 while there is none. Listens on 127.0.0.1, on a free port unless one
 * is given. Resolves to { url, close }.
 */
export function serveCode(logPath, port = 0) {
  const server = createServer((req, res) => {
    let code = null;
    try {
      code = req.url === '/code' ? latestCode(readFileSync(logPath, 'utf8')) : null;
    } catch {
      code = null;
    }
    res.writeHead(code ? 200 : 404, { 'Content-Type': 'text/plain' });
    res.end(code ?? 'no code');
  });
  return new Promise((resolve) => {
    server.listen(port, '127.0.0.1', () => {
      resolve({
        url: `http://127.0.0.1:${server.address().port}/code`,
        close: () => new Promise((done) => server.close(done)),
      });
    });
  });
}
