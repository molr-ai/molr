import { useLayoutEffect, useRef } from 'react';
import type { KeyboardEvent, PointerEvent, RefObject } from 'react';
export type FloatingBounds = { x: number; y: number; width: number; height: number };

/** Freeform panel geometry. Bounds remain in the owning page's memory. */
export function useFloatingPanel(floating: boolean, saved: RefObject<FloatingBounds | null>) {
  const ref = useRef<HTMLDivElement>(null);
  const gesture = useRef<{ x: number; y: number; bounds: FloatingBounds; resize: boolean } | null>(null);
  function place(next: FloatingBounds) {
    const dialog = ref.current?.closest('dialog'); if (!dialog) return;
    const vw = document.documentElement.clientWidth, vh = document.documentElement.clientHeight;
    const width = Math.min(vw - 32, Math.max(360, next.width)), height = Math.min(vh - 32, Math.max(300, next.height));
    const bounds = { width, height, x: Math.max(16, Math.min(vw - width - 16, next.x)), y: Math.max(16, Math.min(vh - height - 16, next.y)) };
    saved.current = bounds;
    Object.assign(dialog.style, { left: `${bounds.x}px`, top: `${bounds.y}px`, width: `${width}px`, height: `${height}px` });
  }
  useLayoutEffect(() => {
    if (!floating) return;
    const dialog = ref.current?.closest('dialog');
    const fit = () => place(saved.current ?? { x: document.documentElement.clientWidth - 936, y: 80, width: 920, height: 620 });
    fit(); window.addEventListener('resize', fit);
    return () => { window.removeEventListener('resize', fit); gesture.current = null; for (const key of ['left', 'top', 'width', 'height']) dialog?.style.removeProperty(key); };
  }, [floating, saved]);
  function start(event: PointerEvent<HTMLElement>, resize: boolean) {
    if (!floating || event.button !== 0 || !saved.current) return;
    if (!resize && (event.target as Element).closest('button') && !(event.target as Element).closest('.files-move')) return;
    event.preventDefault(); event.currentTarget.setPointerCapture(event.pointerId);
    gesture.current = { x: event.clientX, y: event.clientY, bounds: { ...saved.current }, resize };
  }
  function move(event: PointerEvent<HTMLElement>) {
    const g = gesture.current; if (!g) return;
    const dx = event.clientX - g.x, dy = event.clientY - g.y;
    place(g.resize ? { ...g.bounds, width: Math.min(g.bounds.width + dx, document.documentElement.clientWidth - g.bounds.x - 16), height: Math.min(g.bounds.height + dy, document.documentElement.clientHeight - g.bounds.y - 16) } : { ...g.bounds, x: g.bounds.x + dx, y: g.bounds.y + dy });
  }
  function end() { gesture.current = null; }
  function key(event: KeyboardEvent<HTMLElement>, resize = false) {
    if (!floating || !saved.current || !['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key)) return;
    event.preventDefault(); event.stopPropagation(); const step = event.shiftKey ? 40 : 10;
    const dx = event.key === 'ArrowLeft' ? -step : event.key === 'ArrowRight' ? step : 0, dy = event.key === 'ArrowUp' ? -step : event.key === 'ArrowDown' ? step : 0;
    place(resize ? { ...saved.current, width: saved.current.width + dx, height: saved.current.height + dy } : { ...saved.current, x: saved.current.x + dx, y: saved.current.y + dy });
  }
  const pointer = { onPointerMove: move, onPointerUp: end, onPointerCancel: end, onLostPointerCapture: end };
  return { ref, drag: { ...pointer, onPointerDown: (e: PointerEvent<HTMLElement>) => start(e, false) }, moveKey: (e: KeyboardEvent<HTMLElement>) => key(e), resize: { ...pointer, onPointerDown: (e: PointerEvent<HTMLElement>) => start(e, true), onKeyDown: (e: KeyboardEvent<HTMLElement>) => key(e, true) } };
}
