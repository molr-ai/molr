import type { MolrAppAPI } from '@molr/ui-host/app';
import type { NotificationAdapter, NotificationCapability } from './types';

export function createDesktopNotifications(bridge: MolrAppAPI['notifications'], open: (route: string) => void): NotificationAdapter {
  // A new scope for every login/provider lifetime prevents stale clicks and
  // cleanup from affecting a later account in the same Runtime view.
  const scope = Array.from(crypto.getRandomValues(new Uint32Array(4)), value => value.toString(16)).join('-');
  let disposed = false, state: NotificationCapability = 'available';
  const ready = bridge.begin(scope);
  void ready.catch(() => { state = 'unsupported'; });
  const unsubscribe = bridge.onClick((owner, route) => { if (!disposed && owner === scope) open(route); });
  async function close(tag?: string) {
    try { await ready; await bridge.close(scope, tag); } catch { /* A closed view cannot retain this scope. */ }
  }
  return {
    capability: () => state,
    async requestPermission() {
      await ready;
      if (!disposed) state = await bridge.requestPermission() ? 'available' : 'unsupported';
      return state;
    },
    async show(value) {
      await ready;
      if (disposed) throw new Error('Notification session expired.');
      await bridge.show(scope, value);
    },
    close,
    dispose() { disposed = true; unsubscribe(); void close(); },
  };
}
