import { Activity, useLayoutEffect, useRef, useState } from 'react';
import type { MouseEvent } from 'react';
import { AssistantAvatar, Badge, Button, ChatMessage, DropdownMenu, EmptyState, IconButton, ModalPresence } from '@molr/ui';
import { ArrowLeft, Ellipsis } from 'lucide-react';
import type { AgentStatus, Assistant, Conversation, Message, Page } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { useMutation } from '../../../use-mutation';
import { Confirm, Loading } from '../components/common';
import './assistant-conversations.css';

type ConversationSummary = Conversation & { Preview?: { Text: string; CreatedAt: string; Kind: string } | null };

export function AssistantConversations({ assistant }: { assistant: Assistant }) {
  const { t, locale } = useCopy(), zh = locale === 'zh';
  const conversations = useQuery<ConversationSummary[]>(`/api/assistants/${assistant.ID}/conversations`, true, 0);
  const all = useQuery<Assistant[]>('/api/assistants?include_temporary=true', true, 0);
  const [selected, setSelected] = useState<string | null>(null);
  const [dismiss, setDismiss] = useState<Assistant | null>(null);
  const end = useMutation([dismiss?.ID, dismiss?.Version]);
  const container = useRef<HTMLDivElement>(null), lastRow = useRef<HTMLButtonElement | null>(null), listScroll = useRef(0);
  const detailsTitle = useRef<HTMLHeadingElement>(null);
  useErrorToast(conversations.error, conversations.reload); useErrorToast(all.error, all.reload);
  const names = new Map((all.data ?? []).map(a => [a.ID, a.Name]));
  const chats = [...(conversations.data ?? [])].sort((a, b) => (Date.parse(b.Preview?.CreatedAt ?? '') || 0) - (Date.parse(a.Preview?.CreatedAt ?? '') || 0) || a.ID.localeCompare(b.ID));
  const chat = chats.find(c => c.ID === selected);
  const peerID = chat ? (chat.PeerID === assistant.ID ? chat.PartnerID : chat.PeerID) : undefined;
  const peer = all.data?.find(a => a.ID === peerID);
  const name = peer?.Name ?? (zh ? '助理' : 'Assistant');
  function open(event: MouseEvent<HTMLButtonElement>, id: string) {
    lastRow.current = event.currentTarget;
    listScroll.current = container.current?.closest('.ui-dialog__body')?.scrollTop ?? 0;
    setSelected(id);
  }
  useLayoutEffect(() => {
    const scroll = container.current?.closest('.ui-dialog__body');
    if (scroll) scroll.scrollTop = selected ? 0 : listScroll.current;
    if (selected) detailsTitle.current?.focus({ preventScroll: true });
    else lastRow.current?.focus({ preventScroll: true });
  }, [selected]);
  return <div ref={container} className="assistant-conversations">
    <Activity mode={selected ? 'hidden' : 'visible'}>
      {conversations.loading && !conversations.data && <Loading />}
      {!conversations.loading && !chats.length && <EmptyState title={zh ? '暂无助理间对话' : 'No assistant conversations yet'} />}
      <ul className="assistant-conversation-list" aria-label={zh ? '助理间对话' : 'Assistant conversations'}>{chats.map(c => {
        const id = c.PeerID === assistant.ID ? c.PartnerID : c.PeerID;
        const other = all.data?.find(a => a.ID === id);
        return <li key={c.ID}><button type="button" className="assistant-conversation-row" onClick={event => open(event, c.ID)}>
          <span className="ui-assistant-avatar" aria-hidden="true"><AssistantAvatar name={other?.Name ?? name} avatar={other?.Avatar} /></span>
          <span className="assistant-conversation-copy">
            <span className="assistant-conversation-row-heading"><strong title={other?.Name}>{other?.Name ?? (zh ? '助理' : 'Assistant')}</strong>
              {c.Preview?.CreatedAt && <time className="assistant-conversation-time" dateTime={c.Preview.CreatedAt}>{new Intl.DateTimeFormat(locale === 'zh' ? 'zh-CN' : 'en-US', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(c.Preview.CreatedAt))}</time>}
            </span>
            <span className="assistant-conversation-preview">{c.Preview?.Text || (zh ? '暂无文字消息' : 'No text message')}</span>
            {other?.EmployerID && <span className="assistant-conversation-status"><TemporaryStatus assistant={other} /></span>}
          </span>
        </button></li>;
      })}</ul>
    </Activity>
    {selected && chat && <>
      <div className="assistant-conversation-toolbar">
        <IconButton label={zh ? '返回对话列表' : 'Back to conversations'} onClick={() => setSelected(null)}><ArrowLeft size={18} /></IconButton>
        <span className="ui-assistant-avatar" aria-hidden="true"><AssistantAvatar name={name} avatar={peer?.Avatar} /></span>
        <div className="assistant-conversation-heading"><h3 ref={detailsTitle} tabIndex={-1}>{name}</h3>{peer?.EmployerID && <TemporaryStatus assistant={peer} />}</div>
        {peer?.EmployerID === assistant.ID && peer.State === 'active' && <DropdownMenu label={t.more} align="end" trigger={<IconButton label={t.more}><Ellipsis size={18} /></IconButton>} items={[{ id: 'dismiss', label: zh ? '结束雇佣' : 'End employment', danger: true, onSelect: () => setDismiss(peer) }]} />}
      </div>
      <CollaborationHistory key={chat.ID} conversation={chat} names={names} assistantID={assistant.ID} />
    </>}
    <ModalPresence>{dismiss && <Confirm danger title={zh ? '结束临时雇佣' : 'End temporary employment'} description={zh ? `停止 ${dismiss.Name} 的执行并保留对话及已交付文件。` : `Stop ${dismiss.Name} and retain conversation history and delivered files.`} onClose={() => setDismiss(null)} onConfirm={async () => { await end('/api/assistants/dismiss', { ID: dismiss.ID }, dismiss.Version); }} />}</ModalPresence>
  </div>;
}

