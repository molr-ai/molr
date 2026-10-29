import type { RuntimeEvent } from './notifications/types';
import { TurnMetricsCache } from './turn-metrics-cache';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import type { ReactNode } from 'react';
import { UIProvider, ToastProvider, useTheme, usePalettePreference, paletteAssetBase } from '@molr/ui';
import type { ConnectionState, UILocale, Palette } from '@molr/ui';
import { api, APIError, clearClientState, csrfToken, onSessionExpired, onMutation } from '@molr/sdk';
import type { Principal } from '@molr/sdk';
import { messages } from './messages';
import { useRuntimeConnection } from './use-runtime-connection';
import { ChatHistoryCache } from './chat-history-cache';
import { QueryStore } from './query-store';
import type { QueryInterval } from './query-store';
import { queryInterval } from './query-policy';
import { poll } from './polling';
import { batchTopics } from './batch-topics';
import { TopicStore } from './topic-store';
import { eventInvalidation, expandTopics, mutationTopics, queryTopic } from './query-invalidation';

const PaletteContext = createContext({ palette: 'forest' as Palette, changePalette: (_: Palette) => {} });
export function useAppearance() { return useContext(PaletteContext); }

const LocaleContext = createContext({ locale: 'zh' as UILocale, setLocale: (_: UILocale) => {} });
export function LocaleProvider({ children }: { children: ReactNode }) {
  const { theme } = useTheme();
  const appearance = usePalettePreference('molr-webui-palette', theme, `${import.meta.env.BASE_URL}brand`);
  const [locale, setLocaleState] = useState<UILocale>(preferredLocale);
  const setLocale = useCallback((value: UILocale) => {
    setLocaleState(value);
    try { document.cookie = `${localeCookieName()}=${value}; Path=/; Max-Age=31536000; SameSite=Lax${location.protocol === 'https:' ? '; Secure' : ''}`; } catch { /* The current page still keeps its preference. */ }
  }, []);
  useEffect(() => {
    const sync = () => setLocaleState(preferredLocale());
    window.addEventListener('languagechange', sync);
    return () => window.removeEventListener('languagechange', sync);
  }, []);
  useEffect(() => { document.documentElement.lang = locale === 'zh' ? 'zh-CN' : 'en'; }, [locale]);
  const value = useMemo(() => ({ locale, setLocale }), [locale, setLocale]);
  return <LocaleContext.Provider value={value}><PaletteContext.Provider value={appearance}><UIProvider locale={locale} brandAssetBase={paletteAssetBase(appearance.palette, `${import.meta.env.BASE_URL}brand`)}><ToastProvider>{children}</ToastProvider></UIProvider></PaletteContext.Provider></LocaleContext.Provider>;
}
function localeCookieName() { return `molr_locale_p${location.port || (location.protocol === 'https:' ? '443' : '80')}`; }
function preferredLocale(): UILocale {
  try {
    const value = document.cookie.split('; ').find(cookie => cookie.startsWith(`${localeCookieName()}=`))?.split('=')[1];
    if (value === 'zh' || value === 'en') return value;
  } catch { /* Browser language remains available without cookies. */ }
  return navigator.language.toLowerCase().startsWith('zh') ? 'zh' : 'en';
}
export function useCopy() { const value = useContext(LocaleContext); return { ...value, t: messages[value.locale] }; }

