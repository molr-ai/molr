import { ChevronLeft, PanelsTopLeft, Settings2 } from 'lucide-react';
import { PageHeader } from '../interfaces/official/components/PageHeader';
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { Button, IconButton, useTheme } from '@molr/ui';
import type { Settings } from '@molr/sdk';
import { api } from '@molr/sdk';
import type { PluginUIReference } from '@molr/sdk/plugins';
import { useCopy, useQuery, useSession } from '../state';
import { useUIHost } from '../ui-host/store';
import { pluginCopy } from './copy';
import './plugins.css';

// srcdoc/blob documents have opaque origins. The parent's frame-src policy also
// prevents a plugin from navigating its own frame to a network URL.
const policy = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'none'; font-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'";
export function PluginFrame({ reference, title }: { reference: PluginUIReference; title: string }) {
  const { locale } = useCopy(), copy = pluginCopy[locale], { theme } = useTheme(), host = useUIHost();
  const administrator = useSession().principal?.User?.Administrator === true;
  const preferences = useQuery<Settings>('/api/settings');
  const palette = preferences.data?.Palette, reducedMotion = preferences.data?.ReducedMotion, preferencesReady = !!preferences.data;
  const frame = useRef<HTMLIFrameElement>(null), [attempt, retry] = useState(0), [document, setDocument] = useState<{ key: string; html: string } | null>(null), [error, setError] = useState(false), [ready, setReady] = useState(false);
  const refKey = JSON.stringify(reference);
  const stableRef = useMemo(() => reference, [refKey]);
  const installed = host.plugins.find(p => p.ID === reference.ID);
  const revision = `${installed?.Version}:${installed?.UserVersion}:${installed?.UserEnabled}`;
  const documentKey = JSON.stringify([refKey, attempt, revision]);
  useEffect(() => {
    const controller = new AbortController(); setDocument(null); setError(false); setReady(false);
    void api.post<{ HTML: string }>('/api/plugins/ui', { ...stableRef, Method: 'document' }).then(value => { if (!controller.signal.aborted) setDocument({ key: documentKey, html: value.HTML }); }).catch(() => { if (!controller.signal.aborted) setError(true); });
    return () => controller.abort();
  }, [stableRef, documentKey]);
  const [blob, setBlob] = useState<{ document: NonNullable<typeof document>; url: string } | null>(null);
  const currentDocument = document?.key === documentKey && preferencesReady && !preferences.error && !error ? document : null;
  useEffect(() => {
    if (!currentDocument?.html) { setBlob(null); return; }
    // Allocate only after commit: StrictMode and interrupted renders must not
    // leave unowned URLs. The same effect owns creation and revocation.
    const url = URL.createObjectURL(new Blob([`<!doctype html><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${policy}"><meta name="referrer" content="no-referrer">`, currentDocument.html], { type: 'text/html;charset=utf-8' }));
    setBlob({ document: currentDocument, url });
    return () => URL.revokeObjectURL(url);
  }, [currentDocument]);
  // Never connect an old document to a new plugin reference while effects settle.
  const url = currentDocument && blob?.document === currentDocument ? blob.url : '';
  useLayoutEffect(() => {
    setReady(false);
    if (!url) return;
    let disposed = false, port: MessagePort | undefined, active = 0;
    const timer = window.setTimeout(() => setError(true), 10000);
    function connect(event: MessageEvent) {
      if (port || event.source !== frame.current?.contentWindow || event.origin !== 'null' || event.data?.type !== 'molr.plugin.ready' || event.data.apiVersion !== 2) return;
      const channel = new MessageChannel(); port = channel.port1;
      port.onmessage = async ({ data }) => {
        if (disposed || !Number.isSafeInteger(data?.id) || !['plans.list', 'settings.get', 'settings.update', 'connections.list', 'connections.test'].includes(data.method)) return;
        if (active >= 4) { port?.postMessage({ id: data.id, error: 'rate_limited' }); return; }
        active++;
        try { const value = await api.post('/api/plugins/ui', { ...stableRef, Method: data.method, ...(data.method === 'settings.update' ? { ExpectedVersion: data.params?.expectedVersion, Values: data.params?.values } : {}), ...(data.method === 'connections.test' ? { ConnectionID: data.params?.connectionId, Mutation: { IdempotencyKey: data.params?.idempotencyKey } } : {}) }); if (!disposed) port?.postMessage({ id: data.id, value }); }
        catch { if (!disposed) port?.postMessage({ id: data.id, error: 'unavailable_or_denied' }); }
        finally { active--; }
      };
      const tokens: Record<string, string> = {};
      const styles = getComputedStyle(window.document.documentElement);
      for (const key of ['--color-background', '--color-surface', '--color-text', '--color-text-muted', '--color-border', '--color-primary', '--color-on-primary', '--color-positive', '--color-selected-bg', '--color-focus', '--color-neutral-bg', '--color-positive-bg', '--radius-control', '--font-sans']) tokens[key] = styles.getPropertyValue(key);
      frame.current!.contentWindow!.postMessage({ type: 'molr.plugin.connect', context: { apiVersion: 2, locale, theme, reducedMotion: window.document.documentElement.dataset.reducedMotion === 'true', tokens } }, '*', [channel.port2]);
      window.clearTimeout(timer); setReady(true);
    }
    window.addEventListener('message', connect);
    return () => { disposed = true; window.clearTimeout(timer); port?.close(); window.removeEventListener('message', connect); };
  }, [url, stableRef, locale, theme, palette, reducedMotion, revision]);
  return <section className="plugin-frame"><PageHeader title={title} icon={<PanelsTopLeft />} leading={reference.Kind === 'page' ? <IconButton label={copy.extensions} onClick={() => { location.hash = '/extensions'; }}><ChevronLeft size={18} /></IconButton> : undefined} actions={administrator && <IconButton label={copy.recover} onClick={() => { location.hash = '/extensions'; }}><Settings2 size={18} /></IconButton>} />
    {error || preferences.error ? <div className="plugin-notice" role="alert"><h2>{copy.unavailable}</h2><p>{copy.permission}</p><Button onClick={() => retry(n => n + 1)}>{copy.retry}</Button></div> : <>{!ready && <p role="status">{copy.loading}</p>}{url && <iframe key={`${url}:${locale}:${theme}:${palette}:${reducedMotion}:${revision}`} ref={frame} title={title} src={url} hidden={!ready} data-ready={ready} sandbox="allow-scripts" referrerPolicy="no-referrer" />}</>}
  </section>;
}
