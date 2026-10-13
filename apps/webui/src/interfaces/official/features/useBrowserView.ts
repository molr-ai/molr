import { useLayoutEffect, useRef, useState } from 'react';
import type { KeyboardEvent, PointerEvent, RefObject } from 'react';

export type BrowserBounds = { x: number; y: number; width: number };
const ratio = 1280 / 800;
const margin = 16;

/** Resize the streamed page, leaving the browser chrome at its normal size. */
export function useBrowserView(floating: boolean, saved: RefObject<BrowserBounds | null>) {
  const panelRef = useRef<HTMLDivElement>(null);
  const [moving, setMoving] = useState(false);
  const gesture = useRef<{ id: number; x: number; y: number; bounds: BrowserBounds; resize: boolean } | null>(null);
  const dialog = () => panelRef.current?.closest<HTMLDialogElement>('dialog');
  function place(bounds: BrowserBounds) {
    const element = dialog();
    if (!element) return;
    const viewport = element.querySelector<HTMLElement>('.browser-viewport');
    const chrome = element.getBoundingClientRect().height - (viewport?.getBoundingClientRect().height ?? 0);
    const border = element.offsetWidth - element.clientWidth;
    const availableWidth = document.documentElement.clientWidth - margin * 2;
    const availableHeight = document.documentElement.clientHeight - margin * 2;
    const maximum = Math.max(1, Math.min(availableWidth, viewport ? (availableHeight - chrome) * ratio + border : availableWidth));
    const width = Math.min(maximum, Math.max(Math.min(480, maximum), bounds.width));
    const height = viewport ? (width - border) / ratio + chrome : Math.min(element.offsetHeight, availableHeight);
    const x = Math.max(margin, Math.min(bounds.x, availableWidth + margin - width));
    const y = Math.max(margin, Math.min(bounds.y, availableHeight + margin - height));
    saved.current = { x, y, width };
    Object.assign(element.style, { left: `${x}px`, top: `${y}px`, width: `${width}px` });
  }
  useLayoutEffect(() => {
    const element = dialog();
    if (!floating || !element) return;
    const fit = () => place(saved.current ?? { x: document.documentElement.clientWidth - 976, y: 80, width: 960 });
    fit();
    // Covers starting/stopping a browser and expanding/resizing auxiliary input.
    const observer = new ResizeObserver(fit);
    observer.observe(element);
    window.addEventListener('resize', fit);
    return () => {
      observer.disconnect(); window.removeEventListener('resize', fit);
      gesture.current = null; setMoving(false);
      for (const property of ['left', 'top', 'width']) element.style.removeProperty(property);
    };
  }, [floating, saved]);
  function start(event: PointerEvent<HTMLElement>, resize = false) {
    if (!floating || event.button !== 0 || !saved.current) return;
    if (!resize && (event.target as Element).closest('button, input, [role="tab"]') && !(event.target as Element).closest('.browser-move')) return;
    event.preventDefault();
    const handle = resize ? event.currentTarget : event.currentTarget.querySelector<HTMLElement>('.browser-move');
    handle?.focus({ preventScroll: true });
    event.currentTarget.setPointerCapture(event.pointerId);
    gesture.current = { id: event.pointerId, x: event.clientX, y: event.clientY, bounds: { ...saved.current }, resize };
    setMoving(true);
  }
  function move(event: PointerEvent<HTMLElement>) {
    const active = gesture.current;
    if (!active || active.id !== event.pointerId) return;
    const dx = event.clientX - active.x, dy = event.clientY - active.y;
    if (active.resize) {
      const width = active.bounds.width + (dx + dy / ratio) / (1 + 1 / ratio ** 2);
      const element = dialog()!, viewport = element.querySelector<HTMLElement>('.browser-viewport');
      const chrome = element.getBoundingClientRect().height - (viewport?.getBoundingClientRect().height ?? 0);
      const border = element.offsetWidth - element.clientWidth;
      const maximum = Math.min(document.documentElement.clientWidth - margin - active.bounds.x, viewport ? (document.documentElement.clientHeight - margin - active.bounds.y - chrome) * ratio + border : Infinity);
      place({ ...active.bounds, width: Math.min(width, maximum) });
    } else place({ ...active.bounds, x: active.bounds.x + dx, y: active.bounds.y + dy });
  }
  function end(event: PointerEvent<HTMLElement>) {
    if (gesture.current?.id !== event.pointerId) return;
    gesture.current = null; setMoving(false);
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
  }
  function key(event: KeyboardEvent<HTMLElement>, resize = false) {
    if (!floating || !saved.current || !['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key)) return;
    event.preventDefault(); event.stopPropagation();
    const step = event.shiftKey ? 40 : 10, current = saved.current;
    if (resize) place({ ...current, width: current.width + (['ArrowLeft', 'ArrowUp'].includes(event.key) ? -step : step) });
    else place({ ...current, x: current.x + (event.key === 'ArrowLeft' ? -step : event.key === 'ArrowRight' ? step : 0), y: current.y + (event.key === 'ArrowUp' ? -step : event.key === 'ArrowDown' ? step : 0) });
  }
  const pointerProps = { onPointerMove: move, onPointerUp: end, onPointerCancel: end, onLostPointerCapture: end };
  return { panelRef, moving, dragProps: { ...pointerProps, onPointerDown: (event: PointerEvent<HTMLElement>) => start(event) }, moveKey: (event: KeyboardEvent<HTMLElement>) => key(event), resizeProps: { ...pointerProps, onPointerDown: (event: PointerEvent<HTMLElement>) => start(event, true), onKeyDown: (event: KeyboardEvent<HTMLElement>) => key(event, true) } };
}
