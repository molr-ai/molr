import { useLayoutEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useUILocale } from '@molr/ui';
import type { Assistant } from '../../../api/types';

export type ChatPanelSelection = { kind: 'info' | 'permissions' | 'tasks' | 'shell_tasks' | 'browser'; assistant: Assistant };
export function isChatPanel(value: { kind: string; assistant: Assistant } | null): value is ChatPanelSelection {
  return !!value && ['info', 'permissions', 'tasks', 'shell_tasks', 'browser'].includes(value.kind);
}

/** Keep the panel mounted until its layout space has finished collapsing. */
export function ChatSidePanel({ panel, docked: requestedDocked, browserFloating = false, children }: { panel: ChatPanelSelection | null; docked: boolean; browserFloating?: boolean; children: (panel: ChatPanelSelection) => ReactNode }) {
  const { reducedMotion } = useUILocale();
  const [retained, setRetained] = useState(panel);
  if (panel && panel !== retained) setRetained(panel);
  const shown = panel ?? retained, open = !!panel;
  const docked = requestedDocked && !(shown?.kind === 'browser' && browserFloating);
  const ref = useRef<HTMLDivElement>(null);
  const previous = useRef<{ width: number; transform: string } | null>(null);
  useLayoutEffect(() => {
    const element = ref.current;
    if (!element || !shown) { previous.current = null; return; }
    if (!docked) { previous.current = null; if (!open) setRetained(null); return; }
    const media = window.matchMedia('(prefers-reduced-motion: reduce)');
    const content = element.querySelector<HTMLElement>('.ui-drawer[data-docked]');
    const width = parseFloat(getComputedStyle(element).getPropertyValue('--chat-panel-width'));
    const target = open ? width : 0;
    const from = previous.current ?? { width: 0, transform: 'translateX(100%)' };
    let active = true, settled = false;
    const animations: Animation[] = [];
    function finish() {
      if (!active || settled) return;
      settled = true;
      previous.current = open ? { width: target, transform: 'translateX(0)' } : null;
      if (open) animations.forEach(animation => animation.cancel());
      else setRetained(null); // Leave the zero-width final frame until unmount.
    }
    if (reducedMotion || media.matches) { finish(); return; }
    const timing: KeyframeAnimationOptions = { duration: open ? 280 : 220, easing: open ? 'cubic-bezier(0.22, 1, 0.36, 1)' : 'cubic-bezier(0.4, 0, 1, 1)', fill: 'forwards' };
    animations.push(element.animate([{ flexBasis: `${from.width}px` }, { flexBasis: `${target}px` }], { ...timing, id: 'molr-chat-panel-size' }));
    if (content) animations.push(content.animate([{ transform: from.transform }, { transform: open ? 'translateX(0)' : 'translateX(100%)' }], { ...timing, id: 'molr-chat-panel-slide' }));
    void Promise.all(animations.map(animation => animation.finished)).then(finish, finish);
    const motionChanged = () => { if (media.matches) finish(); };
    media.addEventListener('change', motionChanged);
    return () => {
      active = false;
      if (!settled) previous.current = { width: element.getBoundingClientRect().width, transform: content ? getComputedStyle(content).transform : 'none' };
      animations.forEach(animation => animation.cancel());
      media.removeEventListener('change', motionChanged);
    };
  }, [docked, open, shown?.kind, shown?.assistant.ID, reducedMotion]);
  if (!shown) return null;
  return <div ref={ref} className="chat-side-panel" data-kind={shown.kind} data-docked={docked || undefined} data-state={open ? 'open' : 'closing'} inert={!open}>
    {children(shown)}
  </div>;
}
