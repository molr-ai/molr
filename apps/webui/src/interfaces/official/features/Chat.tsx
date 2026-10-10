import { AssistantNotifications } from '../../../notifications/AssistantNotifications';
import { weixinCopy } from '../../../weixin-copy';
import type { WeixinStatus } from '../../../api/types';
import { ShellTasksButton } from './ShellTasks';
import { ConversationCard } from './ConversationCard';
import { PermissionRequestCard } from './PermissionRequests';
import { TurnInformation } from './TurnInformation';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { AssistantAvatar, Alert, Button, ChatAttachmentList, ChatComposer, ChatMessage, ChatStatus, DropdownMenu, IconButton } from '@molr/ui';
import { ArrowLeft, Ellipsis, CalendarClock, Download, Zap } from 'lucide-react';
import { api, mutationKey } from '../../../api/client';
import type { AgentStatus, Assistant, Conversation, ExpediteStatus, FileInfo } from '../../../api/types';
import { useCopy, useData, useQuery } from '../../../state';
import { useChatHistory } from '../../../use-chat-history';
import { Empty, ErrorNotice, Loading, useAction } from '../components/common';

export type Draft = { text: string; files: FileInfo[] };
export type AssistantAction = 'channel' | 'info' | 'clone' | 'dismiss' | 'permissions' | 'tasks' | 'shell_tasks' | 'pin';
export function Chat({ assistant, conversation, timeZone, draft, updateDraft, onAction, onBack, onCard, onDebug, onAutomatic, targetMessageID = '', onClearTarget }: {
  targetMessageID?: string; onClearTarget: () => void;
  assistant: Assistant; conversation?: Conversation; timeZone?: string; draft: Draft; updateDraft: (draft: Draft | ((previous: Draft) => Draft)) => void;
  onAutomatic: () => void; onDebug?: (turnID?: string) => void; onAction: (action: AssistantAction) => void; onBack?: () => void; onCard: (kind: string, id: string) => void;
}) {
  const { t, locale } = useCopy(), { invalidate, invalidateTopics } = useData(); const action = useAction();
  const status = useQuery<AgentStatus>(assistant.State === 'active' ? `/api/assistants/${assistant.ID}/status` : null);
  const channel = useQuery<WeixinStatus>(`/api/assistants/${assistant.ID}/weixin`);
  const weixin = channel.data?.Channel === 'weixin';
  const chatReadOnly = !channel.data || weixin;
  const channelCopy = weixinCopy[locale];
  const history = useChatHistory(assistant.ConversationID, targetMessageID);
  const [expediteBusy, setExpediteBusy] = useState(false), [expediteFailed, setExpediteFailed] = useState(false), [requestedThrough, setRequestedThrough] = useState(0);
  useEffect(() => { setExpediteBusy(false); setExpediteFailed(false); setRequestedThrough(0); }, [assistant.ID]);
  const [uploading, setUploading] = useState(false), [notice, setNotice] = useState('');
  const log = useRef<HTMLDivElement>(null), bottom = useRef(true), savedScroll = useRef<{ top: number; height: number } | null>(null), readThrough = useRef(conversation?.LastReadSequence ?? 0);
  const targetElement = useRef<HTMLDivElement>(null), located = useRef('');
  const navigation = useRef(0);
  useLayoutEffect(() => {
    const changed = () => { navigation.current++; };
    window.addEventListener('hashchange', changed);
    return () => { changed(); window.removeEventListener('hashchange', changed); };
  }, []);
  const draftAttachments = useFilePreviews(draft.files);
  const archived = assistant.State === 'dismissed' || conversation?.Archived;
  const lastSequence = history.messages.at(-1)?.Sequence ?? 0;
  function markRead() {
    if (targetMessageID || !bottom.current || document.visibilityState !== 'visible' || !document.hasFocus() || document.querySelector('dialog[open]') || lastSequence <= Math.max(readThrough.current, conversation?.LastReadSequence ?? 0)) return;
    const previous = readThrough.current; readThrough.current = lastSequence;
    void api.post('/api/conversations/read', { ConversationID: assistant.ConversationID, ThroughSequence: lastSequence }).catch(() => { if (readThrough.current === lastSequence) readThrough.current = previous; });
  }
  useLayoutEffect(() => {
    const element = log.current; if (!element || !history.loaded || (history.loadingOlder && savedScroll.current)) return;
    if (targetMessageID) {
      bottom.current = false; savedScroll.current = null;
      if (history.targetStatus === 'found' && targetElement.current && located.current !== targetMessageID) {
        targetElement.current.scrollIntoView({ block: 'center', behavior: 'instant' });
        targetElement.current.focus({ preventScroll: true });
        located.current = targetMessageID;
      }
      return;
    }
    if (located.current) { located.current = ''; bottom.current = true; }
    if (savedScroll.current) { element.scrollTop = savedScroll.current.top + element.scrollHeight - savedScroll.current.height; savedScroll.current = null; }
    else if (bottom.current) element.scrollTop = element.scrollHeight;
  }, [history, targetMessageID]);
  useEffect(() => { markRead(); const visible = () => markRead(); document.addEventListener('visibilitychange', visible); window.addEventListener('focus', visible); return () => { document.removeEventListener('visibilitychange', visible); window.removeEventListener('focus', visible); }; }, [lastSequence, conversation?.LastReadSequence, targetMessageID]);
  async function send() {
    if (chatReadOnly || archived || uploading || action.busy || (!draft.text.trim() && !draft.files.length)) return;
    const sent = draft, startedNavigation = navigation.current, startedHash = location.hash;
    if (/^\/compress(?:\s|$)/.test(sent.text.trim())) {
      if (sent.text.trim() !== '/compress') { setNotice(t.chat.compressArguments); return; }
      await action.run(async () => {
        await api.mutate(`/api/assistants/${assistant.ID}/compress`, {});
        updateDraft(current => current === sent ? { ...sent, text: '' } : current); setNotice(''); status.reload();
      }, undefined, false);
      return;
    }
    await action.run(async () => {
      await api.mutate('/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: sent.text, AttachmentIDs: sent.files.map(file => file.ID) } });
      invalidateTopics([`messages:${assistant.ConversationID}`]);
      updateDraft(current => current === sent ? { text: '', files: [] } : current);
      // A mobile exit animation may keep this component mounted after navigation.
      if (navigation.current === startedNavigation && location.hash === startedHash) {
        bottom.current = true; onClearTarget();
      }
    }, undefined, false);
  }
  async function attach(files: File[]) {
    if (chatReadOnly || uploading || action.busy) return;
    if (draft.files.length + files.length > 5 || files.some(file => file.size > 20 * 1024 * 1024)) { setNotice(t.chat.uploadLimit); return; }
    setNotice(''); setUploading(true);
    const added: FileInfo[] = [];
    try { for (const file of files) added.push(await api.upload(file, mutationKey())); }
    catch { setNotice(t.chat.uploadError); }
    finally { if (added.length) updateDraft({ ...draft, files: [...draft.files, ...added] }); setUploading(false); }
  }
  const state = status.data?.State ?? 'idle'; const working = state === 'working', dreaming = state === 'dreaming', compressing = status.data?.Compression?.State === 'running';
  const latestUnread = [...history.messages].reverse().find(message => message.Sender === 'user' && !message.ReadAt && (!working || message.ID !== status.data?.TriggerMessageID) && message.Sequence > (status.data?.InterruptedThroughSequence ?? 0));
  const expedited = status.data?.Expedite;
  // Local coverage only bridges the request and its durable status update.
  // Keeping an old cutoff would cover an excluded, still-unread trigger later.
  useEffect(() => {
    if (requestedThrough && expedited && expedited.ThroughSequence >= requestedThrough) setRequestedThrough(0);
  }, [requestedThrough, expedited?.ThroughSequence, expedited?.State]);
  const pendingThrough = Math.max(requestedThrough, expedited && ['pending', 'waiting_tool', 'waiting'].includes(expedited.State) ? expedited.ThroughSequence : 0);
  const covered = !!latestUnread && latestUnread.Sequence <= pendingThrough;
  async function expedite() {
    if (!latestUnread || !working || expediteBusy || covered) return;
    const through = latestUnread.Sequence;
    setExpediteBusy(true); setExpediteFailed(false);
    try {
      const result = await api.mutate<ExpediteStatus>(`/api/assistants/${assistant.ID}/expedite`, { ConversationID: assistant.ConversationID, ThroughSequence: through });
      if (['pending', 'waiting_tool', 'waiting'].includes(result.State)) setRequestedThrough(through);
      status.reload();
    } catch { setExpediteFailed(true); }
    finally { setExpediteBusy(false); }
  }
  return <section className="chat-panel" aria-label={assistant.Name}>
    <header className="chat-header"><div className="chat-identity">{onBack && <IconButton label={t.assistants.back} onClick={onBack}><ArrowLeft size={20} /></IconButton>}<h1><button type="button" className="chat-assistant-info" aria-label={t.assistantInfo.open(assistant.Name)} onClick={() => onAction('info')}>{assistant.Name}</button></h1><ChatStatus compact status={working || dreaming ? 'acting' : state === 'waiting' ? 'waiting' : state === 'stopped' ? 'stopped' : 'idle'} label={archived ? t.chat.archived : compressing ? t.chat.compressing : t.chat[state]} /></div>
      <div className="form-actions"><ShellTasksButton assistantID={assistant.ID} disabled={archived} onOpen={() => onAction('shell_tasks')} /><IconButton label={t.nav.plans} onClick={() => onAction('tasks')}><CalendarClock size={20} /></IconButton><DropdownMenu label={t.more} align="end" trigger={<IconButton label={t.more}><Ellipsis size={20} /></IconButton>} items={[
        { id: 'plans', label: t.nav.plans, onSelect: onAutomatic },
        ...(onDebug ? [{ id: 'debug', label: t.workbench.debugAssistant, onSelect: onDebug }] : []),
        { id: 'channel', label: channelCopy.title, onSelect: () => onAction('channel') },
        { id: 'info', label: t.assistantInfo.title, onSelect: () => onAction('info') }, { id: 'pin', label: assistant.Pinned ? t.assistants.unpin : t.assistants.pin, disabled: archived, onSelect: () => onAction('pin') },
        { id: 'clone', label: t.assistants.clone, disabled: archived, separatorBefore: true, onSelect: () => onAction('clone') }, { id: 'dismiss', label: t.assistants.dismiss, disabled: archived, danger: true, onSelect: () => onAction('dismiss') },
      ]} /></div>
    </header>
    {archived && <Alert title={t.assistants.archived} />}
    <ErrorNotice error={action.error || history.error || status.error || channel.error} retry={action.error ? invalidate : history.error ? invalidate : status.error ? status.reload : channel.reload} />
    <AssistantNotifications status={status.data ?? undefined} assistant={assistant} onAction={onAction} onNavigate={onCard}/>
    {targetMessageID && <div className="chat-search-location" role="status"><span>{history.targetStatus === 'missing' ? t.chat.targetMissing : history.targetStatus === 'found' ? t.chat.targetFound : t.chat.targetLoading}</span><Button size="sm" variant="ghost" onClick={() => { bottom.current = true; onClearTarget(); }}>{t.chat.returnLatest}</Button></div>}
    <div className="chat-history" ref={log} role="log" aria-label={t.chat.message} aria-live="polite" onScroll={() => { const element = log.current; if (element) { bottom.current = element.scrollHeight - element.scrollTop - element.clientHeight < 48; markRead(); } }}>
      {history.more && <Button variant="ghost" loading={history.loadingOlder} disabled={!!targetMessageID && history.targetStatus === 'loading'} onClick={() => { if (log.current) savedScroll.current = { top: log.current.scrollTop, height: log.current.scrollHeight }; history.loadOlder(); }}>{t.loadOlder}</Button>}
      {history.loading && !history.loaded ? <Loading /> : !history.messages.length ? <Empty title={t.chat.noMessages} description={t.chat.noMessagesHelp} /> : history.messages.map(message => <div key={message.ID} data-message-id={message.ID} className={message.ID === targetMessageID && history.targetStatus === 'found' ? 'chat-message-target' : undefined} ref={message.ID === targetMessageID ? targetElement : undefined} tabIndex={message.ID === targetMessageID ? -1 : undefined} aria-label={message.ID === targetMessageID ? t.chat.targetFound : undefined}><ChatMessage avatar={message.Sender === 'assistant' ? <AssistantAvatar name={assistant.Name} avatar={assistant.Avatar} /> : undefined} role={message.Sender} sender={message.Sender === 'user' ? t.chat.you : assistant.Name} timestamp={message.CreatedAt} timeZone={timeZone} text={message.Content.Text} readStatus={message.Sender === 'user' ? message.ReadAt ? 'read' : 'unread' : undefined}
        cards={message.Content.Card || message.Content.AttachmentIDs?.length ? <>{message.Content.Card && (message.Content.Card.Kind === 'permission_request' ? <PermissionRequestCard requestID={message.Content.Card.TargetID} assistant={assistant} timeZone={timeZone} title={message.Content.Card.Title} summary={message.Content.Card.Summary} /> : <ConversationCard card={message.Content.Card} timeZone={timeZone} onOpen={onCard} />)}{message.Content.AttachmentIDs?.length ? <MessageAttachments ids={message.Content.AttachmentIDs} /> : null}</> : undefined}
        footer={message.Sender === 'user' ? !archived && !dreaming && state !== 'stopped' && state !== 'paused' && message.ID === latestUnread?.ID && (working || covered) ? <div className="chat-expedite">
          <button type="button" className="message-expedite-button" aria-label={covered || expediteBusy ? t.chat.expediting : expediteFailed ? t.chat.expediteFailed : t.chat.expedite} title={covered || expediteBusy ? t.chat.expediting : expediteFailed ? t.chat.expediteFailed : t.chat.expediteHelp} aria-busy={expediteBusy || undefined} disabled={covered || expediteBusy || !working} onClick={() => void expedite()}><Zap size={15} aria-hidden="true" /><span>{covered || expediteBusy ? t.chat.expediting : expediteFailed ? t.chat.expediteFailed : t.chat.expedite}</span></button>
          {covered && (expedited?.State === 'waiting_tool' || !working) && <small role="status">{working ? t.chat.expediteTool : t.chat.expediteWaiting}</small>}
        </div> : undefined : <TurnInformation message={message} revision={history.metadataVersions[message.ID] ?? 0} onDebug={onDebug} readOnly={!!archived} />} /></div>)}
    </div>
    {!channel.data ? <Loading /> : <div className="chat-input"><ChatComposer lock={weixin ? { title: channelCopy.readonly, description: channelCopy.readonlyHelp, action: <Button variant="secondary" size="sm" onClick={() => onAction('channel')}>{channelCopy.manage}</Button> } : undefined} commands={[{ name: 'compress', description: t.chat.compress, keywords: ['压缩', '上下文'], disabled: compressing }]} value={draft.text} onValueChange={text => updateDraft({ ...draft, text })} onSubmit={() => void send()} placeholder={t.chat.placeholder} disabled={chatReadOnly || archived || uploading || action.busy} attachments={draftAttachments}
      onAttachFiles={files => void attach(files)} onRemoveAttachment={id => updateDraft({ ...draft, files: draft.files.filter(file => file.ID !== id) })} notice={uploading ? t.chat.uploading : dreaming ? `${t.chat.dreaming}${status.data?.DreamDeadline ? ` · ${new Date(status.data.DreamDeadline).toLocaleTimeString()} ${locale.startsWith('zh') ? '前结束，消息排队等待' : 'deadline; messages queued'}` : ''}` : notice || (status.data?.Compression?.State === 'failed' ? t.chat.compressFailed : '')} processingLabel={compressing ? t.chat.compressing : dreaming ? t.chat.dreaming : undefined} sending={working || dreaming} allowSendWhileSending onStop={working || dreaming ? () => void action.run(() => api.post(`/api/assistants/${assistant.ID}/stop`), undefined, false) : undefined} />
    </div>}
  </section>;
}
export function Attachment({ id }: { id: string }) {
  const { t } = useCopy(), file = useQuery<FileInfo>(`/api/files/${id}`);
  return <div className="attachment"><ErrorNotice error={file.error} retry={file.reload} />{file.data && <a href={`/api/files/${encodeURIComponent(id)}/content`} download><Download size={16} />{file.data.Name}<small>{Math.ceil(file.data.Size / 1024)} KB</small></a>}{file.loading && !file.data && <span>{t.loading}</span>}</div>;
}

function chatAttachment(file: FileInfo, previewUrl?: string) {
  const url = `/api/files/${encodeURIComponent(file.ID)}/content`;
  return { id: file.ID, name: file.Name, size: file.Size, type: file.MediaType, url, previewUrl };
}

function MessageAttachments({ ids }: { ids: string[] }) {
  const { t } = useCopy();
  const [files, setFiles] = useState<FileInfo[]>([]), [error, setError] = useState<unknown>(null), [loading, setLoading] = useState(true), [attempt, setAttempt] = useState(0);
  const attachments = useFilePreviews(files);
  const key = JSON.stringify(ids);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(null); setFiles([]);
    void Promise.allSettled((JSON.parse(key) as string[]).map(id => api.get<FileInfo>(`/api/files/${encodeURIComponent(id)}`, controller.signal))).then(results => {
      if (controller.signal.aborted) return;
      setFiles(results.flatMap(result => result.status === 'fulfilled' ? [result.value] : []));
      setError(results.find(result => result.status === 'rejected')?.reason ?? null);
      setLoading(false);
    });
    return () => controller.abort();
  }, [key, attempt]);
  return <div><ErrorNotice error={error} retry={() => setAttempt(value => value + 1)} />{loading && <span>{t.loading}</span>}<ChatAttachmentList files={attachments} /></div>;
}