function TemporaryStatus({ assistant }: { assistant: Assistant }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const status = useQuery<AgentStatus>(assistant.State === 'active' ? `/api/assistants/${assistant.ID}/status` : null, false, 0);
  useErrorToast(status.error, status.reload);
  const states: Record<string, string> = zh ? { working: '处理中', idle: '空闲', waiting: '等待中', paused: '已暂停', stopped: '已停止', dreaming: '整理记忆中' } : { working: 'Working', idle: 'Idle', waiting: 'Waiting', paused: 'Paused', stopped: 'Stopped', dreaming: 'Organizing memory' };
  const label = assistant.State === 'dismissed' ? (zh ? '已结束' : 'Ended') : states[status.data?.State ?? ''] ?? (zh ? '在职' : 'Active');
  return <span className="assistant-conversation-tags">
    <Badge size="sm" tone={assistant.State === 'active' ? 'positive' : 'neutral'}>{label}</Badge>
    <Badge size="sm" tone="neutral" dot={false}>{zh ? '临时助理' : 'Temporary assistant'}</Badge>
  </span>;
}

function CollaborationHistory({ conversation, names, assistantID }: { conversation: Conversation; names: Map<string, string>; assistantID: string }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const [cursor, setCursor] = useState(''), [cursors, setCursors] = useState<string[]>([]);
  const query = useQuery<Page<Message>>(`/api/conversations/${conversation.ID}/messages?order=desc&limit=50${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`, false, 0);
  useErrorToast(query.error, query.reload);
  return <section className="assistant-conversation-history" aria-label={zh ? '对话历史' : 'Conversation history'}>
    {query.loading && !query.data && <Loading />}
    {(query.data?.NextCursor || cursors.length > 0) && <div className="form-actions">
      <Button size="sm" variant="ghost" disabled={!query.data?.NextCursor || query.loading} onClick={() => { setCursors(old => [...old, cursor]); setCursor(query.data!.NextCursor); }}>{zh ? '更早消息' : 'Older messages'}</Button>
      <Button size="sm" variant="ghost" disabled={!cursors.length || query.loading} onClick={() => { setCursor(cursors.at(-1) ?? ''); setCursors(old => old.slice(0, -1)); }}>{zh ? '更新消息' : 'Newer messages'}</Button>
    </div>}
    {[...(query.data?.Items ?? [])].reverse().map(m => <ChatMessage key={m.ID} className={m.SenderID === assistantID ? 'assistant-conversation-message--employer' : undefined} role="assistant" showAvatar={false} sender={names.get(m.SenderID ?? '') ?? m.SenderID} timestamp={m.CreatedAt} text={m.Content.Text || m.Content.Card?.Summary || m.Content.Card?.Title || ''} footer={<>{m.Content.AttachmentIDs?.map(id => <AttachmentLink key={id} id={id} />)}</>} />)}
    {!query.loading && !query.data?.Items?.length && <EmptyState title={zh ? '暂无消息' : 'No messages yet'} />}
  </section>;
}
function AttachmentLink({ id }: { id: string }) {
  const { locale } = useCopy();
  const file = useQuery<{ Name: string }>(`/api/files/${id}`, false, 0);
  useErrorToast(file.error, file.reload);
  return <a href={`/api/files/${encodeURIComponent(id)}/content`} download>{file.data?.Name ?? (locale === 'zh' ? '下载附件' : 'Download attachment')}</a>;
}
