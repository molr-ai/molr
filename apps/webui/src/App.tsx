import { DraftProvider } from './drafts';
import { AssistantNavigationItem } from './AssistantNavigationItem';
import { AppFrame, AppTitlebarBrand } from './app-mode';
import { AppAppearanceSync } from './app-appearance';
import { UpdateProvider, UpdateBadge, useUpdateNotice } from './updates/provider';
import { ErrorToastProvider, useErrorToast } from './error-toasts';
import { SetupGate } from './setup';
import { pluginCopy } from './plugins/copy';
import { NotificationProvider } from './notifications/provider';
import { Component, lazy, Suspense, useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { Avatar, Button, ConnectionLogo, ConnectionNotice, ModalScope, Tooltip, UIProvider, useBreakpoint, useTheme } from '@molr/ui';
import { Puzzle, CalendarClock, FlaskConical, Folder, MessagesSquare, Search as SearchIcon } from 'lucide-react';
import { resolveUI, packageKey } from '@molr/ui-host';
import type { UIPage } from '@molr/sdk/ui';
import type { Settings, RuntimeInfo } from '@molr/sdk';
import { DataProvider, LocaleProvider, SessionProvider, useAppearance, useCopy, useConnection, useQuery, useSession } from './state';
import { useNavigation } from './navigation';
import { Loading } from './interfaces/official/components/common';
import { Login } from './interfaces/official/features/Login';
import { LoginLinkLogin } from './interfaces/official/features/LoginLinkLogin';
import { useLoginLink } from './login-link';
import { UIHostProvider, useUIHost } from './ui-host/store';
import { hostCopy } from './ui-host/copy';
import './ui-host/host.css';

const OfficialPage = lazy(() => import('./interfaces/official/OfficialPage').then(module => ({ default: module.OfficialPage })));
const AdminSettings = lazy(() => import('./administration/AdminSettings').then(module => ({ default: module.AdminSettings })));
const ExtensionPages = lazy(() => import('./plugins/Plugins').then(module => ({ default: module.ExtensionPages })));
const PluginFrame = lazy(() => import('./plugins/PluginFrame').then(module => ({ default: module.PluginFrame })));

export function App() { return <LocaleProvider><AppFrame><SessionProvider><ErrorToastScope /></SessionProvider></AppFrame></LocaleProvider>; }
function ErrorToastScope() {
 const { principal } = useSession();
 return <ErrorToastProvider key={principal?.SessionID ?? 'anonymous'}><SessionGate /></ErrorToastProvider>;
}
function SessionGate() {
  const session = useSession(), loginLink = useLoginLink();
  const [setupRevision, setSetupRevision] = useState(0);
  useErrorToast(!loginLink && !session.loading ? session.error : null, () => void session.reload());
  if (loginLink) return <LoginLinkLogin key={loginLink.id} attempt={loginLink} />;
  if (session.loading) return <main className="session-page"><Loading /></main>;
  if (session.error) return <main className="session-page" />;
  if (!session.principal) return <Login />;
  // Setup creates account preferences and an assistant; discard all pre-setup snapshots.
  return <DataProvider key={`${session.principal.SessionID}:${setupRevision}`}>{window.molrApp?.setAppearance && <AppAppearanceSync />}<SetupGate onComplete={() => setSetupRevision(value => value + 1)}><UpdateProvider><NotificationProvider><DraftProvider key={session.principal.OwnerID} owner={session.principal.OwnerID}><UIHostProvider><WorkspaceHost /></UIHostProvider></DraftProvider></NotificationProvider></UpdateProvider></SetupGate></DataProvider>;
}
const icons = { assistants: MessagesSquare, plans: CalendarClock, resources: Folder, extensions: Puzzle, workbench: FlaskConical };
// Trusted in-process adapter. Selection/resolution is identical for partial official and external packages.
const builtinEntries: Record<string, typeof OfficialPage> = { official: OfficialPage };
function WorkspaceModals({ container, mobile, children }: { container: HTMLElement | null; mobile: boolean; children: ReactNode }) {
  // App mode owns one modal scope around the whole window, including its titlebar.
  return window.molrApp ? children : <ModalScope container={container} enabled={mobile}>{children}</ModalScope>;
}
function WorkspaceHost() {
  const updateNotice = useUpdateNotice();
  const { t, locale, setLocale } = useCopy(), copy = hostCopy[locale], { theme, changePreference } = useTheme(), { changePalette } = useAppearance();
  const session = useSession(), administrator = session.principal?.User?.Administrator === true;
  const mobile = useBreakpoint('mobile'), { page, id, navigate } = useNavigation(), host = useUIHost();
  const { connection, hasConnectionNotice, retryConnection } = useConnection();
  const settings = useQuery<Settings>('/api/settings'), runtime = useQuery<RuntimeInfo>('/api/status');
  const [scope, setScope] = useState<HTMLDivElement | null>(null), [recoveryAttempt, setRecoveryAttempt] = useState(0);
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
  async function recover() {
    setRecoveryAttempt(value => value + 1);
    // A failed module download is also cached by the browser. Persist the
    // default selection before reloading, so recovery cannot reload the old theme.
    if (await host.reset()) location.reload();
  }
  const mobileNavigation = mobile && !(page === 'assistants' && id);
  const NativePage = builtinEntries[selected.manifest.pages[uiPage]!];
  if (host.loading) return <main className="session-page"><Loading /></main>;
  return <UIProvider locale={locale} reducedMotion={settings.data?.ReducedMotion}><div ref={setScope} className={`workspace${mobile ? ' workspace--mobile' : ''}${mobileNavigation ? ' workspace--mobile-nav' : ''}`}><WorkspaceModals container={scope} mobile={mobile}>
    {window.molrApp && <AppTitlebarBrand><ConnectionLogo state={connection} onRetry={retryConnection} tooltipSide="bottom" variant={theme === 'dark' ? 'light' : 'dark'} /></AppTitlebarBrand>}
    <nav className="workspace-nav" aria-label={t.app} aria-hidden={mobile && !mobileNavigation || undefined} inert={mobile && !mobileNavigation}>
      {!mobile && !window.molrApp && <ConnectionLogo markOnly className="workspace-logo" state={connection} onRetry={retryConnection} tooltipSide="right" variant={theme === 'dark' ? 'light' : 'dark'} />}
      {!mobile && selected.runtime === 'builtin' && <Tooltip content={t.search} side="right" width="auto"><button type="button" className="nav-button nav-search" aria-label={t.search} aria-haspopup="dialog" onClick={() => window.dispatchEvent(new Event('molr:search'))}><span className="nav-icon"><SearchIcon size={22} /></span></button></Tooltip>}
      {(Object.keys(icons) as (keyof typeof icons)[]).filter(key => (key !== 'resources' || administrator) && (key !== 'workbench' || workbenchVisible) && (key !== 'extensions' || pluginsVisible)).map(key => { if (key === 'assistants') return <AssistantNavigationItem key={key} active={page === key} mobile={mobile} onNavigate={() => navigate(key)} />; const Icon = icons[key], label = key === 'extensions' ? pluginCopy[locale].extensions : t.nav[key]; return <Tooltip key={key} content={label} side={mobile ? 'top' : 'right'} width="auto"><button className="nav-button" type="button" aria-label={label} aria-current={page === key ? 'page' : undefined} onClick={() => navigate(key)}><span className="nav-icon"><Icon size={22} /></span><span>{key === 'extensions' ? pluginCopy[locale].nav : t.nav[key]}</span></button></Tooltip>; })}
      {!mobile && <div className="nav-account-actions">
        <Tooltip content={t.account.title} side="right" width="auto"><button type="button" className="nav-button" aria-label={t.account.title} aria-current={page === 'account' || page === 'settings' ? 'page' : undefined} onClick={() => navigate('account', updateNotice ? 'about' : undefined)}><Avatar name={session.principal?.User?.DisplayName || session.principal?.User?.Username || 'U'} avatar={session.principal?.User?.Avatar} selected={page === 'account' || page === 'settings'} notice={<UpdateBadge compact />} /><span>{locale === 'zh' ? '我的' : 'Account'}</span></button></Tooltip>
      </div>}
    </nav>
    <div className="workspace-content">
      {hasConnectionNotice && <ConnectionNotice connection={connection} onRetry={retryConnection} />}
    <main className="workspace-main" data-page={page}>
      <PageBoundary key={`${packageKey(selected.manifest)}:${recoveryAttempt}`} routeKey={`${uiPage}:${id}`} fallback={<div className="ui-frame-notice" role="alert"><h2>{copy.failed}</h2><p>{copy.failedHelp}</p><Button onClick={() => location.reload()}>{copy.retry}</Button><Button onClick={recover}>{copy.recover}</Button><Button variant="secondary" onClick={() => navigate('account', 'theme')}>{copy.manage}</Button></div>}>
        <Suspense fallback={<Loading />}>{page === 'settings' ? <AdminSettings initialTab={id} onSelectSection={value => navigate('settings', value)} /> : page === 'resources' && !administrator ? <div className="ui-frame-notice" role="alert">{locale === 'zh' ? '此页面仅管理员可访问' : 'Administrator access required'}</div> : page === 'account' ? <OfficialPage /> : page === 'extensions' ? (pluginsVisible ? <ExtensionPages id={id} /> : null) : selected.plugin ? <PluginFrame reference={{ ...selected.plugin, Page: uiPage }} title={selected.manifest.name} /> : <NativePage />}</Suspense>
      </PageBoundary>
    </main>
    </div>
  </WorkspaceModals></div></UIProvider>;
}
class PageBoundary extends Component<{ children: ReactNode; fallback: ReactNode; routeKey: string }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidUpdate(previous: { routeKey: string }) { if (this.state.failed && previous.routeKey !== this.props.routeKey) this.setState({ failed: false }); }
  render() { return this.state.failed ? this.props.fallback : this.props.children; }
}
