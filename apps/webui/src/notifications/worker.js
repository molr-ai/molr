// Notification display/click handling only. No offline cache or Web Push.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', event => event.waitUntil(self.clients.claim()));
self.addEventListener('notificationclick', event => {
  const { account, route } = event.notification.data ?? {};
  event.notification.close();
  if (typeof account !== 'string' || !/^#\/(?:assistants(?:\/[A-Za-z0-9_-]{1,128}(?:\?panel=(?:info|permissions|channel|tasks)(?:&task=[A-Za-z0-9_-]{1,128})?)?)|plans\/[A-Za-z0-9_-]{1,128}|workbench\/[A-Za-z0-9_-]{1,128}\?event=[A-Za-z0-9_-]{1,128})$/.test(route)) return;
  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    const target = windows.find(client => new URL(client.url).origin === self.location.origin);
    if (target) {
      await target.focus();
      target.postMessage({ type: 'molr.notification.open', account, route });
    } else {
      await self.clients.openWindow(new URL(route, self.registration.scope).href);
    }
  })());
});