type SessionState = { principal: Principal | null; loading: boolean; error: unknown; reload: () => Promise<void>; refreshProfile: () => Promise<void>; end: () => void };
const SessionContext = createContext<SessionState | null>(null);
export function SessionProvider({ children }: { children: ReactNode }) {
  const [principal, setPrincipal] = useState<Principal | null>(null), [loading, setLoading] = useState(true), [error, setError] = useState<unknown>(null);
  const channel = useRef<BroadcastChannel | null>(null), revision = useRef(0), foregroundRequest = useRef(false);
  const end = useCallback(() => { revision.current++; clearClientState(); setPrincipal(null); setError(null); setLoading(false); channel.current?.postMessage('changed'); }, []);
  const refresh = useCallback(async (foreground: boolean, announce = false, retryErrors = false) => {
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
      else { if (foreground) setError(error); if (retryErrors) throw error; }
    } finally { if (request === revision.current) { foregroundRequest.current = false; setLoading(false); } }
  }, []);
  const reload = useCallback(() => refresh(true, true), [refresh]);
  const refreshProfile = useCallback(async () => {
    await refresh(false);
    channel.current?.postMessage('profile');
  }, [refresh]);
  useEffect(() => {
    void refresh(true);
    // Cookies are shared between tabs. A changed identity must unmount the old
    // account's data provider before another account's snapshots are displayed.
    try { channel.current = new BroadcastChannel(`molr-session:${location.origin}`); channel.current.onmessage = ({ data }) => {
      if (data === 'profile') { void refresh(false); return; }
      setPrincipal(null); clearClientState(); void refresh(true);
    }; } catch { /* Focus checks also cover browsers without BroadcastChannel. */ }
    const loop = poll(async () => { await refresh(false, false, true); return 30_000; }, false);
    loop.reschedule(30_000);
    const check = () => loop.request();
    window.addEventListener('focus', check);
    const unsubscribe = onSessionExpired(() => { revision.current++; clearClientState(); setPrincipal(null); setError(null); setLoading(false); });
    return () => { revision.current++; unsubscribe(); channel.current?.close(); channel.current = null; loop.stop(); window.removeEventListener('focus', check); };
  }, [refresh, end]);
  return <SessionContext.Provider value={{ principal, loading, error, reload, refreshProfile, end }}>{children}</SessionContext.Provider>;
}
export function useSession() { return useContext(SessionContext)!; }

