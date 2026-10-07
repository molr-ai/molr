import { useState } from 'react';
import { ChatMessage, ChatMessageMeta, ChatFeedback, useUILocale } from '@molr/ui';
import { ComponentExample } from './ComponentExample';
import './design-feedback.css';
import { productMessages } from './i18n';

function FeedbackState({ initial, label }: { initial?: 'up' | 'down'; label: string }) {
  const [value, setValue] = useState(initial);
  return <div className="feedback-study__state"><ChatFeedback value={value} onChange={next => setValue(current => current === next ? undefined : next)} /><span>{label}</span></div>;
}

export function FeedbackDesignExample() {
  const { locale } = useUILocale();
  const t = productMessages[locale].chatShowcase.feedbackDesign;
  const [feedback, setFeedback] = useState<'up' | 'down'>();
  return <ComponentExample id="feedback-design" title={t.title} description={t.description} wide>
    <div className="feedback-study">
      <div className="feedback-study__states">
        <FeedbackState label={t.idle} />
        <FeedbackState initial="up" label={t.like} />
        <FeedbackState initial="down" label={t.dislike} />
      </div>
      <div className="feedback-study__conversation">
        <ChatMessage sender="Molr" text={t.sample} footer={<ChatMessageMeta feedback={feedback} onFeedback={next => setFeedback(current => current === next ? undefined : next)} />} />
      </div>
      <p className="feedback-study__hint">{t.hint}</p>
    </div>
  </ComponentExample>;
}
