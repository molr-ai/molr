import { useChatChannel } from './use-chat-channel';
import { ChatComposerContainer } from './ChatComposerContainer';
import { useFilePreviews } from './use-file-previews';
import { FolderOpen } from 'lucide-react';
import { useErrorToast } from '../../../error-toasts';
import { AssistantNotifications } from '../../../notifications/AssistantNotifications';
import { BrowserButton, TerminalButton, ShellTasksButton } from './ChatTools';
import { ConversationCard } from './ConversationCard';
import { PermissionRequestCard } from './PermissionRequests';
import { TurnInformation } from './TurnInformation';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import {
  AssistantAvatar,
  Alert,
  Button,
  ChatAttachmentList,
  ChatMessage,
  ChatStatus,
  DropdownMenu,
  IconButton,
} from '@molr/ui';
import {
  ArrowDown,
  ArrowLeft,
  Ellipsis,
  CalendarClock,
  Download,
  Zap,
} from 'lucide-react';
import { api } from '@molr/sdk';
import type {
  AgentStatus,
  Assistant,
  Conversation,
  ExpediteStatus,
  FileInfo,
} from '@molr/sdk';
import { useCopy, useData, useQuery, useSession } from '../../../state';
import { useChatHistory } from '../../../use-chat-history';
import { isChatObscured, onChatRevealed } from '../../../chat-visibility';
import { Empty, Loading } from '../components/common';

export type AssistantAction =
  | 'channel'
  | 'info'
  | 'clone'
  | 'dismiss'
  | 'permissions'
  | 'tasks'
  | 'shell_tasks'
  | 'browser'
  | 'files'
  | 'terminal'
  | 'pin';
