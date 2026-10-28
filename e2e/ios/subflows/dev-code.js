/* global http, output, CODE_URL -- Maestro runScript globals */
// The sign-in code the local API just logged, from the helper that
// `npm run test:ios -- --api` serves at CODE_URL (scripts/dev-code.mjs).
const res = http.get(CODE_URL);
if (!res.ok) throw new Error('no dev sign-in code at ' + CODE_URL + ': ' + res.status);
output.code = res.body.trim();
