/** Bundle into a standalone plugin HTML. The host supplies identity and scope. */
import type { PluginConnection, PluginUserSettings } from './plugins';
export type PluginContext = { apiVersion: 2; locale: 'zh' | 'en'; theme: 'light' | 'dark'; reducedMotion: boolean; tokens: Record<string, string> };
export type PluginHost = {
  context: PluginContext;
  plans: { list: () => Promise<unknown> };
  settings: { get: () => Promise<PluginUserSettings>; update: (input: { expectedVersion: number; values: PluginUserSettings['Values'] }) => Promise<PluginUserSettings> };
  connections: { list: () => Promise<PluginConnection[]>; test: (input: { connectionId: string; idempotencyKey: string }) => Promise<unknown> };
  dispose: () => void;
};
export async function connectPlugin(timeout = 10000): Promise<PluginHost> {
  if (window.parent === window) throw new Error('Plugin host required');
  return new Promise((resolve, reject) => {
    const ready = (event: MessageEvent) => {
      if (event.source !== window.parent || event.data?.type !== 'molr.plugin.connect' || event.data.context?.apiVersion !== 2 || !event.ports[0]) return;
      clearTimeout(timer); window.removeEventListener('message', ready);
      const port = event.ports[0], pending = new Map<number, { resolve: (value: unknown) => void; reject: (error: Error) => void; timer: ReturnType<typeof setTimeout> }>();
      let id = 0, disposed = false;
      port.onmessage = ({ data }) => { const item = pending.get(data?.id); if (!item) return; pending.delete(data.id); clearTimeout(item.timer); data.error ? item.reject(new Error(data.error)) : item.resolve(data.value); };
      const call = <T>(method: string, params?: unknown) => new Promise<T>((resolve, reject) => {
        if (disposed) { reject(new Error('Plugin disconnected')); return; }
        if (pending.size >= 4) { reject(new Error('Too many plugin requests')); return; }
        const requestID = ++id, timer = setTimeout(() => { pending.delete(requestID); reject(new Error('Plugin request timed out')); }, method === 'connections.test' ? 65000 : timeout);
        pending.set(requestID, { resolve: value => resolve(value as T), reject, timer }); port.postMessage({ id: requestID, method, params });
      });
      resolve({ context: event.data.context,
        plans: { list: () => call('plans.list') },
        settings: { get: () => call('settings.get'), update: input => call('settings.update', input) },
        connections: { list: () => call('connections.list'), test: input => call('connections.test', input) },
        dispose() { disposed = true; port.close(); for (const item of pending.values()) { clearTimeout(item.timer); item.reject(new Error('Plugin disconnected')); } pending.clear(); },
      });
    };
    const timer = setTimeout(() => { window.removeEventListener('message', ready); reject(new Error('Plugin handshake timed out')); }, timeout);
    window.addEventListener('message', ready); window.parent.postMessage({ type: 'molr.plugin.ready', apiVersion: 2 }, '*');
  });
}
