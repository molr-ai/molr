import { WorkspaceFiles } from './features/WorkspaceFiles';
import type { ExplorerMemory } from './features/WorkspaceFiles';
import type { FloatingBounds } from './features/useFloatingPanel';
import { useErrorToast } from '../../error-toasts';
import { MobileChatPresence } from './components/MobileChatPresence';
import { MobileAccountButton } from './components/MobileAccountButton';
import { ChatChannelDialog } from './features/WeixinChannel';
import { useUIHost } from '../../ui-host/store';
import { useEffect, useId, useRef, useState } from 'react';
import { ModalPresence, AssistantList, Button, EmptyState, IconButton, Logo, SearchField, Select, useBreakpoint, useTheme } from '@molr/ui';
import { MessagesSquare, Pin, Plus, Search as SearchIcon } from 'lucide-react';
import type { Assistant, AssistantActivity, Conversation, Model, RuntimeInfo, Settings as SettingsDTO } from '../../api/types';
import { api } from '../../api/client';
import { useCopy, useQuery } from '../../state';
import { guardedNavigation, useNavigation } from '../../navigation';
import type { RoutePage } from '../../navigation';
import { Confirm, Loading, useAction } from './components/common';
import { Chat } from './features/Chat';
import { AssistantMessagePreview } from './features/AssistantMessagePreview';
import type { AssistantAction } from './features/Chat';
import { CloneAssistant, HireAssistant } from './features/AssistantDialogs';
import { Plans } from './features/Plans';
import { Resources } from './features/Resources';
import { Settings } from './features/Settings';
import { Workbench } from './features/Workbench';
import { AssistantInfo } from './features/AssistantInfo';
import { Tasks } from './features/Tasks';
import { Browser } from './features/Browser';
import type { BrowserBounds } from './features/useBrowserView';
import { Terminals } from './features/Terminals';
import { ShellTasks } from './features/ShellTasks';
import { Search } from './features/Search';
import { usePanelDocking } from '../../use-panel-docking';
import { ChatSidePanel, isChatPanel } from './components/ChatSidePanel';

