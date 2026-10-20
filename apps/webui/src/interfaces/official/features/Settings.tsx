import { useErrorToast } from '../../../error-toasts';
import { AboutService, VersionEntry } from '../../../updates/AboutUpdates';
import { UpdateBadge } from '../../../updates/provider';
import { ProfileSettings } from '../../../administration/UserSettings';
import { NotificationSettings } from '../../../notifications/Settings';
import { notificationCopy } from '../../../notifications/copy';
import { Memories } from './Memories';
import { AccountUsage } from '../../../administration/Usage';
import { Activity, useEffect, useState } from 'react';
import { SlidingHighlight, ModalPresence, Badge, Button, Checkbox, IconButton, Logo, TextField, useBreakpoint, useTheme, useUILocale } from '@molr/ui';
import { Bell, Brain, ChartNoAxesCombined, ChevronLeft, ChevronRight, Database, Download, Info, Palette as PaletteIcon, Settings as SettingsIcon, Settings2, Shield, Star, ArrowUpRight, UserRound, UserRoundX } from 'lucide-react';
import { api } from '../../../api/client';
import type { Assistant, Session } from '../../../api/types';
import { useReauthentication } from '../components/ReauthenticationDialog';
import { useCopy, useQuery, useSession } from '../../../state';
import { useNavigation } from '../../../navigation';
import { Attachment } from './Chat';
import { LoginLinksSettings } from './LoginLinksSettings';
import { SecurityVerificationSettings } from './SecurityVerificationSettings';
import { GeneralSettings } from './GeneralSettings';
import { ThemeSettings } from './ThemeSettings';
import { Confirm, EditDialog, Empty, Loading, PageHeader, useAction } from '../components/common';
import './settings.css';

