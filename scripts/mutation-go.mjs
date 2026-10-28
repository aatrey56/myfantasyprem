// Mutation testing for the rule-heavy Go packages (gremlins). Slow and
// advisory: NOT part of .office/checks. See docs/testing.md.
// Usage: npm run test:mutation:go [-- scoring draft ...]
import { spawnSync } from 'node:child_process';

const GREMLINS = 'github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0';
const DEFAULT = ['scoring', 'draft', 'lineup', 'waivers', 'trades', 'league'];
const pkgs = process.argv.slice(2).length ? process.argv.slice(2) : DEFAULT;

const rows = [];
for (const pkg of pkgs) {
  console.log(`\n=== internal/${pkg}`);
  const res = spawnSync(
    'go',
    [
      'run',
      GREMLINS,
      'unleash',
      '--invert-logical',
      '--workers',
      '2',
      '--timeout-coefficient',
      '30',
      '-S',
      'lt',
      `./internal/${pkg}`,
    ],
    { cwd: 'server', encoding: 'utf8' },
  );
  if (res.error) {
    console.error(`mutation-go: ${res.error.message}`);
    process.exit(1);
  }
  const out = `${res.stdout}${res.stderr}`;
  console.log(out.trim());
  if (res.status !== 0) {
    console.error(
      `mutation-go: gremlins failed for internal/${pkg} (exit ${res.status ?? `signal ${res.signal}`}); no score reported`,
    );
    process.exit(res.status ?? 1);
  }
  const killed = Number(/Killed: (\d+)/.exec(out)?.[1] ?? NaN);
  const lived = Number(/Lived: (\d+)/.exec(out)?.[1] ?? NaN);
  const uncovered = Number(/Not covered: (\d+)/.exec(out)?.[1] ?? NaN);
  const timedOut = Number(/Timed out: (\d+)/.exec(out)?.[1] ?? NaN);
  if ([killed, lived, uncovered, timedOut].some(Number.isNaN)) {
    console.error(`mutation-go: could not parse gremlins output for internal/${pkg}`);
    process.exit(1);
  }
  rows.push({ pkg, killed, lived, uncovered, timedOut });
}

console.log('\npackage    killed/(killed+lived)  not-covered  timed-out');
for (const r of rows) {
  const score = `${r.killed}/${r.killed + r.lived}`;
  console.log(
    `${r.pkg.padEnd(10)} ${score.padEnd(22)} ${String(r.uncovered).padEnd(12)} ${r.timedOut}`,
  );
}
