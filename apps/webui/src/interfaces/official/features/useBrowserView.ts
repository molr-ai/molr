import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { useUILocale } from '@molr/ui';

// Keep the modal layer until contraction finishes, so the sidebar cannot clip
// the moving panel. Drawer still owns focus containment and initial opening.
export function useBrowserView(docked: boolean) {
  const panelRef = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState(false), [moving, setMoving] = useState(false);
  const { reducedMotion } = useUILocale();
  const normal = useRef<DOMRect | null>(null), from = useRef<DOMRect | null>(null);
  const animation = useRef<Animation | null>(null), settling = useRef(false), restoreSize = useRef<(() => void) | null>(null);
  const dialog = () => panelRef.current?.closest<HTMLDialogElement>('dialog');
  const cancelDrawerMotion = (element: HTMLElement) => {
    element.getAnimations().filter(a => a.id.startsWith('molr-drawer-')).forEach(a => a.cancel());
  };
  const animate = (element: HTMLElement, start: DOMRect, end: DOMRect, base: DOMRect, finish: () => void) => {
    if (reducedMotion || matchMedia('(prefers-reduced-motion: reduce)').matches) { finish(); return; }
    // Animate bounds, keeping toolbar text at its normal size throughout.
    const saved = { minWidth: element.style.minWidth, maxWidth: element.style.maxWidth, minHeight: element.style.minHeight, maxHeight: element.style.maxHeight };
    Object.assign(element.style, { minWidth: '0', maxWidth: 'none', minHeight: '0', maxHeight: 'none' });
    restoreSize.current = () => Object.assign(element.style, saved);
    const frame = (rect: DOMRect) => ({ left: `${rect.x - base.x}px`, top: `${rect.y - base.y}px`, width: `${rect.width}px`, height: `${rect.height}px` });
    setMoving(true);
    const motion = element.animate([frame(start), frame(end)], { id: 'molr-browser-view', duration: 260, easing: 'cubic-bezier(0.22, 1, 0.36, 1)', fill: 'both' });
    animation.current = motion;
    void motion.finished.then(() => {
      if (animation.current !== motion) return;
      flushSync(() => { motion.cancel(); animation.current = null; restoreSize.current?.(); restoreSize.current = null; finish(); setMoving(false); });
    }, () => {});
  };
  function changeView(next: boolean) {
    const element = dialog();
    if (!element || moving || next === expanded) return;
    const current = element.getBoundingClientRect();
    if (next) { normal.current = current; from.current = current; setExpanded(true); return; }
    const side = docked ? element.closest('.chat-side-panel')?.getBoundingClientRect() : null;
    const old = normal.current ?? current;
    // The side panel remains in layout while the browser occupies the modal layer.
    const target = side ?? new DOMRect(current.right - Math.min(old.width, current.width), current.bottom - Math.min(old.height, current.height), Math.min(old.width, current.width), Math.min(old.height, current.height));
    animate(element, current, target, current, () => { settling.current = true; setExpanded(false); });
  }
  useLayoutEffect(() => {
    const element = dialog();
    if (!element) return;
    if (settling.current) { cancelDrawerMotion(element); settling.current = false; return; }
    if (!expanded || !from.current) return;
    cancelDrawerMotion(element);
    const start = from.current; from.current = null;
    const end = element.getBoundingClientRect();
    animate(element, start, end, end, () => {});
  }, [expanded]);
  useEffect(() => { if (reducedMotion) animation.current?.finish(); }, [reducedMotion]);
  useEffect(() => () => { animation.current?.cancel(); animation.current = null; restoreSize.current?.(); restoreSize.current = null; }, []);
  return { panelRef, expanded, moving, changeView };
}