// File downloads are deliberately served as octet-stream. Use typed, temporary
// blob URLs for image rendering without changing the safe download endpoint.
function useFilePreviews(files: FileInfo[]) {
  type Preview = { type: string; controller: AbortController; url?: string; failed?: boolean };
  const entries = useRef(new Map<string, Preview>());
  const [previews, setPreviews] = useState<Record<string, { type: string; url?: string }>>({});
  const key = JSON.stringify(files.filter(file => file.MediaType.startsWith('image/')).map(file => [file.ID, file.MediaType]));
  useEffect(() => {
    const current = entries.current;
    return () => { for (const entry of current.values()) { entry.controller.abort(); if (entry.url) URL.revokeObjectURL(entry.url); } current.clear(); };
  }, []);
  useEffect(() => {
    const current = entries.current, wanted = new Map(JSON.parse(key) as [string, string][]);
    const publish = () => setPreviews(Object.fromEntries([...current].map(([id, entry]) => [id, { type: entry.type, url: entry.url ?? (entry.failed ? 'data:image/png;base64,' : undefined) }])));
    for (const [id, entry] of current) {
      if (wanted.get(id) === entry.type && !entry.failed) continue;
      entry.controller.abort(); if (entry.url) URL.revokeObjectURL(entry.url); current.delete(id);
    }
    for (const [id, type] of wanted) {
      if (current.has(id)) continue;
      const controller = new AbortController(), entry: Preview = { type, controller };
      current.set(id, entry);
      void api.download(`/api/files/${encodeURIComponent(id)}/content`, controller.signal).then(blob => {
        if (controller.signal.aborted) return;
        entry.url = URL.createObjectURL(new Blob([blob], { type })); publish();
      }).catch(() => {
        if (!controller.signal.aborted) { entry.failed = true; publish(); }
      });
    }
    publish();
  }, [key]);
  return files.map(file => chatAttachment(file, previews[file.ID]?.type === file.MediaType ? previews[file.ID].url : undefined));
}
