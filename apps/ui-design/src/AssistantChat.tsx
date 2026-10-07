import { planMessages } from './plan-messages';
import { productMessages } from './i18n';
import { useEffect, useRef, useState } from 'react';
import { ArrowLeft, CalendarDays, Copy, Ellipsis, ListTodo, PanelLeft, Pencil, Pin, Shield, UserMinus, X } from 'lucide-react';
import { Button, ChatCard, ChatStatus, DropdownMenu, IconButton } from '@molr/ui';
import type { Locale } from './messages';
import type { InterfaceDemo } from './interface-demo';
import { MessageComposer } from './MessageComposer';
import { ChatMessageView } from './ChatMessageView';
import { statusLabels } from './interface-demo';

export function AssistantChat({ demo, locale, openTasks, openAccess, openSchedule, onRename, onPin, onClone, onDismiss, onBack, sidebar, active }: { demo: InterfaceDemo; locale: Locale; openTasks: () => void; openAccess: () => void; openSchedule: () => void; onRename: () => void; onPin: () => void; onClone: () => void; onDismiss: () => void; onBack?: () => void; sidebar?: { id: string; open: boolean; onToggle: () => void }; active: boolean }) {
  const t = productMessages[locale];
  const log = useRef<HTMLDivElement>(null); const atBottom = useRef(true); const assistant = demo.activeAssistant;
  const [documentVisible, setDocumentVisible] = useState(document.visibilityState !== 'hidden');
  const task = demo.works.find(item => item.id === assistant.reference);
  const running = demo.works.find(item => ['running', 'verifying'].includes(item.status));
  const waiting = demo.works.some(item => ['blocked', 'budget', 'failed'].includes(item.status));
  const status = !demo.connected ? t.assistantChat.disconnected : assistant.pendingReply ? t.assistantChat.typing : running ? running.status === 'verifying' ? t.assistantChat.verifyingResults : running.stage < 2 ? t.assistantChat.browsingFilesToolCall : t.assistantChat.preparingContent : waiting ? t.assistantChat.waitingForYou : t.common.idle;
  const statusKind = !demo.connected ? 'error' : assistant.pendingReply ? 'typing' : running ? (running.status === 'verifying' || running.stage < 2 ? 'acting' : 'thinking') : waiting ? 'waiting' : 'idle';
  useEffect(() => { const change = () => setDocumentVisible(document.visibilityState !== 'hidden'); document.addEventListener('visibilitychange', change); return () => document.removeEventListener('visibilitychange', change); }, []);
  useEffect(() => { if (active && documentVisible && atBottom.current) { if (log.current) log.current.scrollTop = log.current.scrollHeight; demo.markAssistantRead(assistant.id); } }, [active, documentVisible, assistant.id, assistant.messages.length]);
  return <div className="assistant-chat">
    <header className="conversation-header">
      {onBack && <IconButton className="conversation-back" size="sm" label={t.assistantChat.backToAssistants} onClick={onBack}><ArrowLeft size={18} /></IconButton>}
      {sidebar && <IconButton className="conversation-sidebar-toggle" size="sm" label={t.common.assistantList} aria-expanded={sidebar.open} aria-controls={sidebar.id} onClick={sidebar.onToggle}><PanelLeft size={18} /></IconButton>}
      <div className="conversation-identity">
        <div className="conversation-identity__text"><h3 title={assistant.name}>{assistant.name}</h3><ChatStatus className="conversation-status" status={statusKind} label={status} compact /></div>
      </div>
      <div className="conversation-header__actions">
        <Button size="sm" variant="ghost" onClick={openTasks} aria-label={planMessages[locale].title}><ListTodo size={16} /><span className="conversation-action-label">{planMessages[locale].title}</span><small>{demo.works.length}</small></Button>
        <DropdownMenu align="end" label={t.assistantChat.moreActions} trigger={<IconButton size="sm" label={t.common.more}><Ellipsis size={18} /></IconButton>} items={[
          { id: 'rename', label: t.common.renameAssistant, icon: <Pencil size={16} />, onSelect: onRename },
          { id: 'pin', label: t.assistantChat.pinConversation, icon: <Pin size={16} />, onSelect: onPin },
          { id: 'clone', label: t.common.cloneAssistant, icon: <Copy size={16} />, onSelect: onClone },
          { id: 'permissions', label: t.assistantChat.permissions, icon: <Shield size={16} />, onSelect: openAccess },
          { id: 'schedule', label: planMessages[locale].title, icon: <CalendarDays size={16} />, onSelect: openSchedule },
          { id: 'dismiss', label: t.common.dismissAssistant, icon: <UserMinus size={16} />, danger: true, separatorBefore: true, onSelect: onDismiss },
        ]} />
      </div>
    </header>
    <div className="assistant-chat__messages" ref={log} role="log" aria-label={t.assistantChat.assistantConversation} onScroll={() => { const el = log.current; if (!el) return; atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 36; if (atBottom.current && active && documentVisible) demo.markAssistantRead(assistant.id); }}>
      {!assistant.messages.length && <p className="assistant-empty">{t.assistantChat.sayHelloOrAskThisAssistantToHelpWith}</p>}
      {assistant.messages.map(message => {
        const related = demo.works.find(w => w.id === message.taskId);
        return <ChatMessageView key={message.id} message={message} sender={message.role === 'user' ? t.common.you : assistant.name} locale={locale} preferences={demo.interfacePreferences} onFeedback={feedback => demo.setMessageFeedback(message.id, feedback)} cards={related && <ChatCard className="message-task-card" eyebrow={`${t.assistantChat.taskRecord} · #${related.id}`} title={locale === 'zh' ? related.title : related.titleEn} status={{ label: statusLabels[locale][related.status], tone: related.status === 'complete' ? 'positive' : related.status === 'failed' ? 'danger' : ['blocked', 'budget'].includes(related.status) ? 'warning' : 'neutral' }} metadata={`v${related.revisions.length + 1}${related.artifact ? t.assistantChat.deliverableReady : ''}`} />} />;
      })}
    </div>
    <div className="conversation-input">
      {task && <div className="conversation-reference"><span>{t.assistantChat.discussing} #{task.id} · {locale === 'zh' ? task.title : task.titleEn}</span><button type="button" aria-label={t.assistantChat.clearTaskReference} onClick={() => demo.patchAssistant({ reference: null })}><X size={15} /></button></div>}
      <MessageComposer key={assistant.id} assistant={assistant} demo={demo} locale={locale} referenced={!!task} />
    </div>
  </div>;
}
