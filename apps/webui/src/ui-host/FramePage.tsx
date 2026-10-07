import { useEffect, useRef, useState } from 'react';
import { Button } from '@molr/ui';
import { api, APIError } from '@molr/sdk';
import { isChatDraft, uiPages } from '@molr/sdk/ui';
import type { ChatDraft, UIContext, UIPackageManifest, UIPage } from '@molr/sdk/ui';
import { requestPermission } from '@molr/ui-host';
import { useUIHost } from './store';
import { useCopy, useData } from '../state';
import { hostCopy } from './copy';

export function FramePage({ manifest, context, navigate, recover }: { manifest: UIPackageManifest; context: UIContext; navigate: (page: UIPage, id?: string) => void; recover: () => void }) {
  const { locale } = useCopy(), copy = hostCopy[locale], store = useUIHost(), data = useData();
  const frame = useRef<HTMLIFrameElement>(null), portRef = useRef<MessagePort | null>(null);
  const latest = useRef({ context, store, navigate }); latest.current = { context, store, navigate };
  const [state, setState] = useState<'loading' | 'ready' | 'failed'>('loading'), [attempt, setAttempt] = useState(0);
  const entry = manifest.pages[context.route.page]!;
  useEffect(() => {
    let disposed = false, connected = false, activeRequests = 0;
    setState('loading');
    const timeout = setTimeout(() => setState('failed'), 15_000);
    const controller = new AbortController();
    function ready(event: MessageEvent) {
      if (connected || event.source !== frame.current?.contentWindow || event.origin !== 'null' || event.data?.type !== 'molr.ui.ready' || event.data.apiVersion !== 1) return;
      connected = true; clearTimeout(timeout);
      const channel = new MessageChannel(), port = channel.port1; portRef.current = port;
      port.onmessage = async ({ data: message }) => {
        if (disposed || !Number.isSafeInteger(message?.id) || typeof message?.method !== 'string') return;
        const respond = (value: unknown) => { if (!disposed) port.postMessage(value); };
        if (activeRequests >= 16) { respond({ id: message.id, error: { code: 'rate_limited', status: 429 } }); return; }
        activeRequests++;
        try {
          const args = message.args; let value: unknown;
          if (!args || typeof args !== 'object') throw new APIError('invalid_argument');
          if (message.method === 'request') {
            const permission = requestPermission(args.method, args.path);
            if (!permission || !manifest.permissions.includes(permission)) throw new APIError('permission_denied', '', 403);
            if (args.method === 'GET') value = args.binary ? await api.download(args.path, controller.signal) : await api.get(args.path, controller.signal);
            else if (args.method === 'DELETE') value = await api.remove(args.path);
            else if (args.path === '/api/files' && args.body instanceof Blob) {
              if (args.body.size > 32 * 1024 * 1024) throw new APIError('invalid_argument');
              const headers = new Headers(args.headers);
              value = await api.upload(new File([args.body], decodeURIComponent(headers.get('X-File-Name') || 'attachment'), { type: args.body.type }), headers.get('Idempotency-Key') || '');
            } else {
              if (typeof args.body !== 'string' || args.body.length > 1024 * 1024) throw new APIError('invalid_argument');
              const body = JSON.parse(args.body); if (!body || typeof body !== 'object' || Array.isArray(body)) throw new APIError('invalid_argument');
              value = await api.post(args.path, body);
            }
          } else if (message.method === 'navigate') {
            if (!uiPages.includes(args.page) || typeof args.id !== 'string' || args.id && !/^[A-Za-z0-9_-]{1,128}$/.test(args.id)) throw new APIError('invalid_argument');
            latest.current.navigate(args.page, args.id);
          } else if (message.method === 'draft.get' || message.method === 'draft.set') {
            const required = message.method === 'draft.get' ? 'chat.read' : 'chat.write';
            if (!manifest.permissions.includes(required)) throw new APIError('permission_denied', '', 403);
            if (typeof args.assistantID !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(args.assistantID)) throw new APIError('invalid_argument');
            if (message.method === 'draft.get') value = Object.hasOwn(latest.current.store.drafts, args.assistantID) ? latest.current.store.drafts[args.assistantID] : { text: '', files: [] };
            else {
              const draft = args.draft as ChatDraft;
              if (!isChatDraft(draft)) throw new APIError('invalid_argument');
              latest.current.store.setDrafts(previous => ({ ...previous, [args.assistantID]: draft }));
            }
          } else throw new APIError('unsupported_host_method');
          respond({ id: message.id, value });
        } catch (error) { const known = error instanceof APIError ? error : new APIError('invalid_argument'); respond({ id: message.id, error: { code: known.code, status: known.status, requestID: known.requestID } }); }
        finally { activeRequests--; }
      };
      port.start(); frame.current!.contentWindow!.postMessage({ type: 'molr.ui.connect', context: latest.current.context }, '*', [channel.port2]); setState('ready');
    }
    window.addEventListener('message', ready);
    return () => { disposed = true; clearTimeout(timeout); controller.abort(); window.removeEventListener('message', ready); portRef.current?.close(); portRef.current = null; };
  }, [entry, manifest, attempt]);
  useEffect(() => { portRef.current?.postMessage({ type: 'context', context }); }, [context]);
  const revision = ['messages', 'plans', 'resources', 'settings', 'assistants', 'status'].map(topic => data.version(topic)).join(':');
  useEffect(() => { portRef.current?.postMessage({ type: 'data' }); }, [revision]);
  return <section className="ui-frame-page">
    <div className="ui-frame-toolbar"><span>{manifest.name} <small>{manifest.version}</small></span><Button size="sm" variant="ghost" onClick={recover}>{copy.recover}</Button></div>
    {state === 'loading' && <p className="ui-frame-notice" role="status">{copy.loading}</p>}
    {state === 'failed' && <div className="ui-frame-notice" role="alert"><h2>{copy.failed}</h2><p>{copy.failedHelp}</p><div className="form-actions"><Button onClick={() => setAttempt(value => value + 1)}>{copy.retry}</Button><Button variant="secondary" onClick={recover}>{copy.recover}</Button></div></div>}
    <iframe key={`${entry}:${attempt}`} ref={frame} title={`${manifest.name} — ${copy.routes[context.route.page]}`} src={entry} sandbox="allow-scripts allow-forms" referrerPolicy="no-referrer" hidden={state === 'failed'} onError={() => setState('failed')} />
  </section>;
}
