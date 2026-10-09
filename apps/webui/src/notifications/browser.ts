import type { MessageNotification, NotificationAdapter, NotificationCapability } from './types';

export function createBrowserNotifications(account: string, open: (route: string) => void): NotificationAdapter {
  const prefix = `molr:${account}:`, active = new Map<string, Notification>(), revisions = new Map<string, number>();
  let registration: Promise<ServiceWorkerRegistration> | undefined, disposed = false, revision = 0;
  const capability = (): NotificationCapability => !window.isSecureContext ? 'insecure' : !('Notification' in window) ? 'unsupported' : Notification.permission;
  const worker = () => registration ??= navigator.serviceWorker.register(`${import.meta.env.BASE_URL}molr-notifications-sw.js`).then(async value => {
    if (!value.active) await new Promise<void>((resolve, reject) => {
      const installing = value.installing ?? value.waiting;
      if (!installing) { reject(new Error('Notification worker unavailable')); return; }
      const timeout = setTimeout(() => { installing.removeEventListener('statechange', changed); reject(new Error('Notification worker timed out')); }, 10_000);
      function changed() {
        if (installing!.state === 'activated' || installing!.state === 'redundant') {
          clearTimeout(timeout); installing!.removeEventListener('statechange', changed);
          if (installing!.state === 'activated') resolve(); else reject(new Error('Notification worker unavailable'));
        }
      }
      installing.addEventListener('statechange', changed); changed();
    });
    return value;
  }).catch(error => { registration = undefined; throw error; });
  const clicked = (event: MessageEvent) => {
    if (event.data?.type === 'molr.notification.open' && event.data.account === account && /^#\/(?:assistants(?:\/[A-Za-z0-9_-]{1,128}(?:\?panel=(?:info|permissions|channel|tasks)(?:&task=[A-Za-z0-9_-]{1,128})?)?)|plans\/[A-Za-z0-9_-]{1,128}|workbench\/[A-Za-z0-9_-]{1,128}\?event=[A-Za-z0-9_-]{1,128})$/.test(event.data.route)) open(event.data.route);
  };
  navigator.serviceWorker?.addEventListener('message', clicked);
  async function close(tag?: string) {
    if (tag) revisions.set(tag, (revisions.get(tag) ?? 0) + 1); else revision++;
    for (const [key, value] of active) if (!tag || key === tag) { value.close(); active.delete(key); }
    try {
      const value = registration ? await registration : await navigator.serviceWorker?.getRegistration(`${import.meta.env.BASE_URL}molr-notifications-sw.js`);
      for (const notification of await value?.getNotifications() ?? []) if (notification.tag.startsWith(prefix) && (!tag || notification.tag === prefix + tag)) notification.close();
    } catch { /* Closing a notification must not interrupt logout or reading. */ }
  }
  return {
    capability,
    async requestPermission() {
      if (capability() === 'default') await Notification.requestPermission();
      return capability();
    },
    async show(value: MessageNotification) {
      if (disposed || capability() !== 'granted') throw new Error('Notifications unavailable');
      if ('serviceWorker' in navigator) {
        const started = revision, tagRevision = revisions.get(value.tag);
        const canceled = () => disposed || started !== revision || tagRevision !== revisions.get(value.tag);
        const service = await worker();
        if (canceled()) return;
        await service.showNotification(value.title, { body: value.body, tag: prefix + value.tag, silent: value.silent ?? true, data: { account, route: value.route } });
        if (canceled()) await close(value.tag);
      } else {
        active.get(value.tag)?.close();
        const notification = new Notification(value.title, { body: value.body, tag: prefix + value.tag, silent: value.silent ?? true });
        notification.onclick = () => { if (!disposed) { if (value.route) { window.focus(); open(value.route); } notification.close(); } };
        active.set(value.tag, notification);
      }
    },
    close,
    dispose() { disposed = true; navigator.serviceWorker?.removeEventListener('message', clicked); void close(); },
  };
}
