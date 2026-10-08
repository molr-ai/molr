import { FeedbackDesignExample } from './FeedbackDesignExample';
import { attachmentPreview } from './attachment-preview';
import { productMessages } from './i18n';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { FileText, ListTodo, Download, RotateCcw } from 'lucide-react';
import { useBreakpoint, Badge, Button, ChatCard, ChatComposer, ChatMessage, ChatMessageMeta, ChatStatus, Drawer, Progress, SegmentedControl, Switch, useUILocale } from '@molr/ui';
import type { ChatAttachment, ChatComposerProps, ChatStatusKind } from '@molr/ui';
import { ComponentExample as Example } from './ComponentExample';
import { ChatAttachmentExamples } from './ChatAttachmentExamples';
import './chat-showcase.css';
import { ChatPermissionExample } from './ChatPermissionExample';
import { ChatCommandExample } from './ChatCommandExample';
import { ChatRichTextExample } from './ChatRichTextExample';

let sequence = 0;
const nextId = () => `chat-example-${++sequence}`;
const demoMetrics = { inputTokens: 128, outputTokens: 86, firstTokenMs: 320, tokensPerSecond: 42.6, model: 'Molr Demo' };
function useCopy() { const { locale } = useUILocale(); return productMessages[locale]; }
function useDraft(active: boolean) {
  const t = useCopy();
  const [value, setValue] = useState(''); const [attachments, setAttachments] = useState<ChatAttachment[]>([]);
  const [model, setModel] = useState('standard'); const [recording, setRecording] = useState(false); const [notice, setNotice] = useState('');
  useEffect(() => { if (!active) setRecording(false); }, [active]);
  async function attach(files: File[]) {
    const accepted = files.filter(file => file.size <= 20 * 1024 * 1024);
    const added = await Promise.all(accepted.slice(0, 5).map(async file => ({ id: nextId(), name: file.name, size: file.size, type: file.type, previewUrl: await attachmentPreview(file) })));
    setAttachments(current => [...current, ...added].slice(0, 5));
    setNotice(accepted.length !== files.length || attachments.length + accepted.length > 5 ? t.common.upTo5Attachments20MbEach : t.chatShowcase.fileMetadataOnlyContentsAreNotUploaded);
  }
  const props: Omit<ChatComposerProps, 'onSubmit'> = {
    commands: [{ name: 'compress', description: t.chatShowcase.commandSelector.command, keywords: ['压缩', '上下文'] }],
    value, onValueChange: setValue, attachments, onAttachFiles: attach, onRemoveAttachment: id => setAttachments(current => current.filter(file => file.id !== id)),
    model, onModelChange: setModel, models: [{ value: 'standard', label: t.common.standardDemo }, { value: 'fast', label: t.common.fastDemo }],
    recording, onVoiceInput: () => { if (recording) { attach([new File(['audio demo'], 'voice-demo.webm', { type: 'audio/webm' })]); setNotice(t.chatShowcase.audioPlaceholderAttachedNoAudioWasRecordedOrTranscribed); } else setNotice(t.chatShowcase.recordingStatePreviewTheMicrophoneIsNotUsedPress); setRecording(current => !current); },
    notice, hint: t.chatShowcase.enterToSendShiftEnterForANewLine,
  };
  const handleCommand = () => { if (value.trim() !== '/compress') return false; setValue(''); setNotice(t.chatShowcase.commandSelector.done); return true; };
  return { handleCommand, props, value, attachments, setValue, setAttachments, setNotice, setRecording, clear: () => { setValue(''); setAttachments([]); setNotice(''); } };
}

