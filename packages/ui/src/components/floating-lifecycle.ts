import { useLayoutEffect } from 'react';
import type { RefObject } from 'react';

/** Keep top-layer visibility independent of positioning effect restarts. */
export function usePopover(open: boolean, panelRef: RefObject<HTMLElement | null>) {
  useLayoutEffect(() => {
    const panel = panelRef.current;
    if (!panel) return;
    if (open && !panel.matches(':popover-open')) panel.showPopover();
    else if (!open && panel.matches(':popover-open')) panel.hidePopover();
    return () => {
      if (panel.matches(':popover-open')) panel.hidePopover();
    };
  }, [open, panelRef]);
}

/** Observe geometry changes; callers retain placement and scroll policy. */
export function observeFloating(anchor: HTMLElement, panel: HTMLElement, boundary: HTMLElement | null | undefined, position: () => void, onScroll: (event: Event) => void = position) {
  const observer = new ResizeObserver(position);
  observer.observe(anchor);
  observer.observe(panel);
  if (boundary) observer.observe(boundary);
  const viewport = window.visualViewport;
  window.addEventListener('resize', position);
  window.addEventListener('scroll', onScroll, true);
  viewport?.addEventListener('resize', position);
  viewport?.addEventListener('scroll', position);
  return () => {
    observer.disconnect();
    window.removeEventListener('resize', position);
    window.removeEventListener('scroll', onScroll, true);
    viewport?.removeEventListener('resize', position);
    viewport?.removeEventListener('scroll', position);
  };
}