const DataContext = createContext({ subscribeEvents: (_listener: (event: RuntimeEvent) => void): (() => void) => () => {}, topics: new TopicStore(), invalidate: () => {}, invalidateTopics: (_topics: string[]) => {}, cache: new ChatHistoryCache(), queries: new QueryStore(), turnMetrics: new TurnMetricsCache(), invalidateMessage: (_id: string, _conversationID?: string): number => 0 });
const ConnectionContext = createContext({ connection: 'reconnecting' as ConnectionState, hasConnectionNotice: false, retryConnection: () => {} });
export const useConnection = () => useContext(ConnectionContext);
export function DataProvider({ children }: { children: ReactNode }) {
  const [eventListeners] = useState(() => new Set<(event: RuntimeEvent) => void>());
  const subscribeEvents = useCallback((listener: (event: RuntimeEvent) => void) => { eventListeners.add(listener); return () => { eventListeners.delete(listener); }; }, [eventListeners]);
  const [topics] = useState(() => new TopicStore());
  const [cache] = useState(() => new ChatHistoryCache());
  const [queries] = useState(() => new QueryStore());
  const [turnMetrics] = useState(() => new TurnMetricsCache());
  const publish = useRef<(topics: string[]) => void>(() => {});
  const invalidate = useCallback(() => topics.invalidate(['all']), [topics]);
  const invalidateMessage = useCallback((id: string, conversationID?: string) => { const revision = cache.invalidate(id, conversationID); publish.current([conversationID ? `messages:${conversationID}` : 'messages']); return revision; }, [cache]);
  const { connection, retryConnection } = useRuntimeConnection(event => {
    try {
      const visible = JSON.parse(event.data) as RuntimeEvent;
      for (const listener of eventListeners) listener(visible);
      const change = eventInvalidation(visible);
      if (change.messageID) invalidateMessage(change.messageID, visible.ConversationID);
      if (change.topics.length) publish.current(change.topics);
    } catch { invalidate(); }
  }, invalidate);
  useEffect(() => {
    turnMetrics.activate();
    const batch = batchTopics(changed => topics.invalidate(changed));
    function changed(topics: string[]) { batch.add(expandTopics(topics)); }
    publish.current = changed;
    const unsubscribe = onMutation(path => {
      const topics = mutationTopics(path);
      if (topics.length) changed(topics);
    });

    return () => { unsubscribe(); publish.current = () => {}; cache.clear(); queries.clear(); turnMetrics.clear(); batch.cancel(); };
  }, [invalidate, cache, queries, topics]);
  const invalidateTopics = useCallback((topics: string[]) => publish.current(topics), []);
  const hasConnectionNotice = connection !== 'connected' && connection !== 'recovered';
  const data = useMemo(() => ({ subscribeEvents, topics, invalidate, invalidateTopics, cache, queries, turnMetrics, invalidateMessage }), [subscribeEvents, topics, invalidate, invalidateTopics, cache, queries, turnMetrics, invalidateMessage]);
  const connectionState = useMemo(() => ({ connection, hasConnectionNotice, retryConnection }), [connection, hasConnectionNotice, retryConnection]);
  return <DataContext.Provider value={data}><ConnectionContext.Provider value={connectionState}>{children}</ConnectionContext.Provider></DataContext.Provider>;
}
export const useData = () => useContext(DataContext);
export function useTopicVersion(topic: string) {
  const { topics } = useData();
  const subscribe = useCallback((listener: () => void) => topics.subscribe(topic, listener), [topics, topic]);
  const snapshot = useCallback(() => topics.version(topic), [topics, topic]);
  return useSyncExternalStore(subscribe, snapshot);
}
function useQuerySubscription<T>(path: string | null, list: boolean, interval?: QueryInterval<T>) {
  const { queries, topics } = useData();
  const topic = queryTopic(path ?? '');
  const key = path ? `${list ? 'list' : 'item'}:${path}` : null;
  const cadence = useMemo(() => interval ?? queryInterval(path ?? ''), [interval, path]);
  const subscribe = useCallback((listener: () => void) => key ? queries.subscribe(key, listener, cadence as QueryInterval) : () => {}, [queries, key, cadence]);
  const load = useCallback((signal: AbortSignal, refresh = false) => {
    let target = path!;
    if (refresh) { const [base, query] = target.split('?'); const params = new URLSearchParams(query); params.set('refresh', 'true'); target = `${base}?${params}`; }
    return list ? api.all(target, signal) as Promise<T> : api.get<T>(target, signal);
  }, [path, list]);
  const sync = useCallback(() => {
    if (!key) return;
    const refresh = () => queries.refresh(key, topics.version(topic), load);
    const unsubscribe = topics.subscribe(topic, refresh);
    // Read after subscribing so changes before the effect are also reconciled.
    refresh();
    return unsubscribe;
  }, [queries, topics, topic, key, load]);
  const reload = useCallback(() => { if (key) queries.refresh(key, topics.version(topic), load, true); }, [queries, topics, topic, key, load]);
  return { queries, key, subscribe, reload, sync };
}
export function useQuery<T>(path: string | null, list = false, interval?: QueryInterval<T>) {
  const { queries, key, subscribe, reload, sync } = useQuerySubscription(path, list, interval);
  const snapshot = useCallback(() => queries.snapshot<T>(key), [queries, key]);
  const state = useSyncExternalStore(subscribe, snapshot);
  // Start after useSyncExternalStore has registered the first subscriber.
  useEffect(sync, [sync]);
  return { data: state.data, error: state.error, loading: !!path && (state.loading || !state.started), reload };
}
/** For data-only views. Select a primitive or an existing subtree, not a new object. */
export function useQueryValue<T, S>(path: string | null, select: (data: T | undefined) => S, list = false, interval?: QueryInterval<T>) {
  const { queries, key, subscribe, sync } = useQuerySubscription(path, list, interval);
  const snapshot = useCallback(() => select(queries.snapshot<T>(key).data), [queries, key, select]);
  const value = useSyncExternalStore(subscribe, snapshot);
  useEffect(sync, [sync]);
  return value;
}