type DemoMessage = { id: string; role: 'user' | 'assistant'; text: string; attachments?: ChatAttachment[]; feedback?: 'up' | 'down'; readStatus?: 'read' | 'unread' };
function ConversationPreview({ active }: { active: boolean }) {
  const t = useCopy(); const draft = useDraft(active);
  const [messages, setMessages] = useState<DemoMessage[]>([]);
  const [pending, setPending] = useState<{ id: string; replyTo: string; text: string; length: number } | null>(null);
  const log = useRef<HTMLDivElement>(null); const atBottom = useRef(true);
  useEffect(() => {
    if (!pending || !active) return;
    const timer = setTimeout(() => {
      const length = Math.min(pending.length + 4, pending.text.length);
      setMessages(current => current.map(message => message.id === pending.id ? { ...message, text: pending.text.slice(0, length) } : message.id === pending.replyTo ? { ...message, readStatus: 'read' } : message));
      setPending(length === pending.text.length ? null : { ...pending, length });
    }, 75);
    return () => clearTimeout(timer);
  }, [pending, active]);
  useLayoutEffect(() => { if (active && atBottom.current && log.current) log.current.scrollTop = log.current.scrollHeight; }, [messages, active]);
  function send() {
    if (draft.handleCommand()) return;
    const id = nextId(); const replyTo = nextId();
    setMessages(current => [...current, { id: replyTo, role: 'user', readStatus: 'unread', text: draft.value.trim(), attachments: draft.attachments }, { id, role: 'assistant', text: '' }]);
    setPending({ id, replyTo, length: 0, text: t.chatShowcase.gotItYouCanKeepAddingRequestsInThe });
    atBottom.current = true; draft.clear();
  }
  function stop() {
    if (!pending) return;
    setMessages(current => current.map(message => message.id === pending.id && !message.text ? { ...message, text: t.chatShowcase.generationStopped } : message));
    setPending(null); draft.setNotice(t.chatShowcase.generationStoppedYouCanSendAnotherMessage);
  }
  return <div className="chat-preview">
    <header className="chat-preview__header"><div><span className="chat-preview__avatar" aria-hidden="true">M</span><div><strong>Molr</strong><ChatStatus status={pending ? 'typing' : 'idle'} compact live="off" /></div></div><Badge>{t.chatShowcase.interactiveDemo}</Badge></header>
    <div className="chat-preview__log" role="log" aria-label={t.chatShowcase.exampleConversation} ref={log} onScroll={() => { const el = log.current!; atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 36; }}>
      <ChatMessage role="user" readStatus="read" sender={t.common.you} text={t.chatShowcase.helpMePlanARelaxedOutingForSaturday} />
      <ChatMessage sender="Molr" text={t.chatShowcase.ofCourseAddYourPreferencesHereWheneverYouLike} cards={<ChatCard title={t.chatShowcase.weekendOuting} eyebrow={t.chatShowcase.taskRecord024} status={{ label: t.chatShowcase.inProgress }} metadata={t.chatShowcase.value2SourcesNoDeliverableYet} />} />
      {messages.filter(message => message.text || message.attachments?.length).map(message => <ChatMessage key={message.id} role={message.role} readStatus={message.readStatus} sender={message.role === 'user' ? t.common.you : 'Molr'} text={message.text} attachments={message.attachments} footer={message.role === 'assistant' && message.text && pending?.id !== message.id && <ChatMessageMeta feedback={message.feedback} onFeedback={feedback => setMessages(current => current.map(item => item.id === message.id ? { ...item, feedback: item.feedback === feedback ? undefined : feedback } : item))} metricsNote={t.chatShowcase.simulatedConversationNoLiveModelMeasurements} />} />)}
      {pending && <ChatStatus status="typing" />}
    </div>
    <div className="chat-preview__input"><ChatComposer {...draft.props} onSubmit={send} sending={!!pending} onStop={stop} /></div>
  </div>;
}

