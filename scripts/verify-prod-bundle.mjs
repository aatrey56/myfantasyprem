// Fails if a production web build contains mock code or mock data, or
// loads club or player images from anywhere but our API (/v1/assets): the
// app never hotlinks upstream (AGENTS.md hard rules).
// Usage: node scripts/verify-prod-bundle.mjs apps/app/dist
// Run after `npm run build:web`. Metro's cache once leaked MSW into a prod
// build; this is the guard.
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';

const dir = resolve(process.argv[2] ?? 'apps/app/dist');
// Strings that only exist in the mock layer (MSW and src/mocks/).
const FORBIDDEN = [
  'setupWorker',
  'mockServiceWorker',
  'The Sunday Draft',
  'Sunday League Heroes',
  'The Office League',
  'Draft Day Derby',
  'mock-access-token',
  'mock-apple-identity-token',
  'mock-magic-link-token',
  'mfp.mockScenario',
];
// Upstream image hosts and paths: images come only through /v1/assets.
const HOTLINKS = ['resources.premierleague.com', '/img/shirts/', '/photos/players/', '/badges/t'];
const TEXT = /\.(html|js|mjs|css|json|map|txt)$/;

if (!existsSync(dir) || !statSync(dir).isDirectory()) {
  console.error(`verify-prod-bundle: ${dir} does not exist. Run npm run build:web first.`);
  process.exit(1);
}

function* files(d) {
  for (const entry of readdirSync(d, { withFileTypes: true })) {
    const p = join(d, entry.name);
    if (entry.isDirectory()) yield* files(p);
    else yield p;
  }
}

const problems = [];
let scanned = 0;
for (const file of files(dir)) {
  const rel = relative(dir, file);
  if (/mockServiceWorker\.js$/.test(file)) problems.push(`${rel}: MSW worker file present`);
  if (!TEXT.test(file)) continue;
  scanned++;
  const text = readFileSync(file, 'utf8');
  for (const needle of FORBIDDEN) {
    if (text.includes(needle)) problems.push(`${rel}: contains "${needle}"`);
  }
  for (const needle of HOTLINKS) {
    if (text.includes(needle)) problems.push(`${rel}: hotlinks upstream images ("${needle}")`);
  }
}

if (problems.length) {
  console.error('verify-prod-bundle: mock code or upstream image links in the production build:');
  for (const p of problems) console.error(`  - ${p}`);
  process.exit(1);
}
console.log(
  `verify-prod-bundle: OK (${scanned} text files in ${relative(process.cwd(), dir) || dir}, no mock code, no upstream image links)`,
);
