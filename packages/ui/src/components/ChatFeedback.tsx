import { useEffect, useId, useState } from 'react';
import { useUILocale } from '../locale';

type Feedback = 'up' | 'down';
export type ChatFeedbackProps = { value?: Feedback; pending?: Feedback; onChange: (value: Feedback) => void };

/** Preserve the original icon outline; independent fill regions add the two-tone selection. */
function FeedbackGlyph({ direction }: { direction: Feedback }) {
  const cuff = 'M7 10v11H3V10Z';
  const palm = 'M7 10l5-8c2 0 3 1 2 4l-1 4h6a2 2 0 0 1 2 2l-2 7a2 2 0 0 1-2 2H7';
  return <svg className="message-feedback__glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <g transform={direction === 'down' ? 'translate(0 24) scale(1 -1)' : undefined}>
      <path className="message-feedback__palm" d={palm} stroke="none" />
      <path className="message-feedback__cuff" d={cuff} stroke="none" />
      <path d={`${cuff}${palm}`} />
    </g>
  </svg>;
}

export function ChatFeedback({ value, pending, onChange }: ChatFeedbackProps) {
  const { messages } = useUILocale();
  const statusID = useId();
  const pendingLabel = pending ? value === pending ? pending === 'up' ? messages.chat.savingLike : messages.chat.savingDislike : messages.chat.clearingFeedback : undefined;
  const [activation, setActivation] = useState<{ direction: Feedback; sequence: number }>();
  useEffect(() => {
    if (!activation) return;
    // Reduced motion does not emit animationend; never replay a stale pulse.
    const timer = setTimeout(() => setActivation(undefined), 600);
    return () => clearTimeout(timer);
  }, [activation]);
  return <div className="message-feedback" role="group" aria-label={messages.chat.messageFeedback} aria-busy={!!pending}>
    {(['up', 'down'] as const).map(direction => {
      const selected = value === direction;
      const label = direction === 'up' ? messages.chat.like : messages.chat.dislike;
      const animate = selected && activation?.direction === direction;
      return <button key={direction} type="button" className="message-feedback__button" data-feedback={direction} aria-label={label} title={pending === direction ? pendingLabel : label} aria-pressed={selected} aria-disabled={!!pending} aria-describedby={pending === direction ? statusID : undefined} data-pending={pending === direction || undefined}
        onClick={() => {
          if (pending) return;
          setActivation(selected ? undefined : { direction, sequence: (activation?.sequence ?? 0) + 1 });
          onChange(direction);
        }}>
        <span key={animate ? `motion-${activation.sequence}` : 'idle'} className="message-feedback__motion" data-animate={animate || undefined} onAnimationEnd={event => {
          if (event.target === event.currentTarget) setActivation(undefined);
        }}><FeedbackGlyph direction={direction} /></span>
        {pending === direction && <span className="message-feedback__pending" aria-hidden="true"><i /><i /><i /></span>}
        {animate && <svg key={`accent-${activation.sequence}`} className="message-feedback__accent" viewBox="0 0 36 36" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
          {direction === 'up' ? <path d="M9 6 7 3M18 4V1M27 6l2-3" /> : <path d="M10 29q8 5 16 0" />}
        </svg>}
      </button>;
    })}
    {pending && <span className="ui-sr-only" id={statusID} role="status">{pendingLabel}</span>}
  </div>;
}

