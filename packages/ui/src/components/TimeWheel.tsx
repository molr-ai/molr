import { useId, useLayoutEffect, useRef } from 'react';

const rowHeight = 36;

export function TimeWheel({ label, count, value, onValueChange }: { label: string; count: number; value: number; onValueChange: (value: number) => void }) {
  const id = useId();
  const list = useRef<HTMLDivElement>(null);
  const fromScroll = useRef<number | null>(null);
  const latest = useRef({ value, onValueChange });
  useLayoutEffect(() => { latest.current = { value, onValueChange }; }, [value, onValueChange]);
  useLayoutEffect(() => {
    if (fromScroll.current === value) { fromScroll.current = null; return; }
    if (list.current) list.current.scrollTop = value * rowHeight;
    // A parent popover can become visible after this child's layout effect.
    const frame = requestAnimationFrame(() => { if (list.current) list.current.scrollTop = value * rowHeight; });
    return () => cancelAnimationFrame(frame);
  }, [value]);
  function select(next: number) {
    const selected = Math.max(0, Math.min(count - 1, next));
    fromScroll.current = null;
    if (list.current) list.current.scrollTop = selected * rowHeight;
    onValueChange(selected);
  }
  return <div className="ui-time-wheel">
    <span id={`${id}-label`} className="ui-time-wheel__label">{label}</span>
    <div ref={list} className="ui-time-wheel__list" role="listbox" tabIndex={0} aria-labelledby={`${id}-label`} aria-orientation="vertical" aria-activedescendant={`${id}-${value}`} onScroll={() => {
      if (!list.current) return;
      const next = Math.max(0, Math.min(count - 1, Math.round(list.current.scrollTop / rowHeight)));
      if (next !== latest.current.value) {
        fromScroll.current = next;
        latest.current.value = next;
        latest.current.onValueChange(next);
      }
    }} onKeyDown={event => {
      let next = value;
      switch (event.key) {
        case 'ArrowUp': next--; break;
        case 'ArrowDown': next++; break;
        case 'PageUp': next -= 5; break;
        case 'PageDown': next += 5; break;
        case 'Home': next = 0; break;
        case 'End': next = count - 1; break;
        default: return;
      }
      event.preventDefault(); select(next);
    }}>
      {Array.from({ length: count }, (_, n) => <button key={n} id={`${id}-${n}`} type="button" role="option" tabIndex={-1} aria-selected={n === value} onClick={() => { list.current?.focus({ preventScroll: true }); select(n); }}>{String(n).padStart(2, '0')}</button>)}
    </div>
  </div>;
}
