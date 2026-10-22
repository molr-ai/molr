import { useRef, useState } from 'react';
import type { CSSProperties } from 'react';

export type PaneSeparatorProps = {
  label: string; value: number; min: number; max: number; reverse?: boolean; style?: CSSProperties; className?: string;
  onResize: (value: number) => void; onCommit?: () => void;
};

/** Controlled horizontal pane resizing; callers own layout, constraints and persistence. */
export function PaneSeparator({ label, value, min, max, reverse = false, style, className = '', onResize, onCommit }: PaneSeparatorProps) {
  const drag = useRef<{ pointer: number; x: number; value: number } | null>(null);
  const [dragging, setDragging] = useState(false);
  const resize = (next: number) => onResize(Math.max(min, Math.min(max, next)));
  function finish() {
    if (!drag.current) return;
    drag.current = null; setDragging(false); onCommit?.();
  }
  return <div className={`ui-pane-separator ${className}`} role="separator" tabIndex={0} aria-label={label} aria-orientation="vertical"
    aria-valuemin={min} aria-valuemax={Math.round(max)} aria-valuenow={Math.round(value)} aria-valuetext={`${Math.round(value)} px`}
    style={style} data-dragging={dragging || undefined}
    onPointerDown={event => {
      if (event.button !== 0 || !event.isPrimary) return;
      event.preventDefault(); event.currentTarget.focus(); event.currentTarget.setPointerCapture(event.pointerId);
      drag.current = { pointer: event.pointerId, x: event.clientX, value }; setDragging(true);
    }}
    onPointerMove={event => {
      const start = drag.current;
      if (start?.pointer === event.pointerId) resize(start.value + (event.clientX - start.x) * (reverse ? -1 : 1));
    }}
    onPointerUp={event => { if (drag.current?.pointer === event.pointerId) { finish(); event.currentTarget.releasePointerCapture(event.pointerId); } }}
    onPointerCancel={finish} onLostPointerCapture={finish}
    onKeyDown={event => {
      const step = (event.shiftKey ? 40 : 10) * (reverse ? -1 : 1);
      const next = event.key === 'ArrowLeft' ? value - step : event.key === 'ArrowRight' ? value + step : event.key === 'Home' ? min : event.key === 'End' ? max : null;
      if (next === null) return;
      event.preventDefault(); resize(next); onCommit?.();
    }} />;
}
