/**
 * Publishes an over-the-air update of the iOS app (docs/IOS.md,
 * "Over-the-air updates"):
 *
 *   npm run updates:publish -- --dir ~/mfp-tailnet/updates [--keep 5]
 *
 * Exports the JavaScript bundle (`expo export --platform ios`, with
 * EXPO_PUBLIC_API_BASE_URL from the environment) and writes it to
 * <dir>/<runtimeVersion>/<updateId>/ with update.json and the public app
 * config, which the Go API serves from UPDATES_DIR (server/internal/updates).
 * The runtime version is resolved from the same app config as the build
 * (apps/app/.env* included), so the update only reaches binaries built
 * from the same native project. Keeps the newest --keep updates per
 * runtime version. `--from <export dir>` skips the export (tests).
 */
import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import {
  cpSync,
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  renameSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { parseArgs } from 'node:util';

const appDir = resolve(import.meta.dirname, '../apps/app');
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const RUNTIME = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

/**
 * Copies an `expo export` output (exportDir, with metadata.json) into
 * <dir>/<runtimeVersion>/<id>/ and writes update.json and expoConfig.json.
 * The folder appears in one rename, so the server never sees half an
 * update. Returns the update's path.
 */
export function writeUpdate({
  dir,
  exportDir,
  runtimeVersion,
  expoConfig,
  id = randomUUID(),
  now = new Date(),
}) {
  if (!RUNTIME.test(runtimeVersion) || runtimeVersion.includes('..')) {
    throw new Error(`invalid runtime version ${JSON.stringify(runtimeVersion)}`);
  }
  if (!UUID.test(id)) throw new Error(`invalid update id ${id}`);
  const meta = JSON.parse(readFileSync(join(exportDir, 'metadata.json'), 'utf8'));
  if (!meta.fileMetadata?.ios?.bundle)
    throw new Error('the export has no iOS bundle (metadata.json)');
  const runtimeDir = join(dir, runtimeVersion);
  const tmp = join(runtimeDir, `.tmp-${id}`);
  const out = join(runtimeDir, id);
  mkdirSync(runtimeDir, { recursive: true });
  cpSync(exportDir, tmp, { recursive: true });
  writeFileSync(join(tmp, 'expoConfig.json'), JSON.stringify(expoConfig));
  writeFileSync(
    join(tmp, 'update.json'),
    JSON.stringify({ id, createdAt: now.toISOString(), runtimeVersion }, null, 2),
  );
  renameSync(tmp, out);
  return out;
}

/** Removes all but the newest keep updates of a runtime version; returns the removed ids. */
export function pruneUpdates(dir, runtimeVersion, keep) {
  const runtimeDir = join(dir, runtimeVersion);
  if (!existsSync(runtimeDir)) return [];
  const updates = readdirSync(runtimeDir)
    .filter((name) => UUID.test(name))
    .map((id) => {
      try {
        const u = JSON.parse(readFileSync(join(runtimeDir, id, 'update.json'), 'utf8'));
        return { id, at: Date.parse(u.createdAt) || 0 };
      } catch {
        return { id, at: 0 };
      }
    })
    .sort((a, b) => b.at - a.at);
  const removed = updates.slice(Math.max(keep, 1)).map((u) => u.id);
  for (const id of removed) rmSync(join(runtimeDir, id), { recursive: true, force: true });
  return removed;
}

function npx(args, env) {
  return execFileSync('npx', args, {
    cwd: appDir,
    env: { ...process.env, FORCE_COLOR: '0', ...env },
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'inherit'],
    maxBuffer: 64 * 1024 * 1024,
  });
}

/** The runtime version the iOS build of this app config has. */
function resolveRuntimeVersion() {
  const out = JSON.parse(npx(['expo-updates', 'runtimeversion:resolve', '--platform', 'ios']));
  if (!out.runtimeVersion) {
    throw new Error(
      'No runtime version: updates are off in this app config. Set APP_UPDATES_URL (and the other ' +
        'prebuild variables) as for the build, in the environment or apps/app/.env.local.',
    );
  }
  return out.runtimeVersion;
}

function main() {
  const { values } = parseArgs({
    options: {
      dir: { type: 'string' },
      keep: { type: 'string', default: '5' },
      from: { type: 'string' },
    },
  });
  if (!values.dir)
    throw new Error('usage: npm run updates:publish -- --dir <updates dir> [--keep N]');
  const keep = Number.parseInt(values.keep, 10);
  if (!(keep >= 1)) throw new Error('--keep must be a whole number of at least 1');
  if (
    !values.from &&
    !process.env.EXPO_PUBLIC_API_BASE_URL &&
    process.env.EXPO_PUBLIC_USE_MOCKS !== 'true'
  ) {
    // apps/app/.env.local may still set it; expo export reads that too.
    const local = join(appDir, '.env.local');
    if (!existsSync(local) || !/^EXPO_PUBLIC_API_BASE_URL=/m.test(readFileSync(local, 'utf8'))) {
      throw new Error('Set EXPO_PUBLIC_API_BASE_URL (the API the phone talks to) for the bundle.');
    }
  }
  const dir = resolve(values.dir);
  const runtimeVersion = resolveRuntimeVersion();
  const expoConfig = JSON.parse(npx(['expo', 'config', '--type', 'public', '--json']));
  let exportDir = values.from && resolve(values.from);
  let cleanup = null;
  if (!exportDir) {
    exportDir = join(tmpdir(), `mfp-update-${randomUUID()}`);
    cleanup = exportDir;
    // NODE_ENV=production: the same .env files and minification as a Release build.
    npx(['expo', 'export', '--platform', 'ios', '--clear', '--output-dir', exportDir], {
      NODE_ENV: 'production',
    });
  }
  try {
    const out = writeUpdate({ dir, exportDir, runtimeVersion, expoConfig });
    const removed = pruneUpdates(dir, runtimeVersion, keep);
    console.log(`Published ${out}`);
    console.log(
      `runtime version ${runtimeVersion} (Account > App version shows the phone's first 8 characters)`,
    );
    if (removed.length > 0) console.log(`Removed ${removed.length} older update(s).`);
  } finally {
    if (cleanup) rmSync(cleanup, { recursive: true, force: true });
  }
}

if (process.argv[1] && resolve(process.argv[1]) === resolve(import.meta.filename)) {
  try {
    main();
  } catch (err) {
    console.error(err.message);
    process.exit(1);
  }
}
