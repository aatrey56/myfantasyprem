/* Push notifications (Web Push, RFC 8030/8291). Registered by
 * src/lib/web-push.ts with scope /push/, so it never competes with the
 * mock builds' MSW worker at /. It only shows pushes and opens their link:
 * no fetch handler, no caching. */
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()));

self.addEventListener('push', (event) => {
  let payload = {};
  try {
    payload = event.data ? event.data.json() : {};
  } catch {
    payload = { title: 'MyFantasyPrem', body: event.data ? event.data.text() : '' };
  }
  const title = payload.title || 'MyFantasyPrem';
  event.waitUntil(
    self.registration.showNotification(title, {
      body: payload.body || '',
      tag: payload.id || undefined,
      icon: '/icons/icon-192.png',
      data: { url: payload.url || '/feed' },
    }),
  );
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const url = new URL(
    (event.notification.data && event.notification.data.url) || '/feed',
    self.location.origin,
  ).href;
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((windows) => {
      for (const w of windows) {
        if (new URL(w.url).origin === self.location.origin && 'focus' in w) {
          return w.focus().then(() => ('navigate' in w ? w.navigate(url) : undefined));
        }
      }
      return self.clients.openWindow(url);
    }),
  );
});
