import type { RuntimeEvent } from './notifications/types';
import { TurnMetricsCache } from './turn-metrics-cache';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { UIProvider, ToastProvider, useTheme, usePalettePreference, paletteAssetBase } from '@molr/ui';
import type { ConnectionState, UILocale, Palette } from '@molr/ui';
import { api, APIError, clearClientState, csrfToken, onSessionExpired, onMutation } from './api/client';
import type { Principal } from './api/types';
import { messages } from './messages';
import { useRuntimeConnection } from './use-runtime-connection';
import { QueryCache } from './query-cache';
import { batchTopics } from './batch-topics';

const PaletteContext = createContext({ palette: 'forest' as Palette, changePalette: (_: Palette) => {} });
export function useAppearance() { return useContext(PaletteContext); }

const LocaleContext = createContext({ locale: 'zh' as UILocale, setLocale: (_: UILocale) => {} });
export function LocaleProvider({ children }: { children: ReactNode }) {
  const { theme } = useTheme();
  const appearance = usePalettePreference('molr-webui-palette', theme, `${import.meta.env.BASE_URL}brand`);
  const [locale, setLocaleState] = useState<UILocale>(() => {
    try { const value = localStorage.getItem('molr-webui-locale'); if (value === 'zh' || value === 'en') return value; } catch { /* storage is optional */ }
    return navigator.language.toLowerCase().startsWith('zh') ? 'zh' : 'en';
  });
  const setLocale = useCallback((value: UILocale) => { setLocaleState(value); try { localStorage.setItem('molr-webui-locale', value); } catch { /* memory fallback */ } }, []);
  useEffect(() => { document.documentElement.lang = locale === 'zh' ? 'zh-CN' : 'en'; }, [locale]);
  const value = useMemo(() => ({ locale, setLocale }), [locale, setLocale]);
  return <LocaleContext.Provider value={value}><PaletteContext.Provider value={appearance}><UIProvider locale={locale} brandAssetBase={paletteAssetBase(appearance.palette, `${import.meta.env.BASE_URL}brand`)}><ToastProvider>{children}</ToastProvider></UIProvider></PaletteContext.Provider></LocaleContext.Provider>;
}
export function useCopy() { const value = useContext(LocaleContext); return { ...value, t: messages[value.locale] }; }

type SessionState = { principal: Principal | null; loading: boolean; error: unknown; reload: () => Promise<void>; end: () => void };
const SessionContext = createContext<SessionState | null>(null);
export function SessionProvider({ children }: { children: ReactNode }) {
  const [principal, setPrincipal] = useState<Principal | null>(null), [loading, setLoading] = useState(true), [error, setError] = useState<unknown>(null);
  const channel = useRef<BroadcastChannel | null>(null), revision = useRef(0), foregroundRequest = useRef(false);
  const end = useCallback(() => { revision.current++; clearClientState(); setPrincipal(null); setError(null); setLoading(false); channel.current?.postMessage('changed'); }, []);
  const refresh = useCallback(async (foreground: boolean, announce = false) => {
    if (!foreground && foregroundRequest.current) return;
    const request = ++revision.current;
    if (foreground) foregroundRequest.current = true;
    if (foreground) { setLoading(true); setError(null); }
    try {
      const user = await api.get<Principal>('/api/auth/me');
      if (request !== revision.current) return;
      setPrincipal(csrfToken() ? user : null); setError(null);
      if (announce) channel.current?.postMessage('changed');
    } catch (error) {
      if (request !== revision.current) return;
      if (error instanceof APIError && error.status === 401) { clearClientState(); setPrincipal(null); }
      else if (foreground) setError(error);
    } finally { if (request === revision.current) { foregroundRequest.current = false; setLoading(false); } }
  }, []);
  const reload = useCallback(() => refresh(true, true), [refresh]);
  useEffect(() => {
    void refresh(true);
    // Cookies are shared between tabs. A changed identity must unmount the old
    // account's data provider before another account's snapshots are displayed.
    try { channel.current = new BroadcastChannel(`molr-session:${location.origin}`); channel.current.onmessage = () => { setPrincipal(null); clearClientState(); void refresh(true); }; } catch { /* Focus checks also cover browsers without BroadcastChannel. */ }
    const check = () => { if (document.visibilityState === 'visible') void refresh(false); };
    window.addEventListener('focus', check); document.addEventListener('visibilitychange', check);
    const timer = setInterval(check, 30000), unsubscribe = onSessionExpired(() => { revision.current++; clearClientState(); setPrincipal(null); setError(null); setLoading(false); });
    return () => { revision.current++; unsubscribe(); channel.current?.close(); channel.current = null; clearInterval(timer); window.removeEventListener('focus', check); document.removeEventListener('visibilitychange', check); };
  }, [refresh, end]);
  return <SessionContext.Provider value={{ principal, loading, error, reload, end }}>{children}</SessionContext.Provider>;
}
export function useSession() { return useContext(SessionContext)!; }

