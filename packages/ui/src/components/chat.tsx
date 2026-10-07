import { ChatFeedback } from './ChatFeedback';
import { useId, useLayoutEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useUILocale } from '../locale';
import { Badge, Button } from './ui';
import type { BadgeProps } from './ui';
import { UIIcon } from './icons';
import { Popover } from './Popover';
import { Select } from './Select';
import { ComposerCommandMenu } from './ComposerCommandMenu';
import { ImageGallery } from './ImageGallery';
import { ChatMarkdown } from './ChatMarkdown';
import { ChatCopyButton } from './ChatCopyButton';
import { formatChatTime } from '../chat-time';

export type ChatAttachment = { id: string; name: string; size: number; type?: string; previewUrl?: string; url?: string };
export type ChatBubbleProps = { text: string; role?: 'assistant' | 'user'; className?: string };
export function ChatBubble({ text, role = 'assistant', className = '' }: ChatBubbleProps) {
  return <div className={`ui-chat-bubble ui-chat-bubble--${role} message-content ${className}`}><ChatMarkdown text={text} /></div>;
}

export type ChatAttachmentListProps = { files: readonly ChatAttachment[]; onRemove?: (id: string) => void; disabled?: boolean; className?: string };
export function ChatAttachmentList({ files, onRemove, disabled, className = '' }: ChatAttachmentListProps) {
  const { messages } = useUILocale();
  if (!files.length) return null;
  const images = files.filter(file => file.type?.startsWith('image/') && file.previewUrl);
  const documents = files.filter(file => !images.includes(file));
  return <div className={className}>
    <ImageGallery images={images.map(file => ({ id: file.id, src: file.previewUrl!, alt: file.name, downloadUrl: file.url }))} onRemove={onRemove} disabled={disabled} />
    {!!documents.length && <ul className="ui-chat-attachments" aria-label={messages.chat.attachments}>{documents.map(file => <li key={file.id}>
      <UIIcon name="paperclip" />{file.url ? <a href={file.url} download>{file.name}</a> : <span>{file.name}</span>}<small>{file.size >= 1024 * 1024 ? `${(file.size / (1024 * 1024)).toFixed(1)} MB` : `${Math.max(1, Math.round(file.size / 1024))} KB`}</small>
      {onRemove && <button type="button" disabled={disabled} aria-label={messages.chat.removeAttachment(file.name)} onClick={() => onRemove(file.id)}><UIIcon name="close" /></button>}
    </li>)}</ul>}
  </div>;
}

export type ChatMessageProps = ChatBubbleProps & { sender?: string; timestamp?: string; timeZone?: string; showSender?: boolean; showAvatar?: boolean; avatar?: ReactNode; attachments?: readonly ChatAttachment[]; images?: readonly { src: string; alt: string }[]; cards?: ReactNode; footer?: ReactNode; readStatus?: 'read' | 'unread' };
export function ChatMessage({ text, role = 'assistant', sender, timestamp, timeZone, showSender = true, showAvatar = true, avatar, attachments, images, cards, footer, readStatus, className = '' }: ChatMessageProps) {
  const { locale, messages } = useUILocale();
  const name = sender?.trim() || (role === 'user' ? messages.chat.you : messages.chat.assistant);
  const time = timestamp ? formatChatTime(timestamp, locale, timeZone) : undefined;
  const timeElement = time && <time className="message-time" dateTime={timestamp} title={time.full}>{time.text}</time>;
  return <article className={`ui-chat-message ui-chat-message--${role} ${className}`}>
    {showAvatar && <span className="ui-chat-avatar" role="img" aria-label={messages.chat.avatar(name)}>{avatar ?? Array.from(name)[0].toUpperCase()}</span>}
    <div className="ui-chat-message__body">
      {((showSender && sender) || time) && <div className="message-heading">
        {role === 'user' && timeElement}
        {showSender && sender && <small className="message-sender" title={sender}>{sender}</small>}
        {role !== 'user' && timeElement}
      </div>}
      {text && <ChatBubble role={role} text={text} />}
      {attachments && <ChatAttachmentList files={attachments} className="message-attachments" />}
      {!!images?.length && <ImageGallery images={images} className="message-images" />}
      {cards && <div className="message-cards">{cards}</div>}
      {(text || footer || (role === 'user' && readStatus)) && <div className="message-actions">
        {text && <ChatCopyButton text={text} />}{footer}
        {role === 'user' && readStatus && <small className="message-read-status" data-read-status={readStatus}>{readStatus === 'read' ? messages.chat.read : messages.chat.unread}</small>}
      </div>}
    </div>
  </article>;
}

