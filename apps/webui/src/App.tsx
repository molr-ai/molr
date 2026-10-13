import { UpdateProvider, UpdateBadge } from './updates/provider';
import { ErrorToastProvider, useErrorToast } from './error-toasts';
import { SetupGate } from './setup';
import { pluginCopy } from './plugins/copy';
import { NotificationProvider } from './notifications/provider';
import { Component, lazy, Suspense, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { AssistantAvatar, Button, ConnectionLogo, ConnectionNotice, ModalScope, Tooltip, UIProvider, useBreakpoint, useTheme } from '@molr/ui';
import { Puzzle, CalendarClock, FlaskConical, Folder, MessagesSquare, Search as SearchIcon } from 'lucide-react';
import { resolveUI, packageKey } from '@molr/ui-host';
import type { UIContext, UIPage } from '@molr/sdk/ui';
import type { Settings, RuntimeInfo, Conversation } from '@molr/sdk';
import { DataProvider, LocaleProvider, SessionProvider, useAppearance, useCopy, useData, useQuery, useSession } from './state';
import { useNavigation } from './navigation';
import { Loading } from './interfaces/official/components/common';
import { Login } from './interfaces/official/features/Login';
import { LoginLinkLogin } from './interfaces/official/features/LoginLinkLogin';
import { useLoginLink } from './login-link';
import { UIHostProvider, useUIHost } from './ui-host/store';
import { hostCopy } from './ui-host/copy';
import './ui-host/host.css';

const OfficialPage = lazy(() => import('./interfaces/official/OfficialPage').then(module => ({ default: module.OfficialPage })));
const TestChatPage = lazy(() => import('./interfaces/test/TestChatPage').then(module => ({ default: module.TestChatPage })));
const AdminSettings = lazy(() => import('./administration/AdminSettings').then(module => ({ default: module.AdminSettings })));
const ExtensionPages = lazy(() => import('./plugins/Plugins').then(module => ({ default: module.ExtensionPages })));
const PluginFrame = lazy(() => import('./plugins/PluginFrame').then(module => ({ default: module.PluginFrame })));
const FramePage = lazy(() => import('./ui-host/FramePage').then(module => ({ default: module.FramePage })));

export function App() { return <LocaleProvider><SessionProvider><ErrorToastScope /></SessionProvider></LocaleProvider>; }
function ErrorToastScope() {
 const { principal } = useSession();
 return <ErrorToastProvider key={principal?.SessionID ?? 'anonymous'}><SessionGate /></ErrorToastProvider>;
}
function SessionGate() {
  const session = useSession(), loginLink = useLoginLink();
  useErrorToast(!loginLink && !session.loading ? session.error : null, () => void session.reload());
  if (loginLink) return <LoginLinkLogin key={loginLink.id} attempt={loginLink} />;
  if (session.loading) return <main className="session-page"><Loading /></main>;
  if (session.error) return <main className="session-page" />;
  if (!session.principal) return <Login />;
  return <DataProvider key={session.principal.SessionID}><SetupGate><UpdateProvider><NotificationProvider><UIHostProvider><WorkspaceHost /></UIHostProvider></NotificationProvider></UpdateProvider></SetupGate></DataProvider>;
}
const icons = { assistants: MessagesSquare, plans: CalendarClock, resources: Folder, extensions: Puzzle, workbench: FlaskConical };
// Trusted in-process adapter. Selection/resolution is identical for partial official and external packages.
const builtinEntries: Record<string, typeof OfficialPage> = { official: OfficialPage, 'test-chat': TestChatPage };
function WorkspaceHost() {
  const { t, locale, setLocale } = useCopy(), copy = hostCopy[locale], { theme, changePreference } = useTheme(), { changePalette } = useAppearance();
  const session = useSession(), administrator = session.principal?.User?.Administrator === true;
  const mobile = useBreakpoint('mobile'), { page, id, navigate } = useNavigation(), host = useUIHost();
  const { connection, hasConnectionNotice, retryConnection } = useData();
  const settings = useQuery<Settings>('/api/settings'), runtime = useQuery<RuntimeInfo>('/api/status');
  const [scope, setScope] = useState<HTMLDivElement | null>(null), [recoveryAttempt, setRecoveryAttempt] = useState(0);
  const conversations = useQuery<Conversation[]>('/api/conversations', true);
  const unread = (conversations.data ?? []).filter(item => !item.Archived).reduce((total, item) => total + Math.max(0, item.UnreadCount), 0);
  const pluginsVisible = settings.data?.PluginsVisible === true;
  useEffect(() => {
    if (settings.data && !pluginsVisible && page === 'extensions') navigate('account', 'general');
  }, [settings.data, pluginsVisible, page, navigate]);
  const workbenchVisible = !!runtime.data?.workbench_available && !!settings.data?.WorkbenchVisible;
  useEffect(() => {
    if (!settings.data) return;
    if (settings.data.Language === 'zh' || settings.data.Language === 'en') setLocale(settings.data.Language);
    changePreference(settings.data.Theme); changePalette(settings.data.Palette || 'forest');
    document.documentElement.dataset.reducedMotion = String(settings.data.ReducedMotion);
    return () => { delete document.documentElement.dataset.reducedMotion; };
  }, [settings.data?.Version, changePreference, changePalette, setLocale]);
  const uiPage: UIPage = page === 'extensions' || page === 'account' ? 'settings' : page === 'todos' || page === 'schedules' ? 'plans' : page;
  const selected = resolveUI(host.configuration, host.packages, uiPage);
  const context = useMemo<UIContext>(() => ({ apiVersion: 1, route: { page: uiPage, id }, locale, theme, palette: settings.data?.Palette || 'forest', reducedMotion: !!settings.data?.ReducedMotion, permissions: selected.manifest.permissions }), [uiPage, id, locale, theme, settings.data?.Palette, settings.data?.ReducedMotion, selected]);
  async function recover() {
    setRecoveryAttempt(value => value + 1);
    // A failed module download is also cached by the browser. Persist the
    // default selection before reloading, so recovery cannot reload the old theme.
    if (await host.reset()) location.reload();
  }
  const mobileNavigation = mobile && !(page === 'assistants' && id);
  const NativePage = builtinEntries[selected.manifest.pages[uiPage]!];
  if (host.loading) return <main className="session-page"><Loading /></main>;
  return <UIProvider locale={locale} reducedMotion={settings.data?.ReducedMotion}><div ref={setScope} className={`workspace${mobile ? ' workspace--mobile' : ''}${mobileNavigation ? ' workspace--mobile-nav' : ''}`}><ModalScope container={scope} enabled={mobile}>
    <nav className="workspace-nav" aria-label={t.app} aria-hidden={mobile && !mobileNavigation || undefined} inert={mobile && !mobileNavigation}>
      {!mobile && <ConnectionLogo markOnly className="workspace-logo" state={connection} onRetry={retryConnection} tooltipSide="right" variant={theme === 'dark' ? 'light' : 'dark'} />}
      {!mobile && selected.runtime === 'builtin' && <Tooltip content={t.search} side="right" width="auto"><button type="button" className="nav-button nav-search" aria-label={t.search} aria-haspopup="dialog" onClick={() => window.dispatchEvent(new Event('molr:search'))}><span className="nav-icon"><SearchIcon size={22} /></span></button></Tooltip>}
      {(Object.keys(icons) as (keyof typeof icons)[]).filter(key => (key !== 'resources' || administrator) && (key !== 'workbench' || workbenchVisible) && (key !== 'extensions' || pluginsVisible)).map(key => { const Icon = icons[key], label = key === 'extensions' ? pluginCopy[locale].extensions : t.nav[key]; return <Tooltip key={key} content={label} side={mobile ? 'top' : 'right'} width="auto"><button className="nav-button" type="button" aria-label={label} aria-current={page === key ? 'page' : undefined} aria-describedby={key === 'assistants' && unread > 0 ? 'nav-unread-count' : undefined} onClick={() => navigate(key)}><span className="nav-icon"><Icon size={22} />{key === 'assistants' && unread > 0 && <span className="nav-unread" id="nav-unread-count" role="status" aria-label={t.assistants.unread(unread)}>{unread > 99 ? '99+' : unread}</span>}</span><span>{key === 'extensions' ? pluginCopy[locale].nav : t.nav[key]}</span></button></Tooltip>; })}
      {!mobile && <div className="nav-account-actions">
        <Tooltip content={t.account.title} side="right" width="auto"><button type="button" className="nav-button" aria-label={t.account.title} aria-current={page === 'account' || page === 'settings' ? 'page' : undefined} onClick={() => navigate('account')}><span className="user-avatar" aria-hidden="true"><AssistantAvatar name={session.principal?.User?.DisplayName || session.principal?.User?.Username || 'U'} avatar={session.principal?.User?.Avatar} /></span><span>{locale === 'zh' ? '我的' : 'Account'}</span><UpdateBadge compact /></button></Tooltip>
      </div>}
    </nav>
    <div className="workspace-content">
      {hasConnectionNotice && <ConnectionNotice connection={connection} onRetry={retryConnection} />}
    <main className="workspace-main" data-page={page}>
      <PageBoundary key={`${packageKey(selected.manifest)}:${recoveryAttempt}`} routeKey={`${uiPage}:${id}`} fallback={<div className="ui-frame-notice" role="alert"><h2>{copy.failed}</h2><p>{copy.failedHelp}</p><Button onClick={() => location.reload()}>{copy.retry}</Button><Button onClick={recover}>{copy.recover}</Button><Button variant="secondary" onClick={() => navigate('account', 'theme')}>{copy.manage}</Button></div>}>
        <Suspense fallback={<Loading />}>{page === 'settings' ? <AdminSettings initialTab={id} onSelectSection={value => navigate('settings', value)} /> : page === 'resources' && !administrator ? <div className="ui-frame-notice" role="alert">{locale === 'zh' ? '此页面仅管理员可访问' : 'Administrator access required'}</div> : page === 'account' ? <OfficialPage /> : page === 'extensions' ? (pluginsVisible ? <ExtensionPages id={id} /> : null) : selected.plugin ? <PluginFrame reference={{ ...selected.plugin, Page: uiPage }} title={selected.manifest.name} /> : selected.runtime === 'builtin' ? <NativePage /> : <FramePage manifest={selected.manifest} context={context} navigate={navigate} recover={recover} />}</Suspense>
      </PageBoundary>
    </main>
    </div>
  </ModalScope></div></UIProvider>;
}
class PageBoundary extends Component<{ children: ReactNode; fallback: ReactNode; routeKey: string }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidUpdate(previous: { routeKey: string }) { if (this.state.failed && previous.routeKey !== this.props.routeKey) this.setState({ failed: false }); }
  render() { return this.state.failed ? this.props.fallback : this.props.children; }
}
