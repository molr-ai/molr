import type { Conversation } from '@molr/sdk';

export const conversationUnread = (items: readonly Conversation[] | undefined) =>
  (items ?? []).reduce((total, item) => total + (item.Archived ? 0 : Math.max(0, item.UnreadCount)), 0);