export type ChatCardDetail = { label: ReactNode; value: ReactNode };
export type ChatCardProps = { title: string; eyebrow?: string; icon?: ReactNode; description?: string; status?: { label: string; tone?: BadgeProps['tone'] }; details?: ChatCardDetail[]; children?: ReactNode; metadata?: ReactNode; actions?: ReactNode; className?: string };
export function ChatCard({ title, eyebrow, icon, description, status, details, children, metadata, actions, className = '' }: ChatCardProps) {
  return <article className={`ui-chat-card ${className}`} aria-label={title}>
    {(eyebrow || status) && <div className="ui-chat-card__heading">{eyebrow && <small>{eyebrow}</small>}{status && <Badge tone={status.tone}>{status.label}</Badge>}</div>}
    <h4>{icon && <span aria-hidden="true">{icon}</span>}{title}</h4>
    {description && <p>{description}</p>}
    {!!details?.length && <dl className="ui-chat-card__details">{details.map((detail, index) => <div key={index}><dt>{detail.label}</dt><dd>{detail.value}</dd></div>)}</dl>}
    {children}
    {(metadata || actions) && <footer>{metadata && <small>{metadata}</small>}{actions && <div className="ui-chat-card__actions">{actions}</div>}</footer>}
  </article>;
}

export type ChatMetrics = { inputTokens?: number; outputTokens?: number; firstTokenMs?: number; tokensPerSecond?: number; model?: string };
export type ChatMessageMetaProps = { metricsContent?: ReactNode; metricsTitle?: string; metricsOpen?: boolean; onMetricsOpenChange?: (open: boolean) => void; metrics?: ChatMetrics; metricsNote?: string; showMetrics?: boolean; showFeedback?: boolean; feedback?: 'up' | 'down'; feedbackPending?: 'up' | 'down'; onFeedback?: (value: 'up' | 'down') => void };
export function ChatMessageMeta({ metricsContent, metricsTitle, metricsOpen, onMetricsOpenChange, metrics = {}, metricsNote, showMetrics = true, showFeedback = true, feedback, feedbackPending, onFeedback }: ChatMessageMetaProps) {
  const { locale, messages } = useUILocale();
  const metric = (value: number | undefined) => value !== undefined && Number.isFinite(value) && value >= 0 ? value.toLocaleString(locale === 'zh' ? 'zh-CN' : 'en-US') : '—';
  if (!showMetrics && !(showFeedback && onFeedback)) return null;
  return <div className="message-footer">
    {showMetrics && <Popover align="start" maxHeight={metricsContent === undefined ? undefined : 600} open={metricsOpen} onOpenChange={onMetricsOpenChange} title={metricsTitle ?? messages.chat.messageInformation} trigger={<button type="button" className="message-info-button" aria-label={metricsTitle ?? messages.chat.messageInformation}><UIIcon name="info" /></button>}>
      {metricsContent ?? <><dl className="message-info-details"><dt>{messages.chat.inputTokens}</dt><dd>{metric(metrics.inputTokens)}</dd><dt>{messages.chat.outputTokens}</dt><dd>{metric(metrics.outputTokens)}</dd><dt>{messages.chat.firstTokenLatency}</dt><dd>{metric(metrics.firstTokenMs)} ms</dd><dt>{messages.chat.decodeSpeed}</dt><dd>{metric(metrics.tokensPerSecond)} tok/s</dd>{metrics.model && <><dt>{messages.chat.model}</dt><dd>{metrics.model}</dd></>}</dl>
      {metricsNote && <p className="message-info-note">{metricsNote}</p>}</>}
    </Popover>}
    {showFeedback && onFeedback && <ChatFeedback value={feedback} pending={feedbackPending} onChange={onFeedback} />}
  </div>;
}

export type ChatStatusKind = 'acting' | 'thinking' | 'typing' | 'waiting' | 'complete' | 'error' | 'idle' | 'stopped';
export type ChatStatusProps = { status: ChatStatusKind; label?: string; description?: string; compact?: boolean; live?: 'polite' | 'off'; className?: string };
export function ChatStatus({ status, label, description, compact = false, live = 'polite', className = '' }: ChatStatusProps) {
  const { messages } = useUILocale();
  const labels = messages.chat.status;
  const icons = { acting: 'tool', thinking: 'sparkles', waiting: 'clock', complete: 'check', error: 'warning' } as const;
  const active = status === 'acting' || status === 'thinking' || status === 'typing';
  return <div className={`ui-chat-status${compact ? ' ui-chat-status--compact' : ''} ${className}`} role="status" aria-live={live} aria-atomic="true" data-status={status} data-active={active}>
    <span className="ui-chat-status__icon" aria-hidden="true">{status === 'typing' ? <span className="ui-chat-status__dots"><i /><i /><i /></span> : (status === 'idle' || status === 'stopped') ? <span className="ui-chat-status__dot" /> : <UIIcon name={icons[status]} />}</span>
    <div className="ui-chat-status__copy"><span className="ui-chat-status__label">{label ?? labels[status]}</span>{description && <small>{description}</small>}</div>
  </div>;
}

