// expo-router parses URL query params (e.g. ?gw=8) with query-string, which
// require()s decode-uri-component. An ESM-only decode-uri-component (>=0.5)
// breaks that at runtime, so resolve query-string the way expo-router does.
describe('query-string as used by expo-router', () => {
  it('parses percent-encoded params', () => {
    const entry = require.resolve('query-string', {
      paths: [require.resolve('expo-router')],
    });
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const qs = require(entry) as { parse: (s: string) => Record<string, unknown> };
    expect({ ...qs.parse('?gw=8&name=a%20b') }).toEqual({ gw: '8', name: 'a b' });
  });
});