const settingsTabs = ['general', 'profile', 'security', 'notifications', 'theme', 'usage', 'memory', 'data', 'former', 'about'];
export function Settings({ assistants, openAssistant, initialTab = '', onSelectSection }: { assistants: Assistant[]; initialTab?: string; openAssistant: (id: string) => void; onSelectSection?: (value: string) => void }) {
  const { t, locale } = useCopy(), mobile = useBreakpoint('mobile');
  const { principal } = useSession(), { navigate } = useNavigation();
  const routed = settingsTabs.includes(initialTab) ? initialTab : '';
  const active = routed || (mobile ? '' : 'general');
  const [visited, setVisited] = useState(() => new Set(active ? [active] : []));
  useEffect(() => { if (active) setVisited(previous => new Set([...previous, active])); }, [active]);
  const sections = [
    { value: 'general', label: t.settings.general, icon: Settings2, content: <GeneralSettings /> },
    { value: 'profile', label: locale === 'zh' ? '个人资料' : 'Profile', icon: UserRound, content: <ProfileSettings /> },
    { value: 'security', label: t.settings.security, icon: Shield, content: <Security /> },
    { value: 'notifications', label: notificationCopy[locale].title, icon: Bell, content: <NotificationSettings /> },
    { value: 'theme', label: t.settings.theme, icon: PaletteIcon, description: t.settings.themeDescription, content: <ThemeSettings /> },
    { value: 'usage', label: t.settings.usage, icon: ChartNoAxesCombined, content: <AccountUsage /> },
    { value: 'memory', label: t.settings.memory, icon: Brain, content: <Memories assistants={assistants} /> },
    { value: 'data', label: t.data, icon: Database, content: <DataExport /> },
    { value: 'former', label: t.assistants.former, icon: UserRoundX, content: <FormerAssistants assistants={assistants} openAssistant={openAssistant} /> },
    { value: 'about', label: t.account.about, icon: Info, content: <About /> },
  ];
  const current = sections.find(item => item.value === active);
  const select = (value: string) => onSelectSection?.(value);
  return <section className="settings-workspace">
    <PageHeader className="settings-header" title={mobile && current ? current.label : locale === 'zh' ? '用户设置' : 'User settings'} icon={<SettingsIcon />} leading={mobile && current && onSelectSection ? <IconButton label={t.back} onClick={() => select('')}><ChevronLeft size={18} /></IconButton> : undefined} actions={principal?.User?.Administrator && <Button className="settings-admin-button" variant="secondary" size="sm" onClick={() => navigate('settings')}><Shield size={16} aria-hidden="true" />{t.account.admin}</Button>} />
    <div className="settings-body">
      <SlidingHighlight as="nav" value={active} disabled={mobile} className="settings-nav" aria-label={locale === 'zh' ? '用户设置' : 'User settings'} hidden={mobile && !!current}>
        {sections.map(item => {
          const Icon = item.icon;
          return <button key={item.value} data-sliding-item={item.value} type="button" aria-current={active === item.value ? 'page' : undefined} onClick={() => select(item.value)}>
            <Icon size={16} className="settings-nav-icon" aria-hidden="true" /><span>{item.label}</span>{item.value === 'about' && <UpdateBadge />}<ChevronRight size={16} className="settings-nav-caret" aria-hidden="true" />
          </button>;
        })}
      </SlidingHighlight>
      <div className="settings-panel" hidden={mobile && !current}>
        {sections.filter(item => visited.has(item.value)).map(item => <div key={item.value} className="settings-pane" hidden={item.value !== active}>
          <Activity mode={item.value === active ? 'visible' : 'hidden'}>
            <header className="settings-pane-header"><h2>{item.label}</h2>{item.description && <p className="settings-description">{item.description}</p>}</header>
            {item.content}
          </Activity>
        </div>)}
      </div>
    </div>
  </section>;
}
function About() {
  const { t, locale } = useCopy(), { theme } = useTheme(), { reducedMotion } = useUILocale();
  const repository = 'https://github.com/molr-ai/molr';
  return <div className="settings-section settings-about">
    <div className="settings-about-brand">
      <Logo animation={reducedMotion ? 'static' : 'daily'} interactive variant={theme === 'dark' ? 'light' : 'dark'} />
      <VersionEntry />
    </div>
    <AboutService />
    <footer className="settings-about-footer">
      <div><h3>{locale === 'zh' ? '一起让 Molr 更好' : 'Help Molr grow'}</h3><p>{locale === 'zh' ? '如果喜欢 Molr，欢迎在 GitHub 上点亮一颗 Star。' : 'Enjoying Molr? Give the project a Star on GitHub.'}</p></div>
      <a className="settings-about-github" href={repository} target="_blank" rel="noopener noreferrer" aria-label={t.account.starLink}><Star size={15} aria-hidden="true" />GitHub<ArrowUpRight size={14} aria-hidden="true" /></a>
    </footer>
  </div>;
}
function FormerAssistants({ assistants, openAssistant }: { assistants: Assistant[]; openAssistant: (id: string) => void }) {
  const { t } = useCopy(), [rehire, setRehire] = useState<Assistant | null>(null); const former = assistants.filter(a => a.State === 'dismissed');
  return <>{!former.length ? <Empty /> : <div className="settings-rows">{former.map(row => <article className="settings-row-item" key={row.ID}>
    <div className="settings-row-item-head"><h3>{row.Name}</h3><Badge>{t.assistants.archived}</Badge><span className="settings-row-item-actions"><Button size="sm" variant="secondary" onClick={() => openAssistant(row.ID)}>{t.chat.message}</Button><Button size="sm" onClick={() => setRehire(row)}>{t.assistants.rehire}</Button></span></div>
  </article>)}</div>}
    <ModalPresence>{rehire && <Confirm title={t.assistants.rehire} description={t.assistants.rehireHelp} onClose={() => setRehire(null)} onConfirm={() => api.mutate('/api/assistants/rehire', { ID: rehire.ID, RestoreGrantIDs: [], RestoreScheduleIDs: [] }, rehire.Version)} />}</ModalPresence>
  </>;
}
function sessionDeviceName(session: Session) {
  const types: Record<string, string> = { web: 'Web', ios: 'iOS', android: 'Android', desktop: 'Desktop', cli: 'CLI' };
  return session.ClientType ? `${session.DeviceName} · ${types[session.ClientType] ?? session.ClientType}` : session.DeviceName;
}
function Security() {
  const { t, locale } = useCopy(), session = useSession(), query = useQuery<Session[]>('/api/auth/sessions', true), action = useAction();
  const [revokeOthers, setRevokeOthers] = useState(false);
  const [revoke, setRevoke] = useState<Session | null>(null), [password, setPassword] = useState(false);
  const devices = (query.data ?? []).slice().sort((a, b) => Number(b.ID === session.principal?.SessionID) - Number(a.ID === session.principal?.SessionID) || Date.parse(b.LastSeenAt) - Date.parse(a.LastSeenAt));
  useErrorToast(query.error, query.reload);
  useErrorToast(action.error);
  return <div className="settings-section settings-security">
    <SecurityVerificationSettings onChangePassword={() => setPassword(true)} />
    <section className="settings-group security-devices" aria-label={t.settings.sessions}>
      <header className="security-section-heading"><h3 className="settings-group-title">{t.settings.sessions}</h3><Button size="sm" variant="secondary" disabled={!devices.some(row => row.ID !== session.principal?.SessionID)} onClick={() => setRevokeOthers(true)}>{t.settings.revokeOthers}</Button></header>
      {query.loading && !query.data ? <Loading /> : <div className="settings-rows">{devices.map(row => <article className="settings-row-item" key={row.ID}>
        <div className="settings-row-item-head"><div className="security-record-title"><h4>{sessionDeviceName(row)}</h4>{row.ID === session.principal?.SessionID && <Badge>{t.settings.current}</Badge>}</div><span className="settings-row-item-actions"><Button size="sm" variant="ghost" onClick={() => setRevoke(row)}>{t.settings.revokeSession}</Button></span></div>
        <dl className="security-record-meta">
          <div><dt>{t.settings.lastSeen}</dt><dd><time dateTime={row.LastSeenAt}>{new Date(row.LastSeenAt).toLocaleString(locale)}</time></dd></div>
          <div><dt>{t.settings.lastLogin}</dt><dd><time dateTime={row.LastLoginAt || row.CreatedAt}>{new Date(row.LastLoginAt || row.CreatedAt).toLocaleString(locale)}</time></dd></div>
        </dl>
      </article>)}</div>}
    </section>
    <LoginLinksSettings />
    <footer className="security-sign-out security-section-heading"><p className="security-description">{t.settings.signOutHelp}</p><Button size="sm" variant="secondary" disabled={action.busy} onClick={() => void action.run(() => api.post('/api/auth/logout'), session.end, false)}>{t.signOut}</Button></footer>
    <ModalPresence>{revoke && <Confirm title={t.settings.revokeSession} description={t.settings.revokeSessionHelp} onClose={() => setRevoke(null)} onConfirm={async () => { await api.remove(`/api/auth/sessions/${revoke.ID}`); if (revoke.ID === session.principal?.SessionID) session.end(); else query.reload(); }} />}</ModalPresence>
    <ModalPresence>{revokeOthers && <Confirm title={t.settings.revokeOthers} description={t.settings.revokeOthersHelp} onClose={() => setRevokeOthers(false)} onConfirm={async () => { await api.post('/api/auth/sessions/revoke-others'); query.reload(); }} />}</ModalPresence>
    <ModalPresence>{password && <PasswordEditor onClose={() => setPassword(false)} />}</ModalPresence>
  </div>;
}
function PasswordEditor({ onClose }: { onClose: () => void }) {
  const { t } = useCopy(), { end } = useSession(), [next, setNext] = useState('');
  const verification = useReauthentication();
  return <><EditDialog title={t.settings.changePassword} onClose={onClose} onSubmit={async () => { const proof = await verification.verify('password.change'); await api.post('/api/auth/password', { verification: proof, new_password: next }); end(); }}>
    <p className="muted">{t.settings.passwordHelp}</p><TextField label={t.settings.newPassword} type="password" name="new-password" autoComplete="new-password" value={next} onChange={event => setNext(event.target.value)} required minLength={12} />
  </EditDialog>{verification.dialog}</>;
}
function DataExport() {
  const { t } = useCopy(), [open, setOpen] = useState(false), [file, setFile] = useState('');
  const scope = t.exportScope;
  return <div className="settings-section settings-export">
    <p className="settings-description">{scope.description}</p>
    {[{ title: scope.includedTitle, items: scope.included }, { title: scope.optionalTitle, items: scope.optional }].map(group => <section className="settings-group" key={group.title} aria-label={group.title}>
      <h3 className="settings-group-title">{group.title}</h3>
      <dl className="settings-export-list">{group.items.map(item => <div key={item.title}><dt>{item.title}</dt><dd className="settings-description">{item.description}</dd></div>)}</dl>
    </section>)}
    <Button variant="secondary" onClick={() => setOpen(true)}><Download size={16} />{t.exportData}</Button>
    {file && <Attachment id={file} />}
    <ModalPresence>{open && <ExportDialog onClose={() => setOpen(false)} onExported={setFile} />}</ModalPresence>
  </div>;
}
function ExportDialog({ onClose, onExported }: { onClose: () => void; onExported: (id: string) => void }) {
  const { t } = useCopy(), [messages, setMessages] = useState(true), [artifacts, setArtifacts] = useState(false);
  const verification = useReauthentication();
  return <><EditDialog title={t.exportData} onClose={onClose} submitLabel={t.exportData} onSubmit={async () => {
    const proof = await verification.verify('export');
    const result = await api.mutate<{ FileID: string }>('/api/settings/export', { verification: proof, include_messages: messages, include_artifacts: artifacts }); onExported(result.FileID);
  }}><p>{t.exportHelp}</p><Checkbox label={t.includeMessages} checked={messages} onChange={event => setMessages(event.target.checked)} /><Checkbox label={t.includeArtifacts} checked={artifacts} onChange={event => setArtifacts(event.target.checked)} /></EditDialog>{verification.dialog}</>;
}
