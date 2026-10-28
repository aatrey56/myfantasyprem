#!/usr/bin/env node
/**
 * Native smoke tests: the Maestro flows in e2e/ios/flows against the mock
 * build on an iOS Simulator. Not part of .office/checks (needs macOS, Xcode,
 * a booted simulator and Maestro). See docs/IOS.md.
 *
 *   npm run test:ios                    # run every flow on the booted simulator
 *   npm run test:ios -- --build         # first build + install the Release mock app
 *   npm run test:ios -- --screenshots   # also copy the ios-*.png captures to docs/screenshots/ios
 *   npm run test:ios -- tour            # only e2e/ios/flows/tour.yaml
 *   npm run test:ios -- --api --build   # e2e/ios/api-flows against a local API (npm run dev:stack)
 *
 * Env: IOS_DEVICE (simulator UDID; default: the booted one), APP_BUNDLE_ID
 * (default com.myfantasyprem.app.dev), APP_SCHEME (default myfantasyprem),
 * MAESTRO_BIN (default: `maestro` on PATH, then ~/.maestro/bin/maestro).
 * With --api: API_URL (default http://localhost:8080; baked into the build)
 * and API_LOG, a file holding the API's output, where the flows read the
 * sign-in code (`npm run dev:stack > api.log 2>&1`).
 */
import { execFileSync, spawn, spawnSync } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readdirSync, statSync } from 'node:fs';
import { homedir, tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { serveCode } from './dev-code.mjs';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const appDir = join(root, 'apps', 'app');
const flags = new Set(process.argv.slice(2).filter((a) => a.startsWith('--')));
const only = process.argv.slice(2).filter((a) => !a.startsWith('--'));
const appId = process.env.APP_BUNDLE_ID ?? 'com.myfantasyprem.app.dev';
const scheme = process.env.APP_SCHEME ?? 'myfantasyprem';
const apiMode = flags.has('--api');
const apiUrl = process.env.API_URL ?? 'http://localhost:8080';
const apiLog = process.env.API_LOG ?? '';
if (apiMode && !apiLog) fail('--api needs API_LOG, the file the API logs to (docs/IOS.md).');

function fail(message) {
  console.error(`test:ios: ${message}`);
  process.exit(1);
}

function run(cmd, args, opts = {}) {
  const r = spawnSync(cmd, args, { stdio: 'inherit', ...opts });
  if (r.status !== 0) fail(`${cmd} ${args.join(' ')} failed.`);
}

function maestroBin() {
  if (process.env.MAESTRO_BIN) return process.env.MAESTRO_BIN;
  const onPath = spawnSync('which', ['maestro'], { encoding: 'utf8' });
  if (onPath.status === 0) return onPath.stdout.trim();
  const local = join(homedir(), '.maestro', 'bin', 'maestro');
  if (existsSync(local)) return local;
  fail('Maestro not found. Install it (docs/IOS.md, "Native smoke tests") or set MAESTRO_BIN.');
}

function bootedDevice() {
  if (process.env.IOS_DEVICE) return process.env.IOS_DEVICE;
  const out = execFileSync('xcrun', ['simctl', 'list', 'devices', 'booted', '-j'], {
    encoding: 'utf8',
  });
  const booted = Object.values(JSON.parse(out).devices)
    .flat()
    .find((d) => d.state === 'Booted');
  if (!booted) fail('No booted simulator. Boot one (docs/IOS.md) or set IOS_DEVICE.');
  return booted.udid;
}

const device = bootedDevice();

if (flags.has('--build')) {
  // The Release build embeds its JS bundle: no Metro needed. EXPO_PUBLIC_*
  // values are inlined then, so the mock and API builds are separate builds.
  const env = { ...process.env, APP_BUNDLE_ID: appId };
  if (apiMode) {
    delete env.EXPO_PUBLIC_USE_MOCKS;
    env.EXPO_PUBLIC_API_BASE_URL = apiUrl;
  } else {
    env.EXPO_PUBLIC_USE_MOCKS = 'true';
  }
  if (!existsSync(join(appDir, 'ios', 'Podfile.lock'))) {
    run('npx', ['expo', 'prebuild', '--platform', 'ios'], { cwd: appDir, env });
  }
  run(
    'xcodebuild',
    [
      '-workspace',
      'ios/MyFantasyPrem.xcworkspace',
      '-scheme',
      'MyFantasyPrem',
      '-configuration',
      'Release',
      '-destination',
      `id=${device}`,
      '-derivedDataPath',
      'ios/build',
      '-quiet',
      'build',
    ],
    { cwd: appDir, env },
  );
  run('xcrun', [
    'simctl',
    'install',
    device,
    join(appDir, 'ios/build/Build/Products/Release-iphonesimulator/MyFantasyPrem.app'),
  ]);
}

// Pre-approve the scheme for links opened from outside the app: a fresh
// simulator otherwise asks "Open in <app>?" on every openLink, a system
// alert Maestro cannot tap. The same record iOS writes when you tap Open.
run('xcrun', [
  'simctl',
  'spawn',
  device,
  'defaults',
  'write',
  'com.apple.launchservices.schemeapproval',
  `com.apple.CoreSimulator.CoreSimulatorBridge-->${scheme}`,
  '-string',
  appId,
]);

const flowsDir = join(root, 'e2e', 'ios', apiMode ? 'api-flows' : 'flows');
const flows = only.length ? only.map((name) => join(flowsDir, `${name}.yaml`)) : [flowsDir];
const out = mkdtempSync(join(tmpdir(), 'mfp-maestro-'));
const codeServer = apiMode ? await serveCode(apiLog) : null;
const vars = [`APP_ID=${appId}`, `APP_SCHEME=${scheme}`];
if (codeServer) vars.push(`CODE_URL=${codeServer.url}`);
// Async, so the code server above can answer while Maestro runs.
const status = await new Promise((resolve) => {
  const child = spawn(
    maestroBin(),
    [
      '--device',
      device,
      'test',
      ...vars.flatMap((v) => ['-e', v]),
      '--test-output-dir',
      out,
      ...flows,
    ],
    {
      stdio: 'inherit',
      env: {
        ...process.env,
        MAESTRO_CLI_NO_ANALYTICS: '1',
        MAESTRO_CLI_ANALYSIS_NOTIFICATION_DISABLED: 'true',
      },
    },
  );
  child.on('exit', (code) => resolve(code ?? 1));
  child.on('error', () => resolve(1));
});
await codeServer?.close();

if (flags.has('--screenshots')) {
  const dest = join(root, 'docs', 'screenshots', 'ios');
  mkdirSync(dest, { recursive: true });
  let copied = 0;
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      const path = join(dir, name);
      if (statSync(path).isDirectory()) walk(path);
      else if (/^ios-.*\.png$/.test(name)) {
        copyFileSync(path, join(dest, name));
        // Half size (the simulator captures at 3x) keeps the repo light.
        execFileSync('sips', ['--resampleHeight', '1280', join(dest, name)], { stdio: 'ignore' });
        copied += 1;
      }
    }
  };
  walk(out);
  console.log(`test:ios: copied ${copied} screenshots to docs/screenshots/ios`);
}

console.log(`test:ios: Maestro output in ${out}`);
process.exit(status);
