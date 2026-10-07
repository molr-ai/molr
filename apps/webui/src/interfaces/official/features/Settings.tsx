import { buildInfo } from '../../../build-info';
import { ProfileSettings } from '../../../administration/UserSettings';
import { NotificationSettings } from '../../../notifications/Settings';
import { notificationCopy } from '../../../notifications/copy';
import { Memories } from './Memories';
import { AccountUsage } from '../../../administration/Usage';
import { Activity, Fragment, useEffect, useState } from 'react';
import { Alert, Badge, Button, Checkbox, IconButton, Logo, Textarea, TextField, useBreakpoint, useTheme, useUILocale } from '@molr/ui';
import { Bell, Brain, ChartNoAxesCombined, ChevronLeft, ChevronRight, Database, Download, Info, Plug, Palette as PaletteIcon, Settings as SettingsIcon, Settings2, Shield, SlidersHorizontal, UserRoundX } from 'lucide-react';
import { api, APIError } from '../../../api/client';
import type { Assistant, Model, RuntimeInfo, Session, Settings as SettingsDTO } from '../../../api/types';
import { useReauthentication } from '../components/ReauthenticationDialog';
import { useCopy, useData, useQuery, useSession } from '../../../state';
import { Attachment } from './Chat';
import { ModelSelect } from '../components/ModelSelect';
import { LoginLinksSettings } from './LoginLinksSettings';
import { SecurityVerificationSettings } from './SecurityVerificationSettings';
import { GeneralSettings } from './GeneralSettings';
import { ThemeSettings } from './ThemeSettings';
import { SettingsHelp } from '../components/SettingsHelp';
import { Confirm, EditDialog, Empty, ErrorNotice, Loading, PageHeader, useAction } from '../components/common';
import './settings.css';

