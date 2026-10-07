import { productMessages } from './i18n';
import type { ReactNode } from 'react';
import { ChatMessage as Message, ChatMessageMeta } from '@molr/ui';
import type { ChatMessage, InterfacePreferences } from './interface-demo';
import type { Locale } from './messages';

export function ChatMessageView({ message, sender, locale, preferences, cards, onFeedback }: { message: ChatMessage; sender: string; locale: Locale; preferences: InterfacePreferences; cards?: ReactNode; onFeedback: (feedback: 'up' | 'down') => void }) {
  const t = productMessages[locale];
  return <Message role={message.role} readStatus={message.readStatus} text={locale === 'en' && message.textEn ? message.textEn : message.text} sender={sender} showSender={preferences.showSender} showAvatar={preferences.showAvatar} attachments={message.attachments} images={message.images} cards={cards} className={`assistant-chat__message assistant-chat__message--${message.role}`} footer={message.role === 'assistant' && <ChatMessageMeta metrics={{ model: message.model ? `${message.model} · ${t.chatMessageView.demo}` : undefined }} metricsNote={t.chatMessageView.noLiveModelConnectedMeasurementsAreUnavailable} showMetrics={preferences.showMetrics} showFeedback={preferences.showFeedback} feedback={message.feedback} onFeedback={onFeedback} />} />;
}