export function Chat({
  assistant,
  conversation,
  timeZone,
  onAction,
  onBack,
  onCard,
  onDebug,
  targetMessageID = '',
  onClearTarget,
}: {
  targetMessageID?: string;
  onClearTarget: () => void;
  assistant: Assistant;
  conversation?: Conversation;
  timeZone?: string;
  onDebug?: (turnID?: string) => void;
  onAction: (action: AssistantAction) => void;
  onBack?: () => void;
  onCard: (kind: string, id: string) => void;
}) {
  const { t, locale } = useCopy(),
    { invalidate } = useData();
  const { principal } = useSession(),
    user = principal?.User;
  const userName = user?.DisplayName || user?.Username || 'U';
  const status = useQuery<AgentStatus>(
    assistant.State === 'active'
      ? `/api/assistants/${assistant.ID}/status`
      : null,
  );
  const channel = useChatChannel(assistant.ID, locale);
  const channelCopy = channel.copy;
  const history = useChatHistory(assistant.ConversationID, targetMessageID);
  const [expediteBusy, setExpediteBusy] = useState(false),
    [expediteFailed, setExpediteFailed] = useState(false),
    [requestedThrough, setRequestedThrough] = useState(0);
  useEffect(() => {
    setExpediteBusy(false);
    setExpediteFailed(false);
    setRequestedThrough(0);
  }, [assistant.ID]);
  const log = useRef<HTMLDivElement>(null),
    bottom = useRef(true),
    savedScroll = useRef<{ top: number; height: number } | null>(null),
    readThrough = useRef(conversation?.LastReadSequence ?? 0);
  const [showLatest, setShowLatest] = useState(false);
  const targetElement = useRef<HTMLDivElement>(null),
    located = useRef('');
  const archived = assistant.State === 'dismissed' || conversation?.Archived;
  const lastSequence = history.messages.at(-1)?.Sequence ?? 0;
  useErrorToast(
    history.error || status.error || channel.error,
    history.error ? invalidate : status.error ? status.reload : channel.reload,
  );
  function markRead() {
    if (
      targetMessageID ||
      !bottom.current ||
      document.visibilityState !== 'visible' ||
      !document.hasFocus() ||
      isChatObscured() ||
      lastSequence <=
        Math.max(readThrough.current, conversation?.LastReadSequence ?? 0)
    )
      return;
    const previous = readThrough.current;
    readThrough.current = lastSequence;
    void api
      .post('/api/conversations/read', {
        ConversationID: assistant.ConversationID,
        ThroughSequence: lastSequence,
      })
      .catch(() => {
        if (readThrough.current === lastSequence)
          readThrough.current = previous;
      });
  }
  function updateScrollPosition() {
    const element = log.current;
    if (!element) return;
    const distance =
      element.scrollHeight - element.scrollTop - element.clientHeight;
    bottom.current = distance < 48;
    setShowLatest(distance > Math.max(600, element.clientHeight));
  }
  function returnToLatest() {
    savedScroll.current = null;
    bottom.current = true;
    onClearTarget();
    log.current?.scrollTo({
      top: log.current.scrollHeight,
      behavior: 'instant',
    });
    log.current?.focus({ preventScroll: true });
    updateScrollPosition();
    markRead();
  }
  useEffect(() => {
    const element = log.current;
    if (!element) return;
    const observer = new ResizeObserver(updateScrollPosition);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  useLayoutEffect(() => {
    const element = log.current;
    if (
      !element ||
      !history.loaded ||
      (history.loadingOlder && savedScroll.current)
    )
      return;
    if (targetMessageID) {
      bottom.current = false;
      savedScroll.current = null;
      if (
        history.targetStatus === 'found' &&
        targetElement.current &&
        located.current !== targetMessageID
      ) {
        targetElement.current.scrollIntoView({
          block: 'center',
          behavior: 'instant',
        });
        targetElement.current.focus({ preventScroll: true });
        located.current = targetMessageID;
      }
      updateScrollPosition();
      return;
    }
    if (located.current) {
      located.current = '';
      bottom.current = true;
    }
    if (savedScroll.current) {
      element.scrollTop =
        savedScroll.current.top +
        element.scrollHeight -
        savedScroll.current.height;
      savedScroll.current = null;
    } else if (bottom.current) element.scrollTop = element.scrollHeight;
    updateScrollPosition();
  }, [
    history.messages,
    history.metadataVersions,
    history.loaded,
    history.loadingOlder,
    history.targetStatus,
    targetMessageID,
  ]);
  useEffect(() => {
    markRead();
    const visible = () => markRead(),
      stopObserving = onChatRevealed(visible);
    document.addEventListener('visibilitychange', visible);
    window.addEventListener('focus', visible);
    return () => {
      stopObserving();
      document.removeEventListener('visibilitychange', visible);
      window.removeEventListener('focus', visible);
    };
  }, [lastSequence, conversation?.LastReadSequence, targetMessageID]);
  const state = status.data?.State ?? 'idle';
  const working = state === 'working',
    dreaming = state === 'dreaming',
    compressing = status.data?.Compression?.State === 'running';
  const latestUnread = [...history.messages]
    .reverse()
    .find(
      (message) =>
        message.Sender === 'user' &&
        !message.ReadAt &&
        (!working || message.ID !== status.data?.TriggerMessageID) &&
        message.Sequence > (status.data?.InterruptedThroughSequence ?? 0),
    );
  const expedited = status.data?.Expedite;
  // Local coverage only bridges the request and its durable status update.
  // Keeping an old cutoff would cover an excluded, still-unread trigger later.
  useEffect(() => {
    if (
      requestedThrough &&
      expedited &&
      expedited.ThroughSequence >= requestedThrough
    )
      setRequestedThrough(0);
  }, [requestedThrough, expedited?.ThroughSequence, expedited?.State]);
  const pendingThrough = Math.max(
    requestedThrough,
    expedited &&
      ['pending', 'waiting_tool', 'waiting'].includes(expedited.State)
      ? expedited.ThroughSequence
      : 0,
  );
  const covered = !!latestUnread && latestUnread.Sequence <= pendingThrough;
  async function expedite() {
    if (!latestUnread || !working || expediteBusy || covered) return;
    const through = latestUnread.Sequence;
    setExpediteBusy(true);
    setExpediteFailed(false);
    try {
      const result = await api.mutate<ExpediteStatus>(
        `/api/assistants/${assistant.ID}/expedite`,
        { ConversationID: assistant.ConversationID, ThroughSequence: through },
      );
      if (['pending', 'waiting_tool', 'waiting'].includes(result.State))
        setRequestedThrough(through);
      status.reload();
    } catch {
      setExpediteFailed(true);
    } finally {
      setExpediteBusy(false);
    }
  }
  return (
    <section className="chat-panel" aria-label={assistant.Name}>
      <header className="chat-header">
        <div className="chat-identity">
          {onBack && (
            <IconButton label={t.assistants.back} onClick={onBack}>
              <ArrowLeft size={20} />
            </IconButton>
          )}
          <h1>
            <button
              type="button"
              className="chat-assistant-info"
              aria-label={t.assistantInfo.open(assistant.Name)}
              onClick={() => onAction('info')}
            >
              {assistant.Name}
            </button>
          </h1>
          <ChatStatus
            compact
            status={
              working || dreaming
                ? 'acting'
                : state === 'waiting'
                  ? 'waiting'
                  : state === 'stopped'
                    ? 'stopped'
                    : 'idle'
            }
            label={
              archived
                ? t.chat.archived
                : compressing
                  ? t.chat.compressing
                  : state === 'idle' && assistant.Status
                    ? assistant.Status
                    : t.chat[state]
            }
          />
        </div>
        <div className="form-actions">
          <IconButton
            label={locale === 'zh' ? '资源管理器' : 'File explorer'}
            disabled={archived}
            onClick={() => onAction('files')}
          >
            <FolderOpen size={20} />
          </IconButton>
          <BrowserButton
            assistantID={assistant.ID}
            disabled={archived}
            onOpen={() => onAction('browser')}
          />
          <TerminalButton
            assistantID={assistant.ID}
            disabled={archived}
            onOpen={() => onAction('terminal')}
          />
          <ShellTasksButton
            assistantID={assistant.ID}
            disabled={archived}
            onOpen={() => onAction('shell_tasks')}
          />
          <IconButton label={t.nav.plans} onClick={() => onAction('tasks')}>
            <CalendarClock size={20} />
          </IconButton>
          <DropdownMenu
            label={t.more}
            align="end"
            trigger={
              <IconButton label={t.more}>
                <Ellipsis size={20} />
              </IconButton>
            }
            items={[
              ...(onDebug
                ? [
                    {
                      id: 'debug',
                      label: t.workbench.debugAssistant,
                      onSelect: onDebug,
                    },
                  ]
                : []),
              {
                id: 'info',
                label: t.assistantInfo.title,
                onSelect: () => onAction('info'),
              },
              {
                id: 'pin',
                label: assistant.Pinned ? t.assistants.unpin : t.assistants.pin,
                disabled: archived,
                onSelect: () => onAction('pin'),
              },
              {
                id: 'channel',
                label: channelCopy.switchChannel,
                onSelect: () => onAction('channel'),
              },
              {
                id: 'clone',
                label: t.assistants.clone,
                disabled: archived,
                separatorBefore: true,
                onSelect: () => onAction('clone'),
              },
              {
                id: 'dismiss',
                label: t.assistants.dismiss,
                disabled: archived,
                danger: true,
                onSelect: () => onAction('dismiss'),
              },
            ]}
          />
        </div>
      </header>
      {archived && <Alert title={t.assistants.archived} />}
      <AssistantNotifications
        status={status.data ?? undefined}
        assistant={assistant}
        onAction={onAction}
        onNavigate={onCard}
      />
      {targetMessageID && (
        <div className="chat-search-location" role="status">
          <span>
            {history.targetStatus === 'missing'
              ? t.chat.targetMissing
              : history.targetStatus === 'found'
                ? t.chat.targetFound
                : t.chat.targetLoading}
          </span>
          <Button size="sm" variant="ghost" onClick={returnToLatest}>
            {(conversation?.UnreadCount ?? 0) > 0
              ? t.chat.newMessages
              : t.chat.returnLatest}
          </Button>
        </div>
      )}
      <div className="chat-history-region">
        <div
          className="chat-history"
          ref={log}
          role="log"
          tabIndex={-1}
          aria-label={t.chat.message}
          aria-live="polite"
          onScroll={() => {
            updateScrollPosition();
            markRead();
          }}
        >
          {history.more && (
            <div className="chat-history-divider">
              <Button
                className="chat-history-divider__action"
                variant="ghost"
                size="sm"
                loading={history.loadingOlder}
                disabled={
                  !!targetMessageID && history.targetStatus === 'loading'
                }
                onClick={() => {
                  if (log.current)
                    savedScroll.current = {
                      top: log.current.scrollTop,
                      height: log.current.scrollHeight,
                    };
                  history.loadOlder();
                }}
              >
                {t.loadOlder}
              </Button>
            </div>
          )}
          {history.loading && !history.loaded ? (
            <Loading />
          ) : !history.messages.length ? (
            <Empty
              title={t.chat.noMessages}
              description={t.chat.noMessagesHelp}
            />
          ) : (
            history.messages.map((message) => (
              <div
                key={message.ID}
                data-message-id={message.ID}
                className={
                  message.ID === targetMessageID &&
                  history.targetStatus === 'found'
                    ? 'chat-message-target'
                    : undefined
                }
                ref={message.ID === targetMessageID ? targetElement : undefined}
                tabIndex={message.ID === targetMessageID ? -1 : undefined}
                aria-label={
                  message.ID === targetMessageID
                    ? t.chat.targetFound
                    : undefined
                }
              >
                <ChatMessage
                  avatar={
                    message.Sender === 'assistant' ? (
                      <AssistantAvatar
                        name={assistant.Name}
                        avatar={assistant.Avatar}
                      />
                    ) : (
                      <AssistantAvatar name={userName} avatar={user?.Avatar} />
                    )
                  }
                  role={message.Sender}
                  sender={message.Sender === 'user' ? userName : assistant.Name}
                  timestamp={message.CreatedAt}
                  timeZone={timeZone}
                  text={message.Content.Text}
                  readStatus={
                    message.Sender === 'user'
                      ? message.ReadAt
                        ? 'read'
                        : 'unread'
                      : undefined
                  }
                  cards={
                    message.Content.Card ||
                    message.Content.AttachmentIDs?.length ? (
                      <>
                        {message.Content.Card &&
                          (message.Content.Card.Kind ===
                          'permission_request' ? (
                            <PermissionRequestCard
                              requestID={message.Content.Card.TargetID}
                              assistant={assistant}
                              timeZone={timeZone}
                              title={message.Content.Card.Title}
                              summary={message.Content.Card.Summary}
                            />
                          ) : (
                            <ConversationCard
                              card={message.Content.Card}
                              timeZone={timeZone}
                              onOpen={onCard}
                            />
                          ))}
                        {message.Content.AttachmentIDs?.length ? (
                          <MessageAttachments
                            ids={message.Content.AttachmentIDs}
                          />
                        ) : null}
                      </>
                    ) : undefined
                  }
                  footer={
                    message.Sender === 'user' ? (
                      !archived &&
                      !dreaming &&
                      state !== 'stopped' &&
                      state !== 'paused' &&
                      message.ID === latestUnread?.ID &&
                      (working || covered) ? (
                        <div className="chat-expedite">
                          <button
                            type="button"
                            className="message-expedite-button"
                            aria-label={
                              covered || expediteBusy
                                ? t.chat.expediting
                                : expediteFailed
                                  ? t.chat.expediteFailed
                                  : t.chat.expedite
                            }
                            title={
                              covered || expediteBusy
                                ? t.chat.expediting
                                : expediteFailed
                                  ? t.chat.expediteFailed
                                  : t.chat.expediteHelp
                            }
                            aria-busy={expediteBusy || undefined}
                            disabled={covered || expediteBusy || !working}
                            onClick={() => void expedite()}
                          >
                            <Zap size={15} aria-hidden="true" />
                            <span>
                              {covered || expediteBusy
                                ? t.chat.expediting
                                : expediteFailed
                                  ? t.chat.expediteFailed
                                  : t.chat.expedite}
                            </span>
                          </button>
                          {covered &&
                            (expedited?.State === 'waiting_tool' ||
                              !working) && (
                              <small role="status">
                                {working
                                  ? t.chat.expediteTool
                                  : t.chat.expediteWaiting}
                              </small>
                            )}
                        </div>
                      ) : undefined
                    ) : (
                      <TurnInformation
                        message={message}
                        revision={history.metadataVersions[message.ID] ?? 0}
                        onDebug={onDebug}
                        readOnly={!!archived}
                      />
                    )
                  }
                />
              </div>
            ))
          )}
        </div>
        {showLatest && (
          <Button
            className="chat-return-latest"
            variant="secondary"
            onClick={returnToLatest}
          >
            <ArrowDown size={16} aria-hidden="true" />
            {(conversation?.UnreadCount ?? 0) > 0
              ? t.chat.newMessages
              : t.chat.returnLatest}
          </Button>
        )}
      </div>
      <ChatComposerContainer
        assistant={assistant}
        archived={!!archived}
        status={status.data}
        channel={channel}
        onStatusChange={status.reload}
        onManageChannel={() => onAction('channel')}
        onSent={() => {
          bottom.current = true;
          onClearTarget();
        }}
      />
    </section>
  );
}
export function Attachment({ id }: { id: string }) {
  const { t } = useCopy(),
    file = useQuery<FileInfo>(`/api/files/${id}`);
  useErrorToast(file.error, file.reload);
  return (
    <div className="attachment">
      {file.data && (
        <a href={`/api/files/${encodeURIComponent(id)}/content`} download>
          <Download size={16} />
          {file.data.Name}
          <small>{Math.ceil(file.data.Size / 1024)} KB</small>
        </a>
      )}
      {file.loading && !file.data && <span>{t.loading}</span>}
    </div>
  );
}

function MessageAttachments({ ids }: { ids: string[] }) {
  const { t } = useCopy();
  const [files, setFiles] = useState<FileInfo[]>([]),
    [error, setError] = useState<unknown>(null),
    [loading, setLoading] = useState(true),
    [attempt, setAttempt] = useState(0);
  const attachments = useFilePreviews(files);
  const key = JSON.stringify(ids);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    setFiles([]);
    void Promise.allSettled(
      (JSON.parse(key) as string[]).map((id) =>
        api.get<FileInfo>(
          `/api/files/${encodeURIComponent(id)}`,
          controller.signal,
        ),
      ),
    ).then((results) => {
      if (controller.signal.aborted) return;
      setFiles(
        results.flatMap((result) =>
          result.status === 'fulfilled' ? [result.value] : [],
        ),
      );
      setError(
        results.find((result) => result.status === 'rejected')?.reason ?? null,
      );
      setLoading(false);
    });
    return () => controller.abort();
  }, [key, attempt]);
  useErrorToast(error, () => setAttempt((value) => value + 1));
  return (
    <div>
      {loading && <span>{t.loading}</span>}
      <ChatAttachmentList files={attachments} />
    </div>
  );
}