export function ChatTypingIndicator({ label }: { label?: string }) {
  return <ChatStatus status="typing" label={label} />;
}

export type ChatComposerCommand = { name: string; description: string; keywords?: readonly string[]; disabled?: boolean };
export type ChatComposerProps = {
  commands?: readonly ChatComposerCommand[];
  value: string; onValueChange: (value: string) => void; onSubmit: () => void; label?: string; placeholder?: string; maxLength?: number;
  attachments?: readonly ChatAttachment[]; onAttachFiles?: (files: File[]) => void; onRemoveAttachment?: (id: string) => void;
  models?: readonly { value: string; label: string }[]; model?: string; onModelChange?: (model: string) => void;
  onVoiceInput?: () => void; recording?: boolean; disabled?: boolean; sendDisabled?: boolean; sending?: boolean; allowSendWhileSending?: boolean; onStop?: () => void;
  hint?: string; notice?: string; processingLabel?: string; className?: string;
};
export function ChatComposer({ commands = [], value, onValueChange, onSubmit, label, placeholder, maxLength = 4000, attachments = [], onAttachFiles, onRemoveAttachment, models, model, onModelChange, onVoiceInput, recording = false, disabled = false, sendDisabled = false, sending = false, allowSendWhileSending = false, onStop, hint, notice, processingLabel, className = '' }: ChatComposerProps) {
  const { messages } = useUILocale();
  const id = useId(); const textarea = useRef<HTMLTextAreaElement>(null); const fileInput = useRef<HTMLInputElement>(null);
  useLayoutEffect(() => { if (textarea.current) resizeComposer(textarea.current); }, [value]);
  useLayoutEffect(() => {
    const el = textarea.current; if (!el) return;
    let width = el.clientWidth, frame = 0;
    const observer = new ResizeObserver(() => {
      if (el.clientWidth !== width) {
        width = el.clientWidth;
        cancelAnimationFrame(frame);
        frame = requestAnimationFrame(() => resizeComposer(el));
      }
    });
    observer.observe(el); return () => { observer.disconnect(); cancelAnimationFrame(frame); };
  }, []);
  const [focused, setFocused] = useState(false), [dismissed, setDismissed] = useState<string | null>(null), [activeCommand, setActiveCommand] = useState(0), [caret, setCaret] = useState(0);
  useLayoutEffect(() => { if (textarea.current && document.activeElement === textarea.current) setCaret(textarea.current.selectionStart); }, [value]);
  const query = /^\/([^\s/]*)$/.exec(value)?.[1];
  const commandMatches = query === undefined ? [] : commands.filter(command => [command.name, ...(command.keywords ?? [])].some(word => word.toLocaleLowerCase().includes(query.toLocaleLowerCase())));
  const commandMenu = focused && !disabled && !recording && commands.length > 0 && query !== undefined && caret === value.length && dismissed !== value;
  const selectedCommand = Math.min(activeCommand, commandMatches.length - 1);
  function completeCommand(index: number) {
    const command = commandMatches[index]; if (!command || command.disabled) return;
    const text = `/${command.name}`; onValueChange(text); setDismissed(text); setCaret(text.length);
    requestAnimationFrame(() => { textarea.current?.focus(); textarea.current?.setSelectionRange(text.length, text.length); });
  }
  const cannotSend = disabled || sendDisabled || recording || (sending && !allowSendWhileSending) || (!value.trim() && !attachments.length);
  function submit() { if (!cannotSend) onSubmit(); }
  return <div className={`message-composer-wrap ${className}`}><form className="message-composer" aria-label={messages.chat.messageComposer} onClick={event => {
    if (disabled || event.defaultPrevented || !(event.target instanceof Element) || event.target.closest('button, input, textarea, select, .select, a, label, [role="button"], [contenteditable="true"]')) return;
    textarea.current?.focus();
  }} onSubmit={event => { event.preventDefault(); submit(); }}>
    <ChatAttachmentList files={attachments} onRemove={onRemoveAttachment} disabled={disabled || recording} className="composer-attachments" />
    {commandMenu && <ComposerCommandMenu id={id} anchor={textarea} commands={commandMatches} selected={selectedCommand} onActive={setActiveCommand} onSelect={completeCommand} onDismiss={() => setDismissed(value)} />}
    <textarea autoComplete="off" aria-autocomplete={commands.length ? 'list' : undefined} aria-controls={commandMenu ? `${id}-commands` : undefined} aria-activedescendant={commandMenu && selectedCommand >= 0 ? `${id}-command-${selectedCommand}` : undefined}
      onFocus={event => { setFocused(true); setDismissed(null); setCaret(event.currentTarget.selectionStart); }} onBlur={() => setFocused(false)} onSelect={event => setCaret(event.currentTarget.selectionStart)} ref={textarea} id={`${id}-message`} rows={1} aria-label={label ?? messages.chat.messageYourAssistant} aria-describedby={[hint && `${id}-hint`, notice && `${id}-notice`].filter(Boolean).join(' ') || undefined} disabled={disabled} value={value} maxLength={maxLength} onChange={event => { onValueChange(event.target.value); setDismissed(null); setActiveCommand(0); setCaret(event.target.selectionStart); }} onPaste={event => {
      if (disabled || recording || !onAttachFiles) return;
      const files = Array.from(event.clipboardData.items).filter(item => item.kind === 'file' && item.type.startsWith('image/')).map(item => item.getAsFile()).filter((file): file is File => !!file);
      if (!files.length) return;
      // Preserve normal text insertion when a clipboard also contains text.
      if (!event.clipboardData.getData('text/plain')) event.preventDefault();
      onAttachFiles(files);
    }} onKeyDown={event => {
      if (event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) return;
      if (commandMenu) {
        if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setDismissed(value); return; }
        if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && commandMatches.length) { event.preventDefault(); setActiveCommand((selectedCommand + (event.key === 'ArrowDown' ? 1 : -1) + commandMatches.length) % commandMatches.length); return; }
        if ((event.key === 'Tab' && !event.shiftKey || event.key === 'Enter' && !event.shiftKey && value !== `/${commandMatches[selectedCommand]?.name}`) && commandMatches.length) { event.preventDefault(); completeCommand(selectedCommand); return; }
      }
      if (event.key === 'Enter'  && !event.shiftKey && !event.nativeEvent.isComposing && event.nativeEvent.keyCode !== 229) { event.preventDefault(); submit(); } }} placeholder={placeholder ?? messages.chat.messagePlaceholder} />
    <div className="message-composer__tools"><div>{onAttachFiles && <><input ref={fileInput} type="file" multiple hidden disabled={disabled || recording} onChange={event => { onAttachFiles(Array.from(event.target.files ?? [])); event.target.value = ''; }} /><button type="button" className="composer-icon" aria-label={messages.chat.attachFiles} disabled={disabled || recording} onClick={() => fileInput.current?.click()}><UIIcon name="paperclip" /></button></>}</div><div className="message-composer__right">
      {models && <Select className="composer-model" label={messages.chat.selectModel} hideLabel size="sm" align="end" disabled={disabled || recording} value={model ?? models[0]?.value ?? ''} onValueChange={value => onModelChange?.(value)} options={models} />}
      {onVoiceInput && <button type="button" className="composer-icon ui-icon-button" disabled={disabled} aria-label={recording ? messages.chat.stopRecording : messages.chat.voiceInput} aria-pressed={recording} onClick={onVoiceInput}><UIIcon name={recording ? 'stop' : 'mic'} /></button>}
      {sending && onStop && <Button variant="ghost" size="sm" type="button" className="composer-processing" aria-label={messages.chat.pauseExecution} title={messages.chat.pauseExecution} disabled={disabled} onClick={onStop}><span className="composer-processing__icons" aria-hidden="true"><span className="ui-spinner" /><UIIcon name="stop" /></span><span className="composer-processing__labels" aria-hidden="true"><span className="composer-processing__working">{processingLabel ?? messages.chat.processing}</span><span className="composer-processing__stop">{messages.chat.stopExecution}</span></span></Button>}
      <Button size="sm" type="submit" aria-label={messages.chat.send} disabled={cannotSend}><UIIcon name="arrow-up" /></Button>
    </div></div>
  </form>{notice && <p id={`${id}-notice`} className="composer-notice" role="status">{notice}</p>}{hint && <p id={`${id}-hint`} className="composer-hint">{hint}</p>}</div>;
}

function resizeComposer(el: HTMLTextAreaElement) {
  el.style.height = '0px';
  el.style.height = `${el.value ? Math.min(180, Math.max(28, el.scrollHeight)) : 28}px`;
  el.style.overflowY = el.scrollHeight > 180 ? 'auto' : 'hidden';
}