export function OfficialPage({ chatLayout = 'default' }: { chatLayout?: 'default' | 'focus' } = {}) {
  const { t } = useCopy(), { theme } = useTheme(), mobile = useBreakpoint('mobile');
  const { page, id, turnID, eventID, messageID, panel, taskID, navigate } = useNavigation(), action = useAction();
  const assistants = useQuery<Assistant[]>('/api/assistants', true), conversations = useQuery<Conversation[]>('/api/conversations', true), models = useQuery<Model[]>('/api/models'), settings = useQuery<SettingsDTO>('/api/settings');
  const activity = useQuery<AssistantActivity[]>(page === 'assistants' && (chatLayout === 'focus' || !mobile || !id) ? '/api/assistants/activity' : null, true);
  const activityByID = new Map(activity.data?.map(item => [item.AssistantID, item]));
  const runtime = useQuery<RuntimeInfo>('/api/status');
  const workbenchVisible = !!runtime.data?.workbench_available && !!settings.data?.WorkbenchVisible;
  const [search, setSearch] = useState(false), [hire, setHire] = useState(false), [dialog, setDialog] = useState<{ kind: AssistantAction; assistant: Assistant } | null>(null), [taskCard, setTaskCard] = useState('');
  const [assistantQuery, setAssistantQuery] = useState('');
  const [chatWorkspace, setChatWorkspace] = useState<HTMLDivElement | null>(null);
  const panelDocked = usePanelDocking(chatWorkspace) && !mobile && chatLayout === 'default';
  const [browserFloating, setBrowserFloating] = useState(true);
  const [terminalFloating, setTerminalFloating] = useState(true);
  const terminalBounds = useRef<FloatingBounds | null>(null);
  const [filesFloating, setFilesFloating] = useState(true);
  const filesBounds = useRef<FloatingBounds | null>(null);
  const filesMemory = useRef(new Map<string, ExplorerMemory>());
  function explorerMemory(id: string) { let value = filesMemory.current.get(id); if (!value) { value = { directory: '.', scroll: 0, hidden: false, sort: 'name' }; filesMemory.current.set(id, value); } return value; }
  const browserBounds = useRef<BrowserBounds | null>(null);
  const floating = browserFloating && !mobile;
  const assistantSearchId = useId();
  const [pendingTask, setPendingTask] = useState<{ assistantID: string; taskID?: string } | null>(null);
  const { drafts, setDrafts } = useUIHost();
  const selection = useQuery<Assistant>(page === 'assistants' && id ? `/api/assistants/${id}` : null);
  const allAssistants = selection.data && !assistants.data?.some(row => row.ID === selection.data?.ID) ? [...(assistants.data ?? []), selection.data] : assistants.data ?? [];
  const messageTimes = new Map(activity.data?.map(item => [item.AssistantID, item.Preview ? Date.parse(item.Preview.CreatedAt) || 0 : 0]));
  const orderedAssistants = [...allAssistants].sort((a, b) => Number(b.Pinned) - Number(a.Pinned)
    || (messageTimes.get(b.ID) ?? 0) - (messageTimes.get(a.ID) ?? 0));
  const active = orderedAssistants.filter(assistant => assistant.State === 'active');
  const selected = selection.data ?? allAssistants.find(assistant => assistant.ID === id);
  useEffect(() => { const open = () => setSearch(true); window.addEventListener('molr:search', open); return () => window.removeEventListener('molr:search', open); }, []);
  useEffect(() => { setDialog(null); setTaskCard(''); }, [page, id]);
  useEffect(()=>{if(panel&&selected?.ID===id){setTaskCard(taskID);setDialog({kind:panel,assistant:selected});}},[panel,taskID,id,selected?.ID]);
  useEffect(() => {
    if (pendingTask && page === 'assistants' && id === pendingTask.assistantID && selected?.ID === pendingTask.assistantID) {
      setTaskCard(pendingTask.taskID ?? ''); setDialog({ kind: 'tasks', assistant: selected }); setPendingTask(null);
    }
  }, [pendingTask, page, id, selected]);
  const entries = (members: Assistant[]) => members.map(assistant => {
    const conversation = conversations.data?.find(row => row.ID === assistant.ConversationID);
    return { id: assistant.ID, name: assistant.Name, avatar: assistant.Avatar, preview: <AssistantActivityPreview activity={activityByID.get(assistant.ID)} />, pinned: assistant.Pinned, unread: conversation?.UnreadCount ?? 0 };
  });
  const normalizedQuery = assistantQuery.trim().toLowerCase();
  const visibleAssistants = active.filter(assistant => assistant.Name.toLowerCase().includes(normalizedQuery));
  useErrorToast(assistants.error || activity.error || selection.error || conversations.error || models.error || action.error, assistants.reload);
  function assistantAction(kind: AssistantAction) {
    if (!selected) return;
    if (kind === 'pin') void action.run(() => api.mutate('/api/assistants/place', { ID: selected.ID, GroupID: selected.GroupID, Pinned: !selected.Pinned }, selected.Version), undefined, false);
    else guardedNavigation(() => void action.run(async () => {
      const current = await api.get<Assistant>(`/api/assistants/${selected.ID}`);
      setDialog({ kind, assistant: current });
    }, undefined, false));
  }
  const go = (next: RoutePage, objectID?: string, turnID?: string, messageID?: string) => { setSearch(false); navigate(next, objectID, turnID, messageID); };
  const canHire = models.data?.some(model => model.SupportsTools) ?? false;
  const searchEmpty = active.length > 0 && !!normalizedQuery;
  const emptyTitle = searchEmpty ? t.assistants.noMatches : t.assistants.none;
  function clearAssistantSearch() { setAssistantQuery(''); document.getElementById(assistantSearchId)?.focus(); }
  function startHiring() { if (!models.data) return; setAssistantQuery(''); if (canHire) setHire(true); else go('resources'); }
  const mobileActions = <><IconButton label={t.search} aria-haspopup="dialog" onClick={() => setSearch(true)}><SearchIcon size={20} /></IconButton><IconButton variant="ghost" label={t.assistants.hire} disabled={!models.data} onClick={startHiring}><Plus size={20} /></IconButton></>;
  const emptyList = <EmptyState title={emptyTitle}
    description={searchEmpty ? t.assistants.noMatchesHelp : canHire ? t.assistants.emptyListHelp : t.assistants.noModels}
    illustration={searchEmpty ? <SearchIcon /> : <MessagesSquare />}
    action={searchEmpty ? <Button variant="secondary" size="sm" onClick={clearAssistantSearch}>{t.assistants.clearSearch}</Button>
      : <Button variant="secondary" size="sm" onClick={startHiring}>{canHire ? t.assistants.hire : t.assistants.configure}</Button>} />;
  return <>
        {page === 'assistants' ? <div className={`assistant-workspace${chatLayout === 'focus' ? ' assistant-workspace--focus' : ''}`} ref={setChatWorkspace}>
          {(chatLayout === 'focus' || !mobile || !selected) && <aside className="workspace-sidebar" aria-label={t.assistants.list}>
            {mobile && chatLayout === 'focus' && <header className="assistant-toolbar"><MobileAccountButton />{mobileActions}</header>}
            {chatLayout === 'focus' ? (assistants.loading && !assistants.data ? <Loading /> : !allAssistants.length ? emptyList : <Select label={t.assistants.list} value={selected?.ID ?? ''} onValueChange={value => go('assistants', value)} options={orderedAssistants.filter(assistant => assistant.State === 'active' || assistant.ID === selected?.ID).map(assistant => ({ value: assistant.ID, label: assistant.Name }))} />) : <>
            <header className="assistant-toolbar">{mobile && <MobileAccountButton />}<SearchField id={assistantSearchId} className="assistant-search" label={t.assistants.search} hideLabel placeholder={t.assistants.search} value={assistantQuery} onValueChange={setAssistantQuery} />{mobile && mobileActions}</header>
            {assistants.loading && !assistants.data ? <Loading /> : assistants.data ? <AssistantList label={t.assistants.list} entries={entries(visibleAssistants)} selectedID={selected?.ID} onSelect={value => go('assistants', value)} mobile={mobile} pinnedLabel={t.assistants.pinned} unreadLabel={t.assistants.unread} emptyLabel={emptyTitle} emptyState={emptyList} pinIcon={<Pin size={12} />} /> : null}
            </>}
            {!mobile && active.length > 0 && <footer className="sidebar-footer"><Button variant="secondary" disabled={!models.data} onClick={startHiring}><Plus size={18} />{t.assistants.hire}</Button></footer>}
          </aside>}
          <MobileChatPresence enabled={mobile}>{selected && (chatLayout === 'focus' || !mobile || id) ? <Chat targetMessageID={messageID} onClearTarget={() => go('assistants', selected.ID)} timeZone={settings.data?.Timezone} onDebug={workbenchVisible ? turnID => go('workbench', selected.ID, turnID) : undefined} key={selected.ID} assistant={selected} conversation={conversations.data?.find(conversation => conversation.ID === selected.ConversationID)} draft={drafts[selected.ID] ?? { text: '', files: [] }} updateDraft={update => setDrafts(previous => ({ ...previous, [selected.ID]: typeof update === 'function' ? update(previous[selected.ID] ?? { text: '', files: [] }) : update }))} onBack={mobile ? () => go('assistants') : undefined} onAction={assistantAction} onCard={(kind, target) => { if (kind === 'task') { setTaskCard(target); setDialog({ kind: 'tasks', assistant: selected }); } else go(kind === 'resource' ? 'resources' : kind === 'settings' ? 'settings' : 'schedules', target); }} /> : null}</MobileChatPresence>
          {!selected && !mobile && <div className="workspace-empty"><EmptyState title={t.chat.workspaceTitle} illustration={<Logo markOnly animation="static" variant={theme === 'dark' ? 'light' : 'dark'} className="workspace-empty__logo" />} /></div>}
          <ChatSidePanel panel={isChatPanel(dialog) ? dialog : null} docked={panelDocked} floating={{ terminal: terminalFloating && !mobile, browser: floating, files: filesFloating && !mobile }}>{panel => <>
            {(panel.kind === 'info' || panel.kind === 'permissions') && <AssistantInfo docked={panelDocked} assistant={assistants.data?.find(item => item.ID === panel.assistant.ID) ?? panel.assistant} models={models.data ?? []} initialTab={panel.kind === 'permissions' ? 'permissions' : 'overview'} onClose={() => setDialog(null)} onDebug={workbenchVisible ? () => { setDialog(null); go('workbench', panel.assistant.ID); } : undefined} />}
            {panel.kind === 'files' && <WorkspaceFiles key={panel.assistant.ID} assistantID={panel.assistant.ID} assistantName={panel.assistant.Name} docked={panelDocked} floating={filesFloating && !mobile} onFloatingChange={setFilesFloating} bounds={filesBounds} memory={explorerMemory(panel.assistant.ID)} onClose={() => setDialog(null)} />}
            {panel.kind === 'browser' && <Browser key={panel.assistant.ID} floating={floating} onFloatingChange={setBrowserFloating} bounds={browserBounds} docked={panelDocked} assistantID={panel.assistant.ID} onClose={() => setDialog(null)} />}
            {panel.kind === 'terminal' && <Terminals key={panel.assistant.ID} assistantID={panel.assistant.ID} assistantName={panel.assistant.Name} docked={panelDocked} floating={terminalFloating && !mobile} onFloatingChange={setTerminalFloating} bounds={terminalBounds} onClose={() => setDialog(null)} />}
            {panel.kind === 'shell_tasks' && <ShellTasks key={panel.assistant.ID} docked={panelDocked} assistantID={panel.assistant.ID} onClose={() => setDialog(null)} />}
            {panel.kind === 'tasks' && <Tasks docked={panelDocked} assistantID={panel.assistant.ID} taskID={taskCard} onClose={() => setDialog(null)} />}
          </>}</ChatSidePanel>
        </div> : page === 'plans' || page === 'todos' || page === 'schedules' ? <Plans key={id} assistantID={id.startsWith('assistant-') ? id.slice(10) : undefined} selectedID={id} assistants={assistants.data ?? []} /> : page === 'resources' ? <Resources resourceID={id} /> : page === 'workbench' ? <Workbench turnID={turnID} eventID={eventID} timezone={settings.data?.Timezone} available={!!runtime.data?.workbench_available} assistants={assistants.data ?? []} assistantID={id} onSelectAssistant={value => go('workbench', value)} onSettings={() => go('account', 'general')} /> : <Settings initialTab={id} onSelectSection={value => go('account', value)} assistants={assistants.data ?? []} openAssistant={value => go('assistants', value)} />}
      <Search open={search} onClose={() => setSearch(false)} navigate={go} taskAssistantID={selected?.ID ?? active[0]?.ID} onOpenTasks={(assistantID, taskID) => { go('assistants', assistantID); setPendingTask({ assistantID, taskID }); }} /><ModalPresence>{hire && <HireAssistant defaultModelID={settings.data?.DefaultModelID} models={models.data ?? []} onClose={() => setHire(false)} onCreated={value => go('assistants', value)} />}</ModalPresence>
      <ModalPresence>{dialog?.kind === 'channel' && <ChatChannelDialog key={dialog.assistant.ID} assistant={assistants.data?.find(item => item.ID === dialog.assistant.ID) ?? dialog.assistant} onClose={() => setDialog(null)} />}</ModalPresence>
      <ModalPresence>{dialog?.kind === 'clone' && <CloneAssistant assistant={dialog.assistant} onClose={() => setDialog(null)} onCreated={value => go('assistants', value)} />}</ModalPresence>
      <ModalPresence>{dialog?.kind === 'dismiss' && <Confirm title={t.assistants.dismiss} description={t.assistants.dismissHelp} danger onClose={() => setDialog(null)} onConfirm={async () => { await api.mutate('/api/assistants/dismiss', { ID: dialog.assistant.ID }, dialog.assistant.Version); go('assistants'); }} />}</ModalPresence>

  </>;
}

function AssistantActivityPreview({ activity }: { activity?: AssistantActivity }) {
 const { t } = useCopy();
 return activity?.Dreaming ? <span className="assistant-dreaming-label">☾ {t.chat.dreaming}</span> : <AssistantMessagePreview activity={activity} />;
}
