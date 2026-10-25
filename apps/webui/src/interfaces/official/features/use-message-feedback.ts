import { useEffect, useRef, useState } from 'react';
import { useToast } from '@molr/ui';
import { api } from '@molr/sdk';
import type { Message } from '@molr/sdk';
import { useCopy, useData } from '../../../state';

type Feedback = 'up' | 'down' | undefined;
type LocalFeedback = { value: Feedback; revision: number; pending?: 'up' | 'down' };

/** Keep optimistic feedback local to its message, independent of chat sending. */
export function useMessageFeedback(message: Message, revision: number) {
  const { invalidateMessage } = useData(), { t } = useCopy(), { toast } = useToast();
  const remote: Feedback = message.Feedback === 'like' ? 'up' : message.Feedback === 'dislike' ? 'down' : undefined;
  const [local, setLocal] = useState<LocalFeedback | null>(null);
  const writing = useRef(false), mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  // Keep the optimistic choice until a read started after this write returns.
  // Older in-flight metadata must not flash the previous selection back on.
  useEffect(() => {
    if (local && !local.pending && revision >= local.revision) setLocal(null);
  }, [local, revision]);
  const value = local && (local.pending || revision < local.revision) ? local.value : remote;
  async function change(direction: 'up' | 'down') {
    if (writing.current) return;
    writing.current = true;
    const next = value === direction ? undefined : direction;
    const previous = local;
    setLocal({ value: next, revision, pending: direction });
    try {
      await api.mutate('/api/conversations/feedback', { MessageID: message.ID, Value: next === 'up' ? 'like' : next === 'down' ? 'dislike' : 'none' });
      const confirmedRevision = invalidateMessage(message.ID, message.ConversationID);
      if (mounted.current) setLocal({ value: next, revision: confirmedRevision });
    } catch {
      if (mounted.current) {
        setLocal(previous);
        toast({ title: t.chat.feedbackFailed, tone: 'danger' });
      }
      // A lost response may still have committed; reconcile with Runtime.
      invalidateMessage(message.ID, message.ConversationID);
    } finally { writing.current = false; }
  }
  return { feedback: value, feedbackPending: local?.pending, onFeedback: change };
}
