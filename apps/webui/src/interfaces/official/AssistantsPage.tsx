import { PaneSeparator } from '@molr/ui';
import { LIST_MIN, useAssistantPaneSizes } from './components/useAssistantPaneSizes';
import type { CSSProperties } from 'react';
import type { ExplorerMemory } from './features/WorkspaceFiles';
import type { FloatingBounds } from './features/useFloatingPanel';
import { useErrorToast } from '../../error-toasts';
import { MobileChatPresence } from './components/MobileChatPresence';
import { MobileAccountButton } from './components/MobileAccountButton';
import { lazy, Suspense, useEffect, useId, useRef, useState } from 'react';
import { ModalPresence, AssistantList, Button, EmptyState, IconButton, Logo, SearchField, useBreakpoint, useTheme } from '@molr/ui';
import { MessagesSquare, Pin, Plus, Search as SearchIcon } from 'lucide-react';
import type { Assistant, AssistantActivity, Conversation, Model, Settings as SettingsDTO } from '@molr/sdk';
import { api } from '@molr/sdk';
import { useCopy, useQuery } from '../../state';
import { guardedNavigation, useNavigation } from '../../navigation';
import { Confirm, Loading, useAction } from './components/common';
import { Chat } from './features/Chat';
import { AssistantMessagePreview } from './features/AssistantMessagePreview';
import type { AssistantAction } from './features/Chat';
import { CloneAssistant, HireAssistant } from './features/AssistantDialogs';
import type { BrowserBounds } from './features/useBrowserView';
import { usePanelDocking } from '../../use-panel-docking';
import { ChatSidePanel, isChatPanel } from './components/ChatSidePanel';

const ChatChannelDrawer = lazy(() => import('./features/WeixinChannel').then(module => ({ default: module.ChatChannelDrawer })));
const AssistantInfo = lazy(() => import('./features/AssistantInfo').then(module => ({ default: module.AssistantInfo })));
const WorkspaceFiles = lazy(() => import('./features/WorkspaceFiles').then(module => ({ default: module.WorkspaceFiles })));
const Browser = lazy(() => import('./features/Browser').then(module => ({ default: module.Browser })));
const Terminals = lazy(() => import('./features/Terminals').then(module => ({ default: module.Terminals })));
const ShellTasks = lazy(() => import('./features/ShellTasks').then(module => ({ default: module.ShellTasks })));
const Tasks = lazy(() => import('./features/Tasks').then(module => ({ default: module.Tasks })));

// Kept by the route shell so leaving this page releases subscriptions without
// losing in-session list and floating-panel preferences.
export interface AssistantsPageMemory {
  query?: string;
  browserFloating?: boolean;
  terminalFloating?: boolean;
  filesFloating?: boolean;
  browserBounds?: BrowserBounds | null;
  terminalBounds?: FloatingBounds | null;
  filesBounds?: FloatingBounds | null;
  files?: Map<string, ExplorerMemory>;
}

