import { productMessages } from './i18n';
import { useEffect, useRef, useState } from 'react';
import { ArrowLeft, Check, ChevronRight, FileText, Folder, MessagesSquare, CalendarDays, Cpu, Globe, Wrench, Monitor, Search, Settings, Shield, X } from 'lucide-react';
import { Button, Logo, ModalScope, Tooltip, useChoiceScroll, useTheme } from '@molr/ui';
import type { Locale } from './messages';
import { demoResources, resourceCategories, sourceContent } from './interface-demo';
import type { InterfaceDemo, ResourceCategory } from './interface-demo';
import type { InterfacePlatform } from './interface-platform';
import { TaskDrawer } from './TaskDrawer';
import { AssistantWorkspace } from './AssistantWorkspace';
import { SettingsPanel } from './SettingsPanel';
import { PlanPanel } from './PlanPanel';
import { planMessages } from './plan-messages';
import { WorkspaceSearchPanel } from './WorkspaceSearchPanel';
import type { SearchDestination } from './WorkspaceSearchPanel';

type Page = 'home' | 'plans' | 'todos' | 'resources' | 'resource' | 'grants' | 'settings' | 'schedule';
export function InterfaceWorkspace({ platform, locale, demo, layout = 'standard', active = true }: { platform: InterfacePlatform; locale: Locale; demo: InterfaceDemo; layout?: 'standard' | 'mobile'; active?: boolean }) {
  const t = productMessages[locale];
  const mobile = layout === 'mobile';
  const { theme } = useTheme();
  const [overlayContainer, setOverlayContainer] = useState<HTMLDivElement | null>(null);
  const body = useRef<HTMLDivElement>(null);
  const [pageActions, setPageActions] = useState<HTMLDivElement | null>(null);
  const [planAssistant, setPlanAssistant] = useState('');
  const [page, setPage] = useState<Page>('home');
  const [mobileConversation, setMobileConversation] = useState(false);
  const [settingsEntry, setSettingsEntry] = useState<'preferences' | 'connection'>('preferences');
  const [history, setHistory] = useState<Page[]>([]);
  const [taskDrawer, setTaskDrawer] = useState<string | null>(null);
  const [selectedResource, setSelectedResource] = useState('meetings');
  const [category, setCategory] = useState<ResourceCategory>('Storage');
  const resourceViewsRef = useChoiceScroll(page);
  const resourceCategoriesRef = useChoiceScroll(category);
  const [query, setQuery] = useState('');
  const [searchOpen, setSearchOpen] = useState(false);
  const [confirmRevoke, setConfirmRevoke] = useState<string | null>(null);
  const [notice, setNotice] = useState('');
  const [file, setFile] = useState<string | null>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    const closeOverlays = () => { setTaskDrawer(null); setSearchOpen(false); };
    if (!active) closeOverlays();
    // Activity tears effects down when hidden; transient overlays must not
    // reopen when it restores the workspace's persistent page state.
    return closeOverlays;
  }, [active]);
  function go(next: Page, conversation = false) {
    if (next === 'todos' || next === 'schedule') next = 'plans';
    if (next === 'plans') setPlanAssistant('');
    if (mobile && next === 'home') setMobileConversation(conversation);
    setTaskDrawer(null); setHistory(previous => [...previous, page]); setPage(next); setQuery(''); setNotice(''); setFile(null); setConfirmRevoke(null);
    if (mobile) body.current?.scrollTo({ top: 0 });
    requestAnimationFrame(() => heading.current?.focus({ preventScroll: true }));
  }
  function back() { if (mobile) body.current?.scrollTo({ top: 0 }); setPage(history.at(-1) ?? 'home'); setHistory(previous => previous.slice(0, -1)); setFile(null); setConfirmRevoke(null); }
  function openResource(id: string) { setCategory(demoResources.find(r => r.id === id)!.category); setSelectedResource(id); go('resource'); }
  function navigateSearch(destination: SearchDestination) {
    if (destination.kind === 'page') go(destination.page);
    else if (destination.kind === 'resource') openResource(destination.id);
    else if (destination.kind === 'task') setTaskDrawer(destination.id ?? 'list');
    else {
      demo.selectAssistant(destination.id); go('home', true);
      requestAnimationFrame(() => body.current?.querySelector<HTMLElement>(mobile ? '.conversation-back' : '.message-composer textarea')?.focus({ preventScroll: true }));
    }
  }

  const names: Record<Page, string> = { plans: planMessages[locale].title, home: t.interfaceWorkspace.assistant, todos: t.common.todos, resources: t.common.resources, resource: t.interfaceWorkspace.resourceDetails, grants: t.interfaceWorkspace.access, settings: t.common.settings, schedule: t.common.schedule };
  const navigation = [{ id: 'home', icon: MessagesSquare }, { id: 'plans', icon: CalendarDays }, { id: 'resources', icon: Folder }] as const;
  const currentResource = demoResources.find(item => item.id === selectedResource)!;
  const resourceName = (id: string) => { const item = demoResources.find(item => item.id === id)!; return locale === 'zh' ? item.zh : item.en; };
  const categoryIcons = { Storage: Folder, Compute: Cpu, Network: Globe, Tools: Wrench };
  function resourceRow(item: typeof demoResources[number]) {
    const Icon = categoryIcons[item.category];
    return <button className="ip-row" type="button" key={item.id} onClick={() => openResource(item.id)}><span className="ip-icon"><Icon size={18} /></span><span className="ip-row__body"><strong>{resourceName(item.id)}</strong><small>{item.category === 'Storage' ? `${item.device} · ${item.count} ${t.interfaceWorkspace.items}` : (locale === 'zh' ? item.scopeZh! : item.scopeEn!)}</small></span><ChevronRight size={16} /></button>;
  }

  const visibleResources = demoResources.filter(item => item.category === category && `${resourceName(item.id)} ${item.device}`.toLowerCase().includes(query.toLowerCase()));
  const { Chrome } = platform;
  const search = <label className="ip-search"><Search size={16} /><input autoComplete="off" aria-label={t.common.search} placeholder={t.interfaceWorkspace.searchByName} value={query} onChange={event => setQuery(event.target.value)} /></label>;
  const empty = <div className="ip-empty"><Search size={24} /><h4>{t.common.noMatchingResults}</h4><p>{t.interfaceWorkspace.tryADifferentSearchOrClearYourFilters}</p><Button variant="secondary" size="sm" onClick={() => { setQuery(''); }}>{t.interfaceWorkspace.clearFilters}</Button></div>;
  return <div ref={setOverlayContainer} className={`interface-preview ip-shell${mobile ? ' ip-shell--mobile' : ''}${page === 'home' ? ' ip-shell--chat' : ''}`}>
    <ModalScope container={overlayContainer} enabled={mobile}>
    <Chrome locale={locale} page={page} />
    <div className="ip-layout">
      {!mobile && <nav className="ip-nav" aria-label={t.interfaceWorkspace.workspaceNavigation}>
        <Logo className="ip-nav-logo" variant={theme === 'dark' ? 'light' : 'dark'} animation="daily" markOnly interactive interactionLabel={t.common.molrSkipTheRestAndPlayTheNextAnimation} />
        <Tooltip content={t.common.search} side="right" width="auto"><Button variant="secondary" className="ip-nav-control" aria-label={t.common.search} aria-haspopup="dialog" onClick={() => setSearchOpen(true)}><Search size={22} aria-hidden="true" /></Button></Tooltip>
        {navigation.map(({ id, icon: Icon }) => <Tooltip key={id} content={names[id]} side="right" width="auto"><button className="ip-nav-control" type="button" aria-label={names[id]} aria-current={(page === id || (id === 'resources' && ['resource', 'grants'].includes(page))) ? 'page' : undefined} onClick={() => go(id)}><Icon size={22} aria-hidden="true" />{id === 'home' && demo.unreadTotal > 0 && <span className="assistant-unread" aria-label={t.common.unreadMessages(demo.unreadTotal)}>{demo.unreadTotal}</span>}</button></Tooltip>)}
        <Tooltip content={names.settings} side="right" width="auto"><button className="ip-nav-control ip-nav-settings" type="button" aria-label={names.settings} aria-current={page === 'settings' ? 'page' : undefined} onClick={() => go('settings')}><Settings size={22} aria-hidden="true" /></button></Tooltip>
      </nav>}
      <div className="ip-body" ref={body}>
        {!demo.connected && <div className="ip-banner" role="status">{t.interfaceWorkspace.runtimeDisconnectedShowingCachedDataReconnectToManageWork} <button type="button" onClick={() => { setSettingsEntry('connection'); go('settings'); }}>{t.interfaceWorkspace.connectionSettings}</button></div>}
        <div className="ip-content">
          {page !== 'home' && <div className="ip-page-heading">
            <div><span className="ip-eyebrow">{`HOME / ${page.toUpperCase()}`}</span><h3 ref={heading} tabIndex={-1}>{page === 'resource' ? resourceName(currentResource.id) : names[page]}</h3></div>
            <div className="ip-page-actions" ref={setPageActions} role="group" aria-label={t.interfaceWorkspace.pageActions}>
              {page === 'resource' && <><Button size="sm" variant="ghost" onClick={back}><ArrowLeft size={15} />{t.common.back}</Button><Button size="sm" variant="secondary" onClick={() => go('grants')}><Shield size={15} />{names.grants}</Button></>}
              {page === 'grants' && <Button size="sm" variant="ghost" onClick={() => go('home', true)}><ArrowLeft size={15} />{t.interfaceWorkspace.backToConversation}</Button>}
            </div>
          </div>}
          {['resources', 'grants'].includes(page) && <div ref={resourceViewsRef} className="ip-resource-nav ui-choice-list ui-choice-list--line" role="group" aria-label={t.interfaceWorkspace.resourceViews}><button type="button" aria-current={page === 'resources' ? 'page' : undefined} onClick={() => go('resources')}><Folder size={15} />{t.interfaceWorkspace.allResources}</button><button type="button" aria-current={page === 'grants' ? 'page' : undefined} onClick={() => go('grants')}><Shield size={15} />{names.grants}</button></div>}
          {notice && <p className="ip-notice" role="status"><Check size={15} />{notice}</p>}
          {page === 'home' && <AssistantWorkspace locale={locale} demo={demo} active={active && taskDrawer === null && !searchOpen} mobile={mobile} mobileConversation={mobileConversation} onMobileConversationChange={setMobileConversation} onSearch={() => setSearchOpen(true)} openTasks={() => setTaskDrawer('list')} openAccess={() => go('grants')} openSchedule={() => setTaskDrawer('list')} />}
          {page === 'plans' && <PlanPanel key={planAssistant} assistantId={planAssistant} locale={locale} demo={demo} actionTarget={pageActions} onOpenExecution={id => { const work=demo.allWorks.find(w=>w.id===id); if(work)demo.selectAssistant(work.assistantId); setTaskDrawer(id); }} />}

          {page === 'resources' && <><div ref={resourceCategoriesRef} className="resource-categories ui-choice-list ui-choice-list--segmented" role="group" aria-label={t.interfaceWorkspace.resourceCategories}>{resourceCategories.map(value => { const Icon = categoryIcons[value]; return <button type="button" key={value} aria-pressed={category === value} onClick={() => { setCategory(value); setQuery(''); }}><Icon size={18} /><span>{value}</span><small>{demoResources.filter(r => r.category === value).length}</small></button>; })}</div>{search}<p className="ip-muted">{t.interfaceWorkspace.browseStorageComputeNetworkAndToolsAccessIsManaged}</p><div className="ip-list">{visibleResources.length ? visibleResources.map(resourceRow) : empty}</div></>}
          {page === 'resource' && currentResource.category === 'Storage' && <><div className="ip-detail-meta"><span><Monitor size={14} />{currentResource.device}</span><code>{currentResource.path}</code><span>{currentResource.count} {t.interfaceWorkspace.items}</span></div><div className="ip-section-title"><h4>{t.interfaceWorkspace.filePreview}</h4><button type="button" onClick={() => { demo.patchAssistant({ reference: null, draft: t.interfaceWorkspace.pleaseOrganizeTheseSources + resourceName(currentResource.id) }); go('home', true); }}>{t.interfaceWorkspace.createATaskWithThisResource} →</button></div>{demo.grants[currentResource.id] ? <div className="ip-list">{currentResource.files.map(item => <button className="ip-row" type="button" key={item} onClick={() => setFile(item)}><FileText size={18} /><span className="ip-row__body"><strong>{item}</strong><small>{t.interfaceWorkspace.sampleFile}</small></span><ChevronRight size={16} /></button>)}</div> : <div className="ip-empty"><Shield size={28} /><h4>{t.common.accessRequired}</h4><p>{t.interfaceWorkspace.allowAccessToPreviewFilesAndCreateTasks}</p><Button size="sm" onClick={() => go('grants')}>{names.grants}</Button></div>}{file && demo.grants[currentResource.id] && <div className="ip-file-preview"><div className="ip-section-title"><h4>{file}</h4><button type="button" aria-label={t.interfaceWorkspace.closeFilePreview} onClick={() => setFile(null)}><X size={18} /></button></div><FileText size={40} /><pre className="assistant-artifact">{sourceContent[file]}</pre><p>{t.interfaceWorkspace.fileInformation}</p><dl><dt>{t.interfaceWorkspace.location}</dt><dd>{currentResource.path}/{file}</dd><dt>{t.interfaceWorkspace.device}</dt><dd>{currentResource.device}</dd><dt>{t.interfaceWorkspace.availableOperations}</dt><dd>{t.interfaceWorkspace.readAndOrganizePreserveOriginals}</dd></dl></div>}</>}
          {page === 'resource' && currentResource.category !== 'Storage' && <div className="resource-capability"><span className="ip-status">{currentResource.category}</span><p className="ip-muted">{currentResource.path}</p><h4>{t.interfaceWorkspace.capabilitiesAndScope}</h4><p>{(locale === 'zh' ? currentResource.scopeZh! : currentResource.scopeEn!)}</p><dl><dt>{t.interfaceWorkspace.provider}</dt><dd>{currentResource.device}</dd><dt>{t.interfaceWorkspace.currentAssistant}</dt><dd>{demo.activeAssistant.name}</dd><dt>{t.interfaceWorkspace.access2}</dt><dd>{demo.grants[currentResource.id] ? t.interfaceWorkspace.allowed : t.interfaceWorkspace.notAllowed}</dd></dl><p className="ip-muted">{t.interfaceWorkspace.capabilityPreviewOnlyAuthorizingDoesNotRunComputeAccess}</p></div>}
          {page === 'grants' && <><p className="ip-notice">{t.interfaceWorkspace.grantTo}{demo.activeAssistant.name}</p><p className="ip-lead">{t.interfaceWorkspace.accessBelongsToTheSelectedAssistantAndAppliesTo}</p><div className="ip-list">{demoResources.map(item => <div className="ip-grant" key={item.id}><div className="ip-row"><Shield size={20} /><span className="ip-row__body"><strong>{resourceName(item.id)}</strong><small>{item.category} · {item.category === 'Storage' ? t.interfaceWorkspace.readSourcesWriteOnlyToTaskOutputs : (locale === 'zh' ? item.scopeZh! : item.scopeEn!)}</small></span><span className="ip-muted">{demo.grants[item.id] ? t.interfaceWorkspace.allowed : t.interfaceWorkspace.notAllowed}</span><Button variant="secondary" size="sm" disabled={!demo.connected} onClick={() => { if (demo.grants[item.id]) setConfirmRevoke(item.id); else { demo.changeGrant(item.id, true); setNotice(t.interfaceWorkspace.accessAllowedResumeBlockedTasksManually); } }}>{demo.grants[item.id] ? t.interfaceWorkspace.revoke : t.interfaceWorkspace.allowAccess}</Button></div>{confirmRevoke === item.id && <div className="ip-confirm" role="group" aria-label={t.interfaceWorkspace.confirmAccessRevocation}><p>{t.interfaceWorkspace.unfinishedTasksWillWaitForAccessSavedResultsWill}</p><div className="ip-actions"><Button size="sm" disabled={!demo.connected} onClick={() => { demo.changeGrant(item.id, false); setConfirmRevoke(null); setNotice(t.interfaceWorkspace.accessRevokedRelatedWorkIsBlocked); }}>{t.interfaceWorkspace.confirmRevoke}</Button><Button size="sm" variant="ghost" onClick={() => setConfirmRevoke(null)}>{t.common.cancel}</Button></div></div>}</div>)}</div></>}
          {page === 'settings' && <SettingsPanel key={settingsEntry} initialSection={settingsEntry} locale={locale} demo={demo} active={active && taskDrawer === null && !searchOpen} />}
        </div>
      </div>
    </div>
    {mobile && !(page === 'home' && mobileConversation) && <nav className="ip-mobile-nav" aria-label={t.interfaceWorkspace.workspaceNavigation}>
      {[...navigation, { id: 'settings', icon: Settings } as const].map(({ id, icon: Icon }) => <button type="button" key={id} aria-label={names[id]} aria-current={(page === id || (id === 'resources' && ['resource', 'grants'].includes(page))) ? 'page' : undefined} onClick={() => go(id)}><span className="ip-mobile-nav__icon"><Icon size={21} />{id === 'home' && demo.unreadTotal > 0 && <span className="assistant-unread" aria-label={t.common.unreadMessages(demo.unreadTotal)}>{demo.unreadTotal}</span>}</span><span>{id === 'settings' ? t.interfaceWorkspace.me : names[id]}</span></button>)}
    </nav>}
    <WorkspaceSearchPanel open={active && searchOpen} onOpenChange={setSearchOpen} locale={locale} demo={demo} onNavigate={navigateSearch} mobile={mobile} />
    <TaskDrawer demo={demo} locale={locale} mobile={mobile} taskId={active ? taskDrawer : null} onClose={() => setTaskDrawer(null)} onSelect={setTaskDrawer} onDiscuss={id => { demo.patchAssistant({ reference: id }); go('home', true); requestAnimationFrame(() => body.current?.querySelector<HTMLTextAreaElement>('.message-composer textarea')?.focus({ preventScroll: true })); }} openResource={openResource} openGrants={() => go('grants')} />
    {!mobile && <div className="ip-footer"><span><span className={`ip-dot ${!demo.connected ? 'ip-dot--offline' : ''}`} />{demo.connected ? t.interfaceWorkspace.connectedLocally : t.interfaceWorkspace.offlineCachedView}</span><span>{t.interfaceWorkspace.personalAiAssistant}</span><span>{t.interfaceWorkspace.prototype}</span></div>}
    </ModalScope>
  </div>;
}