function StatusExample() {
  const t = useCopy();
  const [status, setStatus] = useState<ChatStatusKind>('acting');
  const [compact, setCompact] = useState(false);
  const states: { value: ChatStatusKind; label: string; description: string }[] = [
    { value: 'acting', label: t.chatShowcase.action, description: t.chatShowcase.browsingFiles },
    { value: 'thinking', label: t.chatShowcase.thinking, description: t.chatShowcase.consideringYourRequest },
    { value: 'typing', label: t.chatShowcase.typing, description: t.chatShowcase.preparingAReply },
    { value: 'waiting', label: t.chatShowcase.waiting, description: t.chatShowcase.waitingForYouToConfirmAccess },
    { value: 'complete', label: t.chatShowcase.complete, description: t.chatShowcase.yourDeliverableIsReadyToView },
    { value: 'error', label: t.chatShowcase.error, description: t.chatShowcase.unableToReadTheFileCheckTheConnectionAnd },
    { value: 'idle', label: t.common.idle, description: t.chatShowcase.readyWheneverYouWantToContinue },
  ];
  return <Example title={t.chatShowcase.assistantStatus} description={t.chatShowcase.aStandaloneIndicatorForActionsThinkingAndTypingIcons} wide>
    <div className="chat-status-controls"><SegmentedControl label={t.chatShowcase.statusType} hideLabel value={status} onValueChange={setStatus} options={states.map(({ value, label }) => ({ value, label }))} /><Switch label={t.chatShowcase.compactDisplay} checked={compact} onChange={event => setCompact(event.target.checked)} /></div>
    <div className="chat-status-preview"><ChatStatus status={status} description={states.find(item => item.value === status)!.description} compact={compact} /></div>
  </Example>;
}