export function AssistantsPage({ assistants, settings, workbenchVisible, memory, onSearch }: {
  assistants: ReturnType<typeof useQuery<Assistant[]>>;
  settings?: SettingsDTO;
  workbenchVisible: boolean;
  memory: AssistantsPageMemory;
  onSearch: () => void;
}) {
  const { t } = useCopy(), { theme } = useTheme(), mobile = useBreakpoint('mobile');
  const { id, messageID, panel, taskID, navigate: go, closePanel } = useNavigation(), action = useAction();
  const conversations = useQuery<Conversation[]>('/api/conversations', true), models = useQuery<Model[]>('/api/models');
  const activity = useQuery<AssistantActivity[]>((!mobile || !id) ? '/api/assistants/activity' : null, true);
  const activityByID = new Map(activity.data?.map(item => [item.AssistantID, item]));
  const [hire, setHire] = useState(false), [dialog, setDialog] = useState<{ kind: AssistantAction; assistant: Assistant } | null>(null), [taskCard, setTaskCard] = useState('');
  const [assistantQuery, setAssistantQuery] = useState(memory.query ?? '');
  const [chatWorkspace, setChatWorkspace] = useState<HTMLDivElement | null>(null);
  const panelDocked = usePanelDocking(chatWorkspace) && !mobile;
  const [browserFloating, setBrowserFloating] = useState(memory.browserFloating ?? true);
  const [terminalFloating, setTerminalFloating] = useState(memory.terminalFloating ?? true);
  const terminalBounds = useRef<FloatingBounds | null>(memory.terminalBounds ?? null);
  const [filesFloating, setFilesFloating] = useState(memory.filesFloating ?? true);
  const filesBounds = useRef<FloatingBounds | null>(memory.filesBounds ?? null);
  const filesMemory = useRef(memory.files ?? new Map<string, ExplorerMemory>());
  function explorerMemory(id: string) { let value = filesMemory.current.get(id); if (!value) { value = { directory: '.', scroll: 0, hidden: false, sort: 'name' }; filesMemory.current.set(id, value); } return value; }
  const browserBounds = useRef<BrowserBounds | null>(memory.browserBounds ?? null);
  const floating = browserFloating && !mobile;
  const paneKind = isChatPanel(dialog) ? dialog.kind : undefined;
  const paneFloating = paneKind === 'browser' ? floating : paneKind === 'files' ? filesFloating && !mobile : paneKind === 'terminal' ? terminalFloating && !mobile : false;
  const panes = useAssistantPaneSizes(chatWorkspace, paneKind, panelDocked && !!paneKind && !paneFloating);
  const resizable = !mobile;
  const assistantSearchId = useId();
  const selection = useQuery<Assistant>(id ? `/api/assistants/${id}` : null);
  const allAssistants = selection.data && !assistants.data?.some(row => row.ID === selection.data?.ID) ? [...(assistants.data ?? []), selection.data] : assistants.data ?? [];
  const messageTimes = new Map(activity.data?.map(item => [item.AssistantID, item.Preview ? Date.parse(item.Preview.CreatedAt) || 0 : 0]));
  const orderedAssistants = [...allAssistants].sort((a, b) => Number(b.Pinned) - Number(a.Pinned)
    || (messageTimes.get(b.ID) ?? 0) - (messageTimes.get(a.ID) ?? 0));
  const active = orderedAssistants.filter(assistant => assistant.State === 'active');
  const selected = selection.data ?? allAssistants.find(assistant => assistant.ID === id);
  useEffect(() => { setDialog(null); setTaskCard(''); }, [id]);
  useEffect(()=>{if(panel&&selected?.ID===id){setTaskCard(taskID);setDialog({kind:panel,assistant:selected});}},[panel,taskID,id,selected?.ID]);
  useEffect(() => {
    Object.assign(memory, { query: assistantQuery, browserFloating, terminalFloating, filesFloating });
  }, [memory, assistantQuery, browserFloating, terminalFloating, filesFloating]);
  useEffect(() => () => {
    Object.assign(memory, { browserBounds: browserBounds.current, terminalBounds: terminalBounds.current, filesBounds: filesBounds.current, files: filesMemory.current });
  }, [memory]);
  const entries = (members: Assistant[]) => members.map(assistant => {
    const conversation = conversations.data?.find(row => row.ID === assistant.ConversationID);
    return { id: assistant.ID, name: assistant.Name, avatar: assistant.Avatar, preview: <AssistantActivityPreview activity={activityByID.get(assistant.ID)} />, pinned: assistant.Pinned, unread: conversation?.UnreadCount ?? 0 };
  });
  const normalizedQuery = assistantQuery.trim().toLowerCase();
  const visibleAssistants = active.filter(assistant => assistant.Name.toLowerCase().includes(normalizedQuery));
  useErrorToast(activity.error || selection.error || conversations.error || models.error || action.error, assistants.reload);
  function closeDialog() { setDialog(null); closePanel(); }
  function assistantAction(kind: AssistantAction) {
    if (!selected) return;
    if (kind === 'pin') void action.run(() => api.mutate('/api/assistants/place', { ID: selected.ID, GroupID: selected.GroupID, Pinned: !selected.Pinned }, selected.Version), undefined, false);
    else guardedNavigation(() => void action.run(async () => {
      const current = await api.get<Assistant>(`/api/assistants/${selected.ID}`);
      setDialog({ kind, assistant: current });
    }, undefined, false));
  }
  const canHire = models.data?.some(model => model.SupportsTools) ?? false;
  const searchEmpty = active.length > 0 && !!normalizedQuery;
  const emptyTitle = searchEmpty ? t.assistants.noMatches : t.assistants.none;
  function clearAssistantSearch() { setAssistantQuery(''); document.getElementById(assistantSearchId)?.focus(); }
  function startHiring() { if (!models.data) return; setAssistantQuery(''); if (canHire) setHire(true); else go('resources'); }
  const mobileActions = <><IconButton label={t.search} aria-haspopup="dialog" onClick={onSearch}><SearchIcon size={20} /></IconButton><IconButton variant="ghost" label={t.assistants.hire} disabled={!models.data} onClick={startHiring}><Plus size={20} /></IconButton></>;
  const emptyList = <EmptyState title={emptyTitle}
    description={searchEmpty ? t.assistants.noMatchesHelp : canHire ? t.assistants.emptyListHelp : t.assistants.noModels}
    illustration={searchEmpty ? <SearchIcon /> : <MessagesSquare />}
    action={searchEmpty ? <Button variant="secondary" size="sm" onClick={clearAssistantSearch}>{t.assistants.clearSearch}</Button>
      : <Button variant="secondary" size="sm" onClick={startHiring}>{canHire ? t.assistants.hire : t.assistants.configure}</Button>} />;
  return <>
        <div className="assistant-workspace" ref={setChatWorkspace} style={resizable ? { '--workspace-sidebar-width': `${panes.list}px` } as CSSProperties : undefined}>
          {(!mobile || !selected) && <aside className="workspace-sidebar" aria-label={t.assistants.list}>
            <header className="assistant-toolbar">{mobile && <MobileAccountButton />}<SearchField id={assistantSearchId} className="assistant-search" label={t.assistants.search} hideLabel placeholder={t.assistants.search} value={assistantQuery} onValueChange={setAssistantQuery} />{mobile && mobileActions}</header>
            {assistants.loading && !assistants.data ? <Loading /> : assistants.data ? <AssistantList label={t.assistants.list} entries={entries(visibleAssistants)} selectedID={selected?.ID} onSelect={value => go('assistants', value)} mobile={mobile} pinnedLabel={t.assistants.pinned} unreadLabel={t.assistants.unread} emptyLabel={emptyTitle} emptyState={emptyList} pinIcon={<Pin size={12} />} /> : null}
            {!mobile && active.length > 0 && <footer className="sidebar-footer"><Button variant="secondary" disabled={!models.data} onClick={startHiring}><Plus size={18} />{t.assistants.hire}</Button></footer>}
          </aside>}
          {resizable && <PaneSeparator className="pane-separator" label={t.assistants.list} value={panes.list} min={LIST_MIN} max={panes.listMax} style={{ left: panes.list }} onResize={value => panes.resize('list', value)} onCommit={panes.save} />}
          {resizable && panelDocked && paneKind && !paneFloating && <PaneSeparator className="pane-separator" key={paneKind} label={t.chat.panelResize} reverse value={panes.panel} min={panes.panelMin} max={panes.panelMax} style={{ right: panes.panel }} onResize={value => panes.resize(paneKind, value)} onCommit={panes.save} />}
          <MobileChatPresence enabled={mobile}>{selected && (!mobile || id) ? <Chat targetMessageID={messageID} onClearTarget={() => go('assistants', selected.ID)} timeZone={settings?.Timezone} onDebug={workbenchVisible ? turnID => go('workbench', selected.ID, turnID) : undefined} key={selected.ID} assistant={selected} conversation={conversations.data?.find(conversation => conversation.ID === selected.ConversationID)} onBack={mobile ? () => go('assistants') : undefined} onAction={assistantAction} onCard={(kind, target) => { if (kind === 'task') { setTaskCard(target); setDialog({ kind: 'tasks', assistant: selected }); } else go(kind === 'resource' ? 'resources' : kind === 'settings' ? 'settings' : 'schedules', target); }} /> : null}</MobileChatPresence>
          {!selected && !mobile && <div className="workspace-empty"><EmptyState title={t.chat.workspaceTitle} illustration={<Logo markOnly animation="static" variant={theme === 'dark' ? 'light' : 'dark'} className="workspace-empty__logo" />} /></div>}
          <ChatSidePanel width={resizable ? panes.panel : undefined} panel={isChatPanel(dialog) ? dialog : null} docked={panelDocked} floating={{ terminal: terminalFloating && !mobile, browser: floating, files: filesFloating && !mobile }}>{panel => <Suspense fallback={<Loading />}>
            {(panel.kind === 'info' || panel.kind === 'permissions') && <AssistantInfo docked={panelDocked} assistant={assistants.data?.find(item => item.ID === panel.assistant.ID) ?? panel.assistant} models={models.data ?? []} initialTab={panel.kind === 'permissions' ? 'permissions' : 'overview'} onClose={closeDialog} onDebug={workbenchVisible ? () => { setDialog(null); go('workbench', panel.assistant.ID); } : undefined} />}
            {panel.kind === 'files' && <WorkspaceFiles key={panel.assistant.ID} assistantID={panel.assistant.ID} assistantName={panel.assistant.Name} docked={panelDocked} floating={filesFloating && !mobile} onFloatingChange={setFilesFloating} bounds={filesBounds} memory={explorerMemory(panel.assistant.ID)} onClose={closeDialog} />}
            {panel.kind === 'browser' && <Browser key={panel.assistant.ID} floating={floating} onFloatingChange={setBrowserFloating} bounds={browserBounds} docked={panelDocked} assistantID={panel.assistant.ID} onClose={closeDialog} />}
            {panel.kind === 'terminal' && <Terminals key={panel.assistant.ID} assistantID={panel.assistant.ID} assistantName={panel.assistant.Name} docked={panelDocked} floating={terminalFloating && !mobile} onFloatingChange={setTerminalFloating} bounds={terminalBounds} onClose={closeDialog} />}
            {panel.kind === 'shell_tasks' && <ShellTasks key={panel.assistant.ID} docked={panelDocked} assistantID={panel.assistant.ID} onClose={closeDialog} />}
            {panel.kind === 'tasks' && <Tasks docked={panelDocked} assistantID={panel.assistant.ID} taskID={taskCard} onClose={closeDialog} />}
          </Suspense>}</ChatSidePanel>
        </div>
      <ModalPresence>{hire && <HireAssistant defaultModelID={settings?.DefaultModelID} models={models.data ?? []} onClose={() => setHire(false)} onCreated={value => go('assistants', value)} />}</ModalPresence>
      <ModalPresence>{dialog?.kind === 'channel' && <Suspense fallback={<Loading />}><ChatChannelDrawer key={dialog.assistant.ID} assistant={assistants.data?.find(item => item.ID === dialog.assistant.ID) ?? dialog.assistant} onClose={closeDialog} /></Suspense>}</ModalPresence>
      <ModalPresence>{dialog?.kind === 'clone' && <CloneAssistant assistant={dialog.assistant} onClose={closeDialog} onCreated={value => go('assistants', value)} />}</ModalPresence>
      <ModalPresence>{dialog?.kind === 'dismiss' && <Confirm title={t.assistants.dismiss} description={t.assistants.dismissHelp} danger onClose={closeDialog} onConfirm={async () => { await api.mutate('/api/assistants/dismiss', { ID: dialog.assistant.ID }, dialog.assistant.Version); go('assistants'); }} />}</ModalPresence>

  </>;
}

function AssistantActivityPreview({ activity }: { activity?: AssistantActivity }) {
 const { t } = useCopy();
 return activity?.Dreaming ? <span className="assistant-dreaming-label">☾ {t.chat.dreaming}</span> : <AssistantMessagePreview activity={activity} />;
}
