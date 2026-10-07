import type { Conversation, Message, Page } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';

export function AssistantMessagePreview({ conversation }: { conversation?: Conversation }) {
  const { t } = useCopy();
  // A single newest message is sufficient, including for conversations that
  // have never been opened. useQuery refreshes on message events and reconnect.
  const latest = useQuery<Page<Message>>(conversation && conversation.LastSequence > 0
    ? `/api/conversations/${conversation.ID}/messages?order=desc&limit=1` : null);
  if (!conversation) return null;
  if (conversation.LastSequence === 0) return t.assistants.start;
  if (!latest.data) return null;
  const message = latest.data.Items?.[0];
  if (!message) return t.assistants.start;
  const content = message.Content;
  const text = (content.Kind === 'card'
    ? content.Card?.Title || content.Card?.Summary || t.assistants.cardPreview
    : content.Text.trim() || t.assistants.attachmentPreview).replace(/\s+/gu, ' ').trim();
  const characters = Array.from(text);
  return characters.length > 80 ? `${characters.slice(0, 80).join('')}…` : text;
}
