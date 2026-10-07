import { APIError, createClient } from './client';
import type { RuntimeTransport } from './transport';
import type { UIContext, UIHost } from './ui';

/** Connect from a sandboxed UI document. The host supplies a private MessagePort. */
export function connectUI(): Promise<UIHost & { api: ReturnType<typeof createClient>['api'] }> {
  return new Promise((resolve, reject) => {
    if (window.parent === window) { reject(new Error('UI must be opened by the Molr host')); return; }
    const timeout = setTimeout(() => { window.removeEventListener('message', initialize); reject(new Error('host_timeout')); }, 15_000);
    function initialize(event: MessageEvent) {
      if (event.source !== window.parent || event.data?.type !== 'molr.ui.connect' || event.data?.context?.apiVersion !== 1 || event.ports.length !== 1) return;
      clearTimeout(timeout); window.removeEventListener('message', initialize);
      const port = event.ports[0]; let sequence = 0, disposed = false;
      const pending = new Map<number, { resolve: (value: any) => void; reject: (error: unknown) => void; timer: ReturnType<typeof setTimeout>; abort?: () => void }>();
      const contexts = new Set<(context: UIContext) => void>(), changes = new Set<() => void>();
      const call = <T>(method: string, args: unknown, signal?: AbortSignal): Promise<T> => new Promise((resolve, reject) => {
        if (disposed) { reject(new APIError('host_disconnected')); return; }
        if (signal?.aborted) { reject(new DOMException('Aborted', 'AbortError')); return; }
        const id = ++sequence;
        const abort = () => { const item = pending.get(id); if (!item) return; pending.delete(id); clearTimeout(item.timer); reject(new DOMException('Aborted', 'AbortError')); };
        const timer = setTimeout(() => { pending.delete(id); signal?.removeEventListener('abort', abort); reject(new APIError('host_timeout')); }, 30_000);
        pending.set(id, { resolve, reject, timer, abort: () => signal?.removeEventListener('abort', abort) });
        signal?.addEventListener('abort', abort, { once: true });
        port.postMessage({ id, method, args });
      });
      const transport: RuntimeTransport = (path, init, binary) => call('request', { path, method: init.method ?? 'GET', body: init.body, headers: [...new Headers(init.headers).entries()], binary }, init.signal ?? undefined);
      const host: UIHost & { api: ReturnType<typeof createClient>['api'] } = {
        context: event.data.context,
        api: createClient(transport).api,
        navigate: (page, id = '') => call('navigate', { page, id }),
        onContext: listener => { contexts.add(listener); return () => { contexts.delete(listener); }; },
        onDataChange: listener => { changes.add(listener); return () => { changes.delete(listener); }; },
        drafts: { get: assistantID => call('draft.get', { assistantID }), set: (assistantID, draft) => call('draft.set', { assistantID, draft }) },
        dispose: () => { disposed = true; port.close(); for (const item of pending.values()) { clearTimeout(item.timer); item.abort?.(); item.reject(new APIError('host_disconnected')); } pending.clear(); contexts.clear(); changes.clear(); },
      };
      port.onmessage = ({ data }) => {
        if (data?.type === 'context') { host.context = data.context; contexts.forEach(listener => listener(host.context)); return; }
        if (data?.type === 'data') { changes.forEach(listener => listener()); return; }
        const item = pending.get(data?.id); if (!item) return;
        pending.delete(data.id); clearTimeout(item.timer); item.abort?.();
        if (data.error) item.reject(new APIError(data.error.code, data.error.requestID, data.error.status)); else item.resolve(data.value);
      };
      port.start(); resolve(host);
    }
    window.addEventListener('message', initialize);
    window.parent.postMessage({ type: 'molr.ui.ready', apiVersion: 1 }, '*');
  });
}