const settingsTabs = ['profile', 'general', 'notifications', 'connection', 'theme', 'preferences', 'memory', 'usage', 'former', 'security', 'data', 'about'];
export function Settings({ models, assistants, openAssistant, initialTab = '', onSelectSection }: { models: Model[]; assistants: Assistant[]; initialTab?: string; openAssistant: (id: string) => void; onSelectSection?: (value: string) => void }) {
  const { t, locale } = useCopy(), mobile = useBreakpoint('mobile');
  const { principal } = useSession();
  const routed = settingsTabs.includes(initialTab) ? initialTab : '';
  const active = routed || (mobile ? '' : 'general');
  const [visited, setVisited] = useState(() => new Set(active ? [active] : []));
  useEffect(() => { if (active) setVisited(previous => new Set([...previous, active])); }, [active]);
  const sections = [
    { value: 'general', label: t.settings.general, icon: Settings2, content: <GeneralSettings /> },
    { value: 'notifications', label: notificationCopy[locale].title, icon: Bell, content: <NotificationSettings /> },
    { value: 'connection', label: t.settings.connection, icon: Plug, content: <Connection /> },
    { value: 'theme', label: t.settings.theme, icon: PaletteIcon, description: t.settings.themeDescription, content: <ThemeSettings /> },
    { value: 'profile', label: locale === 'zh' ? '个人资料' : 'Profile', icon: Settings2, content: <ProfileSettings /> },
    { value: 'preferences', label: t.settings.preferences, icon: SlidersHorizontal, content: <Preferences models={models} /> },
    { value: 'memory', label: t.settings.memory, icon: Brain, content: <Memories assistants={assistants} /> },
    { value: 'usage', label: t.settings.usage, icon: ChartNoAxesCombined, content: <AccountUsage /> },
    { value: 'former', label: t.assistants.former, icon: UserRoundX, content: <FormerAssistants assistants={assistants} openAssistant={openAssistant} /> },
    { value: 'security', label: t.settings.security, icon: Shield, content: <Security /> },
    { value: 'data', label: t.data, icon: Database, content: <DataExport /> },
    { value: 'about', label: t.account.about, icon: Info, content: <About /> },
  ];
  const current = sections.find(item => item.value === active);
  const select = (value: string) => onSelectSection?.(value);
  return <section className="settings-workspace">
    <PageHeader className="settings-header" title={mobile && current ? current.label : locale === 'zh' ? '用户设置' : 'User settings'} icon={<SettingsIcon />} leading={mobile && current && onSelectSection ? <IconButton label={t.back} onClick={() => select('')}><ChevronLeft size={18} /></IconButton> : undefined} />
    <div className="settings-body">
      <nav className="settings-nav" aria-label={locale === 'zh' ? '用户设置' : 'User settings'} hidden={mobile && !!current}>
        {sections.map(item => {
          const Icon = item.icon;
          return <Fragment key={item.value}>
            {item.value === 'about' && principal?.User?.Administrator && <a href="#/settings">
              <Shield size={16} className="settings-nav-icon" aria-hidden="true" /><span>{t.account.admin}</span><ChevronRight size={16} className="settings-nav-caret" aria-hidden="true" />
            </a>}
            <button type="button" aria-current={active === item.value ? 'page' : undefined} onClick={() => select(item.value)}>
              <Icon size={16} className="settings-nav-icon" aria-hidden="true" /><span>{item.label}</span><ChevronRight size={16} className="settings-nav-caret" aria-hidden="true" />
            </button>
          </Fragment>;
        })}
      </nav>
      <div className="settings-panel" hidden={mobile && !current}>
        {sections.filter(item => visited.has(item.value)).map(item => <div key={item.value} className="settings-pane" hidden={item.value !== active}>
          <Activity mode={item.value === active ? 'visible' : 'hidden'}>
            <header className="settings-pane-header"><h2>{item.label}</h2>{item.description && <p>{item.description}</p>}</header>
            {item.content}
          </Activity>
        </div>)}
      </div>
    </div>
  </section>;
}
function About() {
  const { t } = useCopy(), { theme } = useTheme(), { reducedMotion } = useUILocale();
  const repository = 'https://github.com/molr-ai/molr';
  return <div className="settings-section settings-about">
    <div className="settings-about-brand">
      <Logo animation={reducedMotion ? 'static' : 'daily'} interactive variant={theme === 'dark' ? 'light' : 'dark'} />
      <p className="muted settings-about-version">v{buildInfo.version} · <a href={`${repository}/commit/${buildInfo.commit}`} title={buildInfo.commit} target="_blank" rel="noopener noreferrer">{buildInfo.commit.slice(0, 12)}</a></p>
    </div>
    <p>{t.account.starHelp}</p>
    <a className="settings-about-github" href={repository} target="_blank" rel="noopener noreferrer">{t.account.starLink}<ChevronRight size={16} aria-hidden="true" /></a>
  </div>;
}
function preferenceValues(settings: SettingsDTO) {
  return { DefaultModelID: settings.DefaultModelID, AnswerPreferences: settings.AnswerPreferences };
}
function Preferences({ models }: { models: Model[] }) {
  const query = useQuery<SettingsDTO>('/api/settings');
  if (!query.data) return <><ErrorNotice error={query.error} retry={query.reload} />{query.loading && <Loading />}</>;
  return <><ErrorNotice error={query.error} retry={query.reload} /><PreferencesForm settings={query.data} models={models} /></>;
}
function PreferencesForm({ settings, models }: { settings: SettingsDTO; models: Model[] }) {
  const { t } = useCopy(), action = useAction();
  const [base, setBase] = useState(settings), [form, setForm] = useState(settings);
  const changedElsewhere = settings.Version > base.Version && JSON.stringify(preferenceValues(settings)) !== JSON.stringify(preferenceValues(base));
  function reset(value: SettingsDTO) { setBase(value); setForm(value); }
  return <form className="editor settings-form" onSubmit={event => { event.preventDefault(); void action.run(async () => {
    const latest = await api.get<SettingsDTO>('/api/settings');
    if (JSON.stringify(preferenceValues(latest)) !== JSON.stringify(preferenceValues(base))) throw new APIError('conflict');
    const values = { ...latest, ...preferenceValues(form) };
    reset(await api.mutate<SettingsDTO>('/api/settings', { Settings: values }, latest.Version));
  }); }}>
    <ModelSelect size="sm" models={models} value={form.DefaultModelID} onValueChange={DefaultModelID => setForm(value => ({ ...value, DefaultModelID }))} allowNone />

    <Textarea label={t.settings.answer} value={form.AnswerPreferences} onChange={event => setForm(value => ({ ...value, AnswerPreferences: event.target.value }))} maxLength={65536} />
    {changedElsewhere && <Alert tone="warning" title={t.errors.conflict} action={<Button variant="secondary" onClick={() => reset(settings)}>{t.refresh}</Button>} />}
    <ErrorNotice error={action.error} /><Button type="submit" loading={action.busy}>{t.save}</Button>
  </form>;
}
function Connection() {
  const { t, locale } = useCopy(), { messages } = useUILocale(), session = useSession(), { connection } = useData(), action = useAction();
  const runtime = useQuery<RuntimeInfo>('/api/status'), sessions = useQuery<Session[]>('/api/auth/sessions', true);
  const current = sessions.data?.find(value => value.ID === session.principal?.SessionID);
  return <div className="settings-section settings-connection">
    <ErrorNotice error={runtime.error || sessions.error || action.error} retry={runtime.reload} />
    {runtime.loading && !runtime.data ? <Loading /> : <div className="settings-list">
      <div className="settings-runtime-heading"><h3>{runtime.data?.name ?? 'Molr Runtime'}</h3><Badge tone={connection === 'connected' || connection === 'recovered' ? 'positive' : connection === 'failed' ? 'danger' : 'warning'}>{messages.connection[connection]}</Badge></div>
      <dl className="connection-details">
        <div><dt>{t.settings.runtimeAddress}</dt><dd>{runtime.data?.public_url ?? '—'}</dd></div>
        <div><dt>{t.settings.deviceName}</dt><dd>{current?.DeviceName ?? '—'}</dd></div>
        <div><dt>{t.settings.connectedSince}</dt><dd>{current ? new Date(current.CreatedAt).toLocaleString(locale) : '—'}</dd></div>
      </dl>
    </div>}
    <div className="settings-disconnect-block"><div className="settings-disconnect-row"><Button variant="ghost" className="settings-disconnect" loading={action.busy} onClick={() => void action.run(() => api.post('/api/auth/logout'), session.end, false)}>{t.settings.disconnect}</Button></div><p className="muted settings-disconnect-hint">{t.settings.disconnectHelp}</p></div>
  </div>;
}
function FormerAssistants({ assistants, openAssistant }: { assistants: Assistant[]; openAssistant: (id: string) => void }) {
  const { t } = useCopy(), [rehire, setRehire] = useState<Assistant | null>(null); const former = assistants.filter(a => a.State === 'dismissed');
  return <div className="settings-section">{!former.length ? <Empty /> : <div className="settings-rows">{former.map(row => <article className="settings-row-item" key={row.ID}>
    <div className="settings-row-item-head"><h3>{row.Name}</h3><Badge>{t.assistants.archived}</Badge><span className="settings-row-item-actions"><Button size="sm" variant="secondary" onClick={() => openAssistant(row.ID)}>{t.chat.message}</Button><Button size="sm" onClick={() => setRehire(row)}>{t.assistants.rehire}</Button></span></div>
  </article>)}</div>}
    {rehire && <Confirm title={t.assistants.rehire} description={t.assistants.rehireHelp} onClose={() => setRehire(null)} onConfirm={() => api.mutate('/api/assistants/rehire', { ID: rehire.ID, RestoreGrantIDs: [], RestoreScheduleIDs: [] }, rehire.Version)} />}
  </div>;
}
function Security() {
  const { t, locale } = useCopy(), session = useSession(), query = useQuery<Session[]>('/api/auth/sessions', true), action = useAction();
  const [revoke, setRevoke] = useState<Session | null>(null), [password, setPassword] = useState(false);
  return <div className="settings-section settings-security"><div className="settings-toolbar"><Button variant="secondary" onClick={() => setPassword(true)}>{t.settings.changePassword}</Button><Button variant="secondary" disabled={action.busy} onClick={() => void action.run(() => api.post('/api/auth/logout'), session.end, false)}>{t.signOut}</Button></div>
    <SecurityVerificationSettings /><LoginLinksSettings /><section className="settings-group"><h3 className="settings-group-title">{t.settings.sessions}</h3><ErrorNotice error={query.error || action.error} retry={query.reload} />{query.loading && !query.data ? <Loading /> : <div className="settings-rows">{query.data?.map(row => <article className="settings-row-item" key={row.ID}>
      <div className="settings-row-item-head"><h3>{row.DeviceName}</h3>{row.ID === session.principal?.SessionID && <Badge>{t.settings.current}</Badge>}<span className="settings-row-item-actions"><Button size="sm" variant="ghost" onClick={() => setRevoke(row)}>{t.settings.revokeSession}</Button></span></div>
      <p className="muted">{new Date(row.LastSeenAt).toLocaleString(locale)}</p>
    </article>)}</div>}</section>
    {revoke && <Confirm title={t.settings.revokeSession} description={t.settings.revokeSessionHelp} onClose={() => setRevoke(null)} onConfirm={async () => { await api.remove(`/api/auth/sessions/${revoke.ID}`); if (revoke.ID === session.principal?.SessionID) session.end(); }} />}
    {password && <PasswordEditor onClose={() => setPassword(false)} />}
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
  return <div className="settings-section"><div className="settings-toolbar"><Button variant="secondary" onClick={() => setOpen(true)}><Download size={16} />{t.exportData}</Button><SettingsHelp label={t.exportData} content={t.exportHelp} /></div>{file && <Attachment id={file} />}{open && <ExportDialog onClose={() => setOpen(false)} onExported={setFile} />}</div>;
}
function ExportDialog({ onClose, onExported }: { onClose: () => void; onExported: (id: string) => void }) {
  const { t } = useCopy(), [messages, setMessages] = useState(true), [artifacts, setArtifacts] = useState(false);
  const verification = useReauthentication();
  return <><EditDialog title={t.exportData} onClose={onClose} submitLabel={t.exportData} onSubmit={async () => {
    const proof = await verification.verify('export');
    const result = await api.mutate<{ FileID: string }>('/api/settings/export', { verification: proof, include_messages: messages, include_artifacts: artifacts }); onExported(result.FileID);
  }}><p>{t.exportHelp}</p><Checkbox label={t.includeMessages} checked={messages} onChange={event => setMessages(event.target.checked)} /><Checkbox label={t.includeArtifacts} checked={artifacts} onChange={event => setArtifacts(event.target.checked)} /></EditDialog>{verification.dialog}</>;
}