function queryTopic(path: string) {
  if (/\/assistants\/[^/]+\/weixin/.test(path)) return 'weixin';
  if (path === '/api/account/quotas' || path === '/api/admin/usage') return 'usage';
  if (/\/debug\/runs\/[^/]+\/steps\//.test(path)) return 'debug-payload';
  if (/\/assistants\/[^/]+\/capabilities/.test(path)) return 'capabilities';
  if (/\/assistants\/[^/]+\/status/.test(path)) return 'status';
  if (path.includes('/messages')) return 'messages';
  return path.split('/')[2]?.split('?')[0] ?? '';
}
const DataContext = createContext({ subscribeEvents: (_listener: (event: RuntimeEvent) => void): (() => void) => () => {}, version: (_topic: string): number => 0, invalidate: () => {}, invalidateTopics: (_topics: string[]) => {}, connection: 'reconnecting' as ConnectionState, hasConnectionNotice: false, retryConnection: () => {}, cache: new QueryCache(), turnMetrics: new TurnMetricsCache(), messageChanges: new Map<string, number>(), invalidateMessage: (_id: string): number => 0 });
export function DataProvider({ children }: { children: ReactNode }) {
  const [eventListeners] = useState(() => new Set<(event: RuntimeEvent) => void>());
  const subscribeEvents = useCallback((listener: (event: RuntimeEvent) => void) => { eventListeners.add(listener); return () => { eventListeners.delete(listener); }; }, [eventListeners]);
  const [versions, setVersions] = useState<Record<string, number>>({});
  const [cache] = useState(() => new QueryCache());
  const [turnMetrics] = useState(() => new TurnMetricsCache());
  const [messageChanges] = useState(() => new Map<string, number>());
  const publish = useRef<(topics: string[]) => void>(() => {});
  const invalidate = useCallback(() => setVersions(previous => ({ ...previous, all: (previous.all ?? 0) + 1 })), []);
  const invalidateMessage = useCallback((id: string) => { const revision = cache.next(); messageChanges.set(id, revision); publish.current(['messages']); return revision; }, [cache, messageChanges]);
  const { connection, retryConnection } = useRuntimeConnection(event => {
    try {
      const visible = JSON.parse(event.data) as RuntimeEvent;
      for (const listener of eventListeners) listener(visible);
      if (visible.Topic === 'notification.changed') publish.current(['notifications']);
      else if (visible.Topic === 'weixin.changed') publish.current(['weixin']);
      else if (visible.Topic === 'assistant.profile') publish.current(['assistants', 'conversations']);
      else if (visible.Topic === 'permission.request') publish.current(['permission-requests', 'grants', 'status']);
      else if (visible.Topic === 'conversation.read') publish.current(['conversations']);
      else if (['message.feedback', 'message.accepted', 'message.read'].includes(visible.Topic) && visible.Object?.ID) {
        invalidateMessage(visible.Object.ID);
        if (visible.Topic === 'message.accepted' || visible.Topic === 'message.read') publish.current(['status', 'conversations', 'tasks', 'memories', 'usage']);
      }
      else publish.current(['messages', 'conversations', 'status', 'tasks', 'memories', 'usage']);
    } catch { invalidate(); }
  }, invalidate);
  useEffect(() => {
    turnMetrics.activate();
    const batch = batchTopics(topics => setVersions(previous => {
      const next = { ...previous };
      topics.forEach(topic => { next[topic] = (next[topic] ?? 0) + 1; });
      return next;
    }));
    function changed(topics: string[]) {
      const expanded = new Set(topics);
      if (topics.some(topic => ['admin', 'account', 'grants'].includes(topic))) expanded.add('usage');
      if (topics.some(topic => ['todos', 'schedules', 'tasks', 'plans'].includes(topic))) expanded.add('plans');
      if (topics.some(topic => ['grants', 'resources', 'assistants', 'status', 'tasks'].includes(topic))) expanded.add('capabilities');
      batch.add([...expanded]);
    }
    publish.current = changed;
    const unsubscribe = onMutation(path => {
      if (path === '/api/weixin/poll' || path === '/api/weixin/cancel') return;
      if (path.startsWith('/api/weixin/')) changed(['weixin']);
      else if (path.includes('/conversations/read')) changed(['conversations']);
      else if (path.includes('/conversations/feedback')) changed(['messages']);
      else if (path.includes('/conversations/messages')) changed(['messages', 'conversations', 'status']);
      else if (path.includes('/assistants/')) changed(['assistants', 'conversations', 'status', 'schedules', 'grants']);
      else if (path === '/api/assistants') changed(['assistants', 'conversations']);
      else if (path.startsWith('/api/sandboxes')) changed(['sandboxes', 'resources', 'grants', 'capabilities', 'assistants']);
      else if (path.startsWith('/api/permission-requests')) changed(['permission-requests', 'grants', 'status']);
      else if (path.startsWith('/api/resources')) changed(['resources', 'models', 'grants']);
      else changed([queryTopic(path)]);
    });
    const resume = () => { if (document.visibilityState === 'visible') invalidate(); };
    // Business services currently expose snapshots; IM supplies the live stream.
    // Periodic refresh reconciles changes made by other clients without polling
    // the conversation stream aggressively or inventing execution state.
    const snapshots = setInterval(resume, 30_000);
    const agent = setInterval(() => { if (document.visibilityState === 'visible') changed(['status']); }, 5_000);
    window.addEventListener('online', invalidate); document.addEventListener('visibilitychange', resume);
    return () => { unsubscribe(); publish.current = () => {}; cache.clear(); turnMetrics.clear(); messageChanges.clear(); batch.cancel(); clearInterval(snapshots); clearInterval(agent); window.removeEventListener('online', invalidate); document.removeEventListener('visibilitychange', resume); };
  }, [invalidate, cache, messageChanges]);
  const invalidateTopics = useCallback((topics: string[]) => publish.current(topics), []);
  const version = (topic: string) => (versions.all ?? 0) + (versions[topic] ?? 0);
  const hasConnectionNotice = connection !== 'connected' && connection !== 'recovered';
  return <DataContext.Provider value={{ subscribeEvents, version, invalidate, invalidateTopics, connection, hasConnectionNotice, retryConnection, cache, turnMetrics, messageChanges, invalidateMessage }}>{children}</DataContext.Provider>;
}
export const useData = () => useContext(DataContext);
export const useTopicVersion = (topic: string) => useData().version(topic);
export function useQuery<T>(path: string | null, list = false) {
  const { version, cache } = useData();
  const [attempt, setAttempt] = useState(0);
  const reload = useCallback(() => setAttempt(value => value + 1), []);
  const revision = version(queryTopic(path ?? ''));
  const key = path ? `${list ? 'list' : 'item'}:${path}` : null;
  const cached = key ? cache.get<T>(key) : undefined;
  const [state, setState] = useState<{ key: string | null; data?: T; error?: unknown; loading: boolean }>(() => ({ key, data: cached, loading: !!path }));
  useEffect(() => {
    if (!path || !key) return;
    const controller = new AbortController();
    const order = cache.next();
    setState(previous => ({ key, data: previous.key === key ? previous.data : cache.get<T>(key), error: previous.key === key ? previous.error : undefined, loading: true }));
    const promise = list ? api.all(path, controller.signal) as Promise<T> : api.get<T>(path, controller.signal);
    void promise.then(data => {
      if (!controller.signal.aborted) { cache.set(key, data, order); setState({ key, data, loading: false }); }
    }, error => {
      if (controller.signal.aborted) return;
      const inaccessible = error instanceof APIError && [401, 403, 404].includes(error.status);
      if (inaccessible) cache.delete(key);
      setState(previous => ({ ...previous, data: inaccessible ? undefined : previous.data, error, loading: false }));
    });
    return () => controller.abort();
  }, [path, key, list, revision, attempt, cache]);
  return { data: state.key === key ? state.data : cached, error: state.key === key ? state.error : null, loading: path !== null && (state.key !== key || state.loading), reload };
}
