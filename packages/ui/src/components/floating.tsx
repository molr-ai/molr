import { cloneElement, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { CSSProperties, HTMLAttributes, ReactElement, Ref } from 'react';
import { observeFloating, usePopover } from './floating-lifecycle';
import { getOverlayViewport, useModalScope } from '../modal-scope';

export type FloatingTrigger = ReactElement<HTMLAttributes<HTMLElement> & { disabled?: boolean; ref?: Ref<HTMLElement> }>;
export type FloatingSide = 'top' | 'bottom' | 'left' | 'right';
export type FloatingOptions = { align?: 'start' | 'end'; width?: number | 'auto'; preferredSide?: FloatingSide; maxHeight?: number };
export function composeRefs(...refs: (Ref<HTMLElement> | undefined)[]) {
  return (element: HTMLElement | null) => {
    const cleanups = refs.map(ref => {
      if (typeof ref === 'function') {
        const cleanup = ref(element);
        return typeof cleanup === 'function' ? cleanup : () => { ref(null); };
      }
      if (ref) { ref.current = element; return () => { ref.current = null; }; }
      return undefined;
    });
    return () => { for (const cleanup of cleanups) cleanup?.(); };
  };
}

export function useComposedRefs(first: Ref<HTMLElement>, second?: Ref<HTMLElement>) {
  return useMemo(() => composeRefs(first, second), [first, second]);
}

export function triggerElement(trigger: FloatingTrigger, ref: Ref<HTMLElement>, props: HTMLAttributes<HTMLElement>) {
  return cloneElement(trigger, { ...props, ref });
}

const baseStyle: CSSProperties = { position: 'fixed', inset: 'auto', margin: 0, boxSizing: 'border-box' };

export function useFloating(open: boolean, onOpenChange: (open: boolean) => void, { align = 'start', width = 280, preferredSide = 'bottom', maxHeight: heightLimit = 400 }: FloatingOptions = {}) {
  const boundary = useModalScope()?.container;
  const changeRef = useRef(onOpenChange);
  useLayoutEffect(() => { changeRef.current = onOpenChange; }, [onOpenChange]);
  const anchorRef = useRef<HTMLElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const [style, setStyle] = useState<CSSProperties>(baseStyle);
  const [side, setSide] = useState<FloatingSide>(preferredSide);
  usePopover(open, panelRef);
  useLayoutEffect(() => {
    const anchor = anchorRef.current;
    const panel = panelRef.current;
    if (!open || !anchor || !panel) return;
    function position() {
      if (!anchor || !panel) return;
      const rect = anchor.getBoundingClientRect();
      const { left: x, top: y, width: vw, height: vh } = getOverlayViewport(boundary);
      if (vw <= 16 || vh <= 16 || rect.bottom < y || rect.top > y + vh) { changeRef.current(false); return; }
      panel.style.width = width === 'auto' ? 'max-content' : `${Math.min(width, vw - 16)}px`;
      const actualWidth = Math.min(panel.getBoundingClientRect().width, vw - 16);
      const desiredHeight = Math.min(panel.scrollHeight + 2, heightLimit);
      const below = vh + y - rect.bottom - 14, above = rect.top - y - 14;
      let nextSide: FloatingSide, maxHeight: number, top: number, left: number;
      if (preferredSide === 'left' || preferredSide === 'right') {
        const before = rect.left - x - 14, after = x + vw - rect.right - 14;
        nextSide = preferredSide === 'right' ? after >= actualWidth || after >= before ? 'right' : 'left' : before >= actualWidth || before >= after ? 'left' : 'right';
        maxHeight = Math.max(0, Math.min(heightLimit, vh - 16));
        const height = Math.min(maxHeight, desiredHeight);
        top = Math.max(y + 8, Math.min(rect.top + (rect.height - height) / 2, y + vh - height - 8));
        left = Math.max(x + 8, Math.min(nextSide === 'right' ? rect.right + 6 : rect.left - actualWidth - 6, x + vw - actualWidth - 8));
      } else {
        nextSide = preferredSide === 'top' ? above >= desiredHeight || above > below ? 'top' : 'bottom' : below < desiredHeight && above > below ? 'top' : 'bottom';
        maxHeight = Math.max(0, Math.min(heightLimit, nextSide === 'top' ? above : below));
        top = Math.max(y + 8, nextSide === 'top' ? rect.top - Math.min(maxHeight, desiredHeight) - 6 : rect.bottom + 6);
        left = Math.max(x + 8, Math.min(align === 'end' ? rect.right - actualWidth : rect.left, x + vw - actualWidth - 8));
      }
      const next: CSSProperties = { ...baseStyle, width: actualWidth, left, top, maxHeight };
      setStyle(previous => Object.keys(next).every(key => previous[key as keyof CSSProperties] === next[key as keyof CSSProperties]) ? previous : next);
      setSide(nextSide);
    }
    function scroll(event: Event) { if (!(event.target instanceof Node && panel?.contains(event.target))) position(); }
    position();
    return observeFloating(anchor, panel, boundary, position, scroll);
  }, [open, align, width, preferredSide, boundary, heightLimit]);
  return { anchorRef, panelRef, style, side };
}
