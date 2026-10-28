import { test as base, expect } from '@playwright/test';

export { expect };
export type { Locator, Page } from '@playwright/test';

/** Hosts the e2e build may reach: the local static server, nothing else. */
const LOCAL_HOSTS = new Set(['localhost', '127.0.0.1', '[::1]']);

function isExternal(url: string): boolean {
  const u = new URL(url);
  if (u.protocol === 'data:' || u.protocol === 'blob:' || u.protocol === 'about:') return false;
  return !LOCAL_HOSTS.has(u.hostname);
}

/**
 * Every spec imports `test` from here: it fails any test whose page asks for
 * anything outside localhost (no FPL, no Premier League image hosts, no
 * CDNs). Such requests are also blocked, so a stray one cannot pass by
 * reaching the network.
 */
export const test = base.extend<{ noExternalRequests: void }>({
  noExternalRequests: [
    async ({ context }, use) => {
      const external: string[] = [];
      context.on('request', (req) => {
        if (isExternal(req.url())) external.push(req.url());
      });
      await context.route(
        (url) => isExternal(url.href),
        (route) => {
          external.push(route.request().url());
          return route.abort('blockedbyclient');
        },
      );
      await use();
      expect([...new Set(external)], 'requests outside localhost').toEqual([]);
    },
    { auto: true },
  ],
});
