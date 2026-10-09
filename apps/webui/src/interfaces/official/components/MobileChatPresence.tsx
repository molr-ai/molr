import { useLayoutEffect, useRef, useState } from 'react';
import type { ReactElement } from 'react';
import { useUILocale } from '@molr/ui';

/** Retain the outgoing mobile conversation only until its opacity reaches zero. */
export function MobileChatPresence({ enabled, children }: { enabled: boolean; children: ReactElement | null }) {
  const { reducedMotion } = useUILocale();
  const [retained, setRetained] = useState(children);
  if (children && children !== retained) setRetained(children);
  const shown = children ?? retained, open = !!children, present = !!shown;
  const ref = useRef<HTMLDivElement>(null), previous = useRef<number | null>(null);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!enabled || !element || !present) {
      previous.current = null;
      if (!open) setRetained(null);
      return;
    }
    const media = window.matchMedia('(prefers-reduced-motion: reduce)');
    let active = true, settled = false;
    let animation: Animation | undefined;
    function finish() {
      if (!active || settled) return;
      settled = true;
      previous.current = open ? 1 : null;
      if (open) animation?.cancel();
      else setRetained(null);
    }
    if (reducedMotion || media.matches) { finish(); return; }
    animation = element.animate([{ opacity: previous.current ?? 0 }, { opacity: open ? 1 : 0 }], { duration: 180, easing: 'ease', fill: 'forwards', id: 'molr-mobile-chat-fade' });
    void animation.finished.then(finish, finish);
    const motionChanged = () => { if (media.matches) finish(); };
    media.addEventListener('change', motionChanged);
    return () => {
      active = false;
      if (!settled) previous.current = Number(getComputedStyle(element).opacity);
      animation?.cancel();
      media.removeEventListener('change', motionChanged);
    };
  }, [enabled, open, present, reducedMotion]);
  if (!enabled) return children;
  if (!shown) return null;
  return <div ref={ref} className="mobile-chat-presence" data-state={open ? 'open' : 'closing'} inert={!open} aria-hidden={!open || undefined}>{shown}</div>;
}
