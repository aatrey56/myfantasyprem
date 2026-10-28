// Copies MSW's service worker into a directory, or removes it.
//   node copy-msw-worker.mjs <dir>           copy (mock builds and the mock dev server)
//   node copy-msw-worker.mjs --remove <dir>  delete (before a production build)
// The worker is never committed: it comes from the installed msw package, so
// it always matches the msw version.
import { copyFileSync, existsSync, rmSync } from 'node:fs';
import { createRequire } from 'node:module';
import { join, resolve } from 'node:path';

const args = process.argv.slice(2);
const remove = args[0] === '--remove';
const dir = resolve(args[remove ? 1 : 0] ?? '.');
const target = join(dir, 'mockServiceWorker.js');

if (remove) {
  if (existsSync(target)) rmSync(target);
} else {
  const source = createRequire(import.meta.url).resolve('msw/mockServiceWorker.js');
  copyFileSync(source, target);
  console.log(`Copied the MSW worker to ${target}`);
}
