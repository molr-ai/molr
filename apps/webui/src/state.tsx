import type { RuntimeEvent } from './notifications/types';
import { TurnMetricsCache } from './turn-metrics-cache';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import type { ReactNode } from 'react';
import { UIProvider, ToastProvider, useTheme, usePalettePreference, paletteAssetBase } from '@molr/ui';
import type { ConnectionState, UILocale, Palette } from '@molr/ui';
import { api, APIError, clearClientState, csrfToken, onSessionExpired, onMutation } from './api/client';
import type { Principal } from './api/types';
import { messages } from './messages';
import { useRuntimeConnection } from './use-runtime-connection';
import { QueryCache } from './query-cache';
import { QueryStore } from './query-store';
import type { QueryInterval } from './query-store';
import { queryInterval } from './query-policy';
import { poll } from './polling';
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

function queryTopic(path: string) {
  if (path === '/api/status') return 'runtime';
  const shell = path.match(/^\/api\/assistants\/([^/]+)\/shell-tasks/);
  if (shell) return `shell-tasks:${shell[1]}`;
  if (/\/assistants\/[^/]+\/feishu/.test(path)) return 'feishu';
  if (/\/assistants\/[^/]+\/weixin/.test(path)) return 'weixin';
  if (/\/assistants\/[^/]+\/dingtalk/.test(path)) return 'dingtalk';
  if (/\/assistants\/[^/]+\/slack/.test(path)) return 'slack';
  if (/\/assistants\/[^/]+\/telegram/.test(path)) return 'telegram';
  if (path === '/api/account/quotas' || path === '/api/admin/usage' || path.startsWith('/api/account/usage?') || path.startsWith('/api/admin/usage/analytics?')) return 'usage';
  if (/\/debug\/runs\/[^/]+\/steps\//.test(path)) return 'debug-payload';
  const assistant = path.match(/^\/api\/assistants\/([^/]+)\/(capabilities|status)$/);
  if (assistant) return `${assistant[2]}:${assistant[1]}`;
  const conversation = path.match(/^\/api\/conversations\/([^/]+)\/messages/);
  if (conversation) return `messages:${conversation[1]}`;
  if (path.startsWith('/api/assistants/activity')) return 'activity';
  return path.split('/')[2]?.split('?')[0] ?? '';
}
const DataContext = createContext({ subscribeEvents: (_listener: (event: RuntimeEvent) => void): (() => void) => () => {}, version: (_topic: string): number => 0, invalidate: () => {}, invalidateTopics: (_topics: string[]) => {}, connection: 'reconnecting' as ConnectionState, hasConnectionNotice: false, retryConnection: () => {}, cache: new QueryCache(), queries: new QueryStore(), turnMetrics: new TurnMetricsCache(), messageChanges: new Map<string, number>(), invalidateMessage: (_id: string, _conversationID?: string): number => 0 });
export function DataProvider({ children }: { children: ReactNode }) {
  const [eventListeners] = useState(() => new Set<(event: RuntimeEvent) => void>());
  const subscribeEvents = useCallback((listener: (event: RuntimeEvent) => void) => { eventListeners.add(listener); return () => { eventListeners.delete(listener); }; }, [eventListeners]);
  const [versions, setVersions] = useState<Record<string, number>>({});
  const [cache] = useState(() => new QueryCache());
  const [queries] = useState(() => new QueryStore());
  const [turnMetrics] = useState(() => new TurnMetricsCache());
  const [messageChanges] = useState(() => new Map<string, number>());
  const publish = useRef<(topics: string[]) => void>(() => {});
  const invalidate = useCallback(() => setVersions(previous => ({ ...previous, all: (previous.all ?? 0) + 1 })), []);
  const invalidateMessage = useCallback((id: string, conversationID?: string) => { const revision = cache.next(); messageChanges.set(id, revision); publish.current([conversationID ? `messages:${conversationID}` : 'messages']); return revision; }, [cache, messageChanges]);
  const { connection, retryConnection } = useRuntimeConnection(event => {
    try {
      const visible = JSON.parse(event.data) as RuntimeEvent;
      for (const listener of eventListeners) listener(visible);
      if (visible.Topic === 'shell.tasks' && visible.Object?.ID) publish.current([`shell-tasks:${visible.Object.ID}`]);
      else if (visible.Topic === 'notification.changed') publish.current(['notifications']);
      else if (visible.Topic === 'feishu.changed') publish.current(['feishu']);
      else if (visible.Topic === 'weixin.changed') publish.current(['weixin']);
      else if (visible.Topic === 'dingtalk.changed') publish.current(['dingtalk']);
      else if (visible.Topic === 'slack.changed') publish.current(['slack']);
      else if (visible.Topic === 'telegram.changed') publish.current(['telegram']);
      else if (visible.Topic === 'assistant.profile') publish.current(['assistants', 'conversations']);
      else if (visible.Topic.startsWith('assistant.') && visible.Object?.ID) publish.current([`status:${visible.Object.ID}`, 'activity', 'tasks', 'dreams', 'memories', 'usage']);
      else if (visible.Topic === 'permission.request') publish.current(['permission-requests', 'grants', 'status']);
      else if (visible.Topic === 'conversation.read') publish.current(['conversations']);
      else if (['message.feedback', 'message.accepted', 'message.read'].includes(visible.Topic) && visible.Object?.ID) {
        invalidateMessage(visible.Object.ID, visible.ConversationID);
        if (visible.Topic === 'message.accepted' || visible.Topic === 'message.read') publish.current(['status', 'conversations', 'tasks', 'memories', 'usage']);
      }
      else publish.current([visible.ConversationID ? `messages:${visible.ConversationID}` : 'messages', 'activity', 'conversations', 'status', 'tasks', 'memories', 'usage']);
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
      for (const topic of topics) if (topic.startsWith('status:')) expanded.add('activity');
      if (topics.some(topic => ['assistants', 'status', 'messages'].includes(topic) || topic.startsWith('messages:'))) expanded.add('activity');
      if (topics.some(topic => ['admin', 'account', 'grants'].includes(topic))) expanded.add('usage');
      if (topics.some(topic => ['todos', 'schedules', 'tasks', 'plans'].includes(topic))) expanded.add('plans');
      if (topics.some(topic => ['grants', 'resources', 'assistants', 'sandboxes'].includes(topic))) expanded.add('capabilities');
      batch.add([...expanded]);
    }
    publish.current = changed;
    const unsubscribe = onMutation(path => {
      if (path === '/api/updates/check') return; // UpdateProvider refreshes its snapshot after a check.
      if (['/api/weixin/poll', '/api/weixin/cancel', '/api/telegram/poll', '/api/telegram/cancel'].includes(path)) return;
      if (path.startsWith('/api/weixin/')) changed(['weixin']);
      else if (path.startsWith('/api/dingtalk/')) changed(['dingtalk']);
      else if (path.startsWith('/api/slack/')) changed(['slack']);
      else if (path.startsWith('/api/telegram/')) changed(['telegram']);
      else if (path.includes('/conversations/read')) changed(['conversations']);
      else if (path.includes('/conversations/feedback')) return; // Message feedback reconciles its exact conversation.
      else if (path.includes('/conversations/messages')) changed(['conversations', 'status']); // The caller and SSE invalidate the affected conversation.
      else if (/\/assistants\/[^/]+\/shell-tasks$/.test(path)) changed([queryTopic(path)]);
      // Browser controls reconcile their own status; pointer/keyboard input must
      // not invalidate unrelated assistant, conversation, schedule or grant data.
      else if (/\/assistants\/[^/]+\/browser$/.test(path)) return;
      else if (path.includes('/assistants/')) changed(['assistants', 'conversations', 'status', 'schedules', 'grants']);
      else if (path === '/api/assistants') changed(['assistants', 'conversations']);
      else if (path.startsWith('/api/sandboxes')) changed(['sandboxes', 'resources', 'grants', 'capabilities', 'assistants']);
      else if (path.startsWith('/api/permission-requests')) changed(['permission-requests', 'grants', 'status']);
      else if (path.startsWith('/api/mcp/')) changed(['mcp', 'tools', 'resources', 'grants', 'capabilities', 'admin', 'usage']);
      else if (path.startsWith('/api/resources')) changed(['resources', 'models', 'grants']);
      else changed([queryTopic(path)]);
    });

    return () => { unsubscribe(); publish.current = () => {}; cache.clear(); queries.clear(); turnMetrics.clear(); messageChanges.clear(); batch.cancel(); };
  }, [invalidate, cache, queries, messageChanges]);
  const invalidateTopics = useCallback((topics: string[]) => publish.current(topics), []);
  const version = (topic: string) => (versions.all ?? 0) + (versions[topic] ?? 0) + (topic.includes(':') ? versions[topic.split(':')[0]] ?? 0 : 0);
  const hasConnectionNotice = connection !== 'connected' && connection !== 'recovered';
  return <DataContext.Provider value={{ subscribeEvents, version, invalidate, invalidateTopics, connection, hasConnectionNotice, retryConnection, cache, queries, turnMetrics, messageChanges, invalidateMessage }}>{children}</DataContext.Provider>;
}
export const useData = () => useContext(DataContext);
export const useTopicVersion = (topic: string) => useData().version(topic);
export function useQuery<T>(path: string | null, list = false, interval?: QueryInterval<T>) {
  const { version, queries } = useData();
  const revision = version(queryTopic(path ?? ''));
  const key = path ? `${list ? 'list' : 'item'}:${path}` : null;
  const cadence = useMemo(() => interval ?? queryInterval(path ?? ''), [interval, path]);
  const subscribe = useCallback((listener: () => void) => key ? queries.subscribe(key, listener, cadence as QueryInterval) : () => {}, [queries, key, cadence]);
  const snapshot = useCallback(() => queries.snapshot<T>(key), [queries, key]);
  const state = useSyncExternalStore(subscribe, snapshot);
  const load = useCallback((signal: AbortSignal) => list ? api.all(path!, signal) as Promise<T> : api.get<T>(path!, signal), [path, list]);
  useEffect(() => { if (key) queries.refresh(key, revision, load); }, [queries, key, revision, load]);
  const reload = useCallback(() => { if (key) queries.refresh(key, revision, load, true); }, [queries, key, revision, load]);
  return { data: state.data, error: state.error, loading: !!path && (state.loading || !state.started), reload };
}