export function ChatShowcase({ active }: { active: boolean }) {
  const t = useCopy(); const draft = useDraft(active);
  const [showAvatar, setShowAvatar] = useState(true); const [showSender, setShowSender] = useState(true); const [showInfo, setShowInfo] = useState(true); const [feedback, setFeedback] = useState<'up' | 'down'>();
  const [locked, setLocked] = useState(false);
  const [disabled, setDisabled] = useState(false); const [sent, setSent] = useState<{ text: string; files: ChatAttachment[] } | null>(null);
  const [progress, setProgress] = useState(50); const [drawer, setDrawer] = useState<'task' | 'file' | null>(null);
  const narrow = useBreakpoint('mobile');
  useEffect(() => { if (!active) setDrawer(null); }, [active]);
  const fileTitle = t.chatShowcase.weekendChecklistMd;
  const content = t.chatShowcase.weekendChecklistMorningAParkWalkWithTimeTo;
  const download = <a className="button button--secondary button--sm" href={`data:text/markdown;charset=utf-8,${encodeURIComponent(content)}`} download={fileTitle}><Download size={14} />{t.chatShowcase.downloadSample}</a>;
  return <div className="component-library__grid chat-showcase">
    <FeedbackDesignExample />
    <ChatRichTextExample />
    <Example title={t.chatShowcase.conversationPreview} description={t.chatShowcase.trySendingFollowingUpAndStoppingAReplyThese} wide><ConversationPreview active={active} /></Example>
    <Example title={t.chatShowcase.messageBubbles} description={t.chatShowcase.borderless16PxCornersAndTextOnlyInformationStays}>
      <div className="chat-example-options"><Switch label={t.chatShowcase.showAvatars} checked={showAvatar} onChange={event => setShowAvatar(event.target.checked)} /><Switch label={t.chatShowcase.showSender} checked={showSender} onChange={event => setShowSender(event.target.checked)} /><Switch label={t.chatShowcase.showInformationIcon} checked={showInfo} onChange={event => setShowInfo(event.target.checked)} /></div>
      <div className="chat-example-messages"><ChatMessage role="user" readStatus="read" sender={t.common.you} showSender={showSender} showAvatar={showAvatar} text={t.chatShowcase.keepWalkingToAMinimumAndLeaveSomeFree} /><ChatMessage sender="Molr" showSender={showSender} showAvatar={showAvatar} text={t.chatShowcase.understoodIWillKeepTheRouteWithinOneArea} footer={<ChatMessageMeta metrics={demoMetrics} metricsNote={t.chatShowcase.sampleMeasurementsToDemonstrateTheLayout} showMetrics={showInfo} feedback={feedback} onFeedback={value => setFeedback(current => current === value ? undefined : value)} />} /></div>
    </Example>
    <Example id="message-composer" title={t.chatShowcase.messageComposer} description={t.chatShowcase.startsAtOneLineAndGrowsWithContentAttach}>
      <Switch label={t.chatShowcase.disableComposer} checked={disabled} onChange={event => { setDisabled(event.target.checked); draft.setRecording(false); }} />
      <Switch label={t.chatShowcase.lockComposer} checked={locked} onChange={event => { setLocked(event.target.checked); draft.setRecording(false); }} />
      <ChatComposer {...draft.props} lock={locked ? { title: t.chatShowcase.composerLocked, description: t.chatShowcase.composerLockedHelp, action: <Button variant="secondary" size="sm" onClick={() => setLocked(false)}>{t.chatShowcase.unlockComposer}</Button> } : undefined} disabled={disabled} onSubmit={() => { if (draft.handleCommand()) return; setSent({ text: draft.value, files: draft.attachments }); draft.clear(); }} />
      {sent ? <div className="chat-sent-preview" role="status"><small>{t.chatShowcase.lastSampleMessage}</small><ChatMessage role="user" readStatus="unread" text={sent.text} attachments={sent.files} /></div> : <p className="component-copy">{t.chatShowcase.tryMultipleLinesOrAttachAFileTheVoice}</p>}
    </Example>
    <ChatCommandExample active={active} />
    <StatusExample />
    <Example id="message-cards" title={t.chatShowcase.messageCards} description={t.chatShowcase.cardsSitOutsideBubblesAndShowTaskProgressOr} wide>
      <div className="chat-card-examples">
      <ChatCard title={t.chatShowcase.planAWeekendOuting} eyebrow={t.chatShowcase.task024} icon={<ListTodo />} status={{ label: progress === 100 ? t.chatShowcase.complete2 : t.chatShowcase.inProgress2, tone: progress === 100 ? 'positive' : 'neutral' }} details={[{label:t.chatShowcase.cardDetails.assistant,value:"Molr"},{label:t.chatShowcase.cardDetails.sources,value:t.chatShowcase.cardDetails.twoSources}]} actions={<Button size="sm" variant="secondary" onClick={() => setDrawer('task')}>{t.chatShowcase.viewTask}</Button>}><Progress label={t.chatShowcase.taskProgress} value={progress} /></ChatCard>
      <ChatCard title={fileTitle} eyebrow={t.chatShowcase.deliverableV1} icon={<FileText />} description={t.chatShowcase.aShortItineraryAndPackingChecklistReadyToPreview} status={{ label: t.chatShowcase.ready, tone: 'positive' }} details={[{label:t.chatShowcase.cardDetails.format,value:"Markdown"},{label:t.chatShowcase.cardDetails.size,value:`${new Blob([content]).size} B`}]} actions={<><Button size="sm" variant="secondary" onClick={() => setDrawer('file')}>{t.chatShowcase.previewDeliverable}</Button>{download}</>} />
      <ChatPermissionExample active={active} />
      </div>
    </Example>
    <ChatAttachmentExamples />
    <Drawer title={drawer === 'task' ? t.chatShowcase.taskDetails : t.chatShowcase.deliverablePreview} open={!!drawer} onOpenChange={open => { if (!open) setDrawer(null); }} side={narrow ? 'bottom' : 'right'} footer={drawer === 'file' ? download : <Button size="sm" variant="secondary" onClick={() => setProgress(value => value === 100 ? 0 : Math.min(100, value + 25))}>{progress === 100 ? <RotateCcw size={14} /> : null}{progress === 100 ? t.chatShowcase.resetSample : t.chatShowcase.advanceSample}</Button>}>
      {drawer === 'task' ? <div className="chat-task-preview"><p>{t.chatShowcase.prepareARelaxedWeekendPlanWithARouteBreaks}</p><Progress label={t.chatShowcase.taskProgress} value={progress} /><ol><li>{t.chatShowcase.reviewOutingPreferences}</li><li>{t.chatShowcase.prepareTheRouteAndTransport}</li><li>{t.chatShowcase.createAndCheckTheChecklist}</li></ol><p className="component-copy">{t.chatShowcase.theSampleButtonAdvancesProgressWithoutExecutingAReal}</p></div> : <pre className="chat-file-preview">{content}</pre>}
    </Drawer>
  </div>;
}
