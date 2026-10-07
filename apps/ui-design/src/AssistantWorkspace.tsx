import { productMessages } from './i18n';
import { useEffect, useId, useRef, useState } from 'react';
import type { TouchEvent } from 'react';
import { useBreakpoint, Button, Dialog, useToast } from '@molr/ui';
import type { InterfaceDemo } from './interface-demo';
import type { Locale } from './messages';
import { AssistantSidebar } from './AssistantSidebar';
import { AssistantChat } from './AssistantChat';
import { AssistantActionDialog } from './AssistantActionDialog';
import type { AssistantAction } from './AssistantActionDialog';

export function AssistantWorkspace({ demo, locale, active, mobile, mobileConversation, onMobileConversationChange, onSearch, openTasks, openAccess, openSchedule }: {
  demo: InterfaceDemo; locale: Locale; active: boolean; mobile: boolean; onSearch: () => void;
  mobileConversation: boolean; onMobileConversationChange: (value: boolean) => void;
  openTasks: () => void; openAccess: () => void; openSchedule: () => void;
}) {
  const t = productMessages[locale];
  const { toast } = useToast();
  const narrow = useBreakpoint('compact');
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [editing, setEditing] = useState<{ id: string; name: string } | null>(null);
  const [action, setAction] = useState<AssistantAction | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const root = useRef<HTMLDivElement>(null);
  const swipe = useRef<{ x: number; y: number; time: number } | null>(null);
  const sidebarId = useId();
  const nameId = useId();
  const compact = mobile || narrow;
  useEffect(() => {
    const closeOverlays = () => { setEditing(null); setAction(null); setSidebarOpen(false); };
    if (!active) closeOverlays();
    return closeOverlays;
  }, [active]);
  function enterConversation() {
    setSidebarOpen(false);
    if (mobile) onMobileConversationChange(true);
    requestAnimationFrame(() => root.current?.querySelector<HTMLElement>(mobile ? '.conversation-back' : '.message-composer textarea')?.focus({ preventScroll: true }));
  }
  function backToList() {
    onMobileConversationChange(false);
    requestAnimationFrame(() => {
      const selected = [...(root.current?.querySelectorAll<HTMLButtonElement>('[data-assistant-id]') ?? [])].find(button => button.dataset.assistantId === demo.activeAssistantId);
      selected?.focus({ preventScroll: true });
    });
  }
  function startSwipe(event: TouchEvent<HTMLDivElement>) {
    swipe.current = null;
    if (!mobile || !mobileConversation || !active || editing || action || event.touches.length !== 1 || !(event.target instanceof Element) || event.target.closest('input, textarea, select, button, a, dialog, [popover], [contenteditable="true"], .message-composer')) return;
    const touch = event.touches[0];
    swipe.current = { x: touch.clientX, y: touch.clientY, time: performance.now() };
  }
  function moveSwipe(event: TouchEvent<HTMLDivElement>) {
    if (!swipe.current) return;
    if (event.touches.length !== 1) { swipe.current = null; return; }
    const dx = Math.abs(event.touches[0].clientX - swipe.current.x), dy = Math.abs(event.touches[0].clientY - swipe.current.y);
    if (dy > 12 && dy > dx) swipe.current = null;
  }
  function endSwipe(event: TouchEvent<HTMLDivElement>) {
    const start = swipe.current;
    swipe.current = null;
    if (!start || !active || !mobileConversation || !event.changedTouches.length) return;
    const touch = event.changedTouches[0], dx = touch.clientX - start.x, dy = Math.abs(touch.clientY - start.y);
    if (dx >= 64 && dx > dy * 1.5 && performance.now() - start.time < 1000) backToList();
  }
  const sidebar = <AssistantSidebar id={sidebarId} demo={demo} locale={locale} onSearch={onSearch} mobile={mobile} hidden={!mobile && compact && !sidebarOpen}
    onSelect={id => { demo.selectAssistant(id); enterConversation(); }} />;
  const chat = <AssistantChat key={demo.activeAssistantId} demo={demo} locale={locale} active={active && !editing && !action && (!mobile || mobileConversation)} openTasks={openTasks} openAccess={openAccess} openSchedule={openSchedule}
      onRename={() => setEditing({ id: demo.activeAssistant.id, name: demo.activeAssistant.name })}
      onPin={() => toast({ title: t.assistantWorkspace.pinConversationPreview, description: t.assistantWorkspace.pinnedConversationsAppearAtTheTopOfTheAssistant })}
      onClone={() => setAction({ kind: 'clone', name: demo.activeAssistant.name })}
      onDismiss={() => setAction({ kind: 'dismiss', name: demo.activeAssistant.name })}
      onBack={mobile ? backToList : undefined}
      sidebar={!mobile && compact ? { id: sidebarId, open: sidebarOpen, onToggle: () => setSidebarOpen(value => !value) } : undefined} />;
  return <div ref={root} className={`conversation-workspace${compact ? ' conversation-workspace--compact' : ''}${mobile ? ' conversation-workspace--mobile' : ''}`} data-mobile-page={mobile ? mobileConversation ? 'chat' : 'list' : undefined}>
    {mobile ? <div className="mobile-conversation-track">
      <div className="mobile-assistant-page" aria-hidden={mobileConversation} inert={mobileConversation}>{sidebar}</div>
      <div className="mobile-chat-page" aria-hidden={!mobileConversation} inert={!mobileConversation} onTouchStart={startSwipe} onTouchMove={moveSwipe} onTouchEnd={endSwipe} onTouchCancel={() => { swipe.current = null; }}>{chat}</div>
    </div> : <>{sidebar}{chat}</>}
    <Dialog open={active && !!editing} onOpenChange={open => { if (!open) setEditing(null); }} title={t.common.renameAssistant} initialFocusRef={input} size="sm">
      <form className="assistant-editor" onSubmit={event => { event.preventDefault(); if (!editing?.name.trim()) return; demo.renameAssistant(editing.id, editing.name); setEditing(null); }}>
        <label htmlFor={nameId}>{t.assistantWorkspace.assistantName}</label>
        <input autoComplete="off" ref={input} id={nameId} className="input" required maxLength={30} value={editing?.name ?? ''} onChange={event => setEditing(current => current ? { ...current, name: event.target.value } : current)} />
        <div className="ip-actions"><Button type="submit" size="sm" disabled={!editing?.name.trim()}>{t.assistantWorkspace.saveName}</Button><Button variant="ghost" size="sm" onClick={() => setEditing(null)}>{t.common.cancel}</Button></div>
      </form>
    </Dialog>
    <AssistantActionDialog action={active ? action : null} locale={locale} onClose={() => setAction(null)} />
  </div>;
}
