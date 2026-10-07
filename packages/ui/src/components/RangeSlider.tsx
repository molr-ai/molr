import { useId, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';
import { useUILocale } from '../locale';

export type RangeSliderProps = {
  label: string;
  value: readonly [number, number];
  onValueChange: (value: [number, number]) => void;
  min?: number;
  max?: number;
  step?: number;
  disabled?: boolean;
  hideLabel?: boolean;
  lowerLabel?: string;
  upperLabel?: string;
  formatValue?: (value: number) => string;
  description?: string;
  error?: string;
  size?: 'sm' | 'md';
  className?: string;
};

/** A controlled, horizontal range with two independently focusable thumbs. */
export function RangeSlider({ label, value, onValueChange, min = 0, max = 100, step = 1, disabled = false, hideLabel = false, lowerLabel, upperLabel, formatValue = String, description, error, size = 'md', className = '' }: RangeSliderProps) {
  const { messages } = useUILocale(); const id = useId();
  const rail = useRef<HTMLDivElement>(null), thumbs = useRef<Array<HTMLButtonElement | null>>([]);
  const pointer = useRef<{ id: number; thumb: 0 | 1 } | null>(null);
  const [active, setActive] = useState<0 | 1>(0);
  const low = Number.isFinite(min) ? min : 0, high = Number.isFinite(max) ? Math.max(low, max) : low;
  const increment = Number.isFinite(step) && step > 0 ? step : 1;
  const clamp = (n: number) => Math.max(low, Math.min(high, n));
  const first = clamp(Number.isFinite(value[0]) ? value[0] : low), last = clamp(Number.isFinite(value[1]) ? value[1] : high);
  const bounds: [number, number] = [Math.min(first, last), Math.max(first, last)];
  const inactive = disabled || low === high;
  const labels = [lowerLabel ?? messages.rangeSlider.start, upperLabel ?? messages.rangeSlider.end];
  const percent = (n: number) => high === low ? 0 : (n - low) / (high - low) * 100;
  const hint = error || description;
  const update = (thumb: 0 | 1, n: number) => {
    if (inactive) return;
    const snapped = n <= low ? low : n >= high ? high : Number((low + Math.round((n - low) / increment) * increment).toFixed(10));
    const next: [number, number] = thumb === 0 ? [Math.min(clamp(snapped), bounds[1]), bounds[1]] : [bounds[0], Math.max(bounds[0], clamp(snapped))];
    if (next[0] !== bounds[0] || next[1] !== bounds[1]) onValueChange(next);
  };
  const at = (x: number) => {
    const box = rail.current!.getBoundingClientRect();
    return low + Math.max(0, Math.min(1, (x - box.left) / Math.max(1, box.width))) * (high - low);
  };
  const keyDown = (event: KeyboardEvent<HTMLButtonElement>, thumb: 0 | 1) => {
    const delta = event.key === 'ArrowRight' || event.key === 'ArrowUp' ? increment : event.key === 'ArrowLeft' || event.key === 'ArrowDown' ? -increment : event.key === 'PageUp' ? increment * 10 : event.key === 'PageDown' ? -increment * 10 : 0;
    if (!delta && event.key !== 'Home' && event.key !== 'End') return;
    event.preventDefault();
    update(thumb, event.key === 'Home' ? low : event.key === 'End' ? high : bounds[thumb] + delta);
  };
  return <div className={`ui-range-slider ui-range-slider--${size} ${className}`} role="group" aria-labelledby={`${id}-label`} aria-describedby={hint ? `${id}-hint` : undefined} aria-disabled={inactive || undefined} aria-invalid={error ? true : undefined}>
    <div className="ui-range-slider__header"><span id={`${id}-label`} className={hideLabel ? 'ui-sr-only' : undefined}>{label}</span><output className="ui-range-slider__value" aria-hidden="true">{formatValue(bounds[0])}<span>—</span>{formatValue(bounds[1])}</output></div>
    <div className="ui-range-slider__control" onPointerDown={event => {
      if (inactive || !event.isPrimary || event.button !== 0 || pointer.current) return;
      const target = event.target instanceof Element ? event.target.closest<HTMLElement>('[data-range-thumb]') : null;
      const position = at(event.clientX);
      const thumb: 0 | 1 = target ? Number(target.dataset.rangeThumb) as 0 | 1 : Math.abs(position - bounds[0]) === Math.abs(position - bounds[1]) ? (position < bounds[0] ? 0 : position > bounds[1] ? 1 : active) : Math.abs(position - bounds[0]) < Math.abs(position - bounds[1]) ? 0 : 1;
      event.preventDefault(); pointer.current = { id: event.pointerId, thumb }; setActive(thumb);
      thumbs.current[thumb]?.focus({ preventScroll: true }); event.currentTarget.setPointerCapture(event.pointerId);
      if (!target) update(thumb, position);
    }} onPointerMove={event => { if (pointer.current?.id === event.pointerId) update(pointer.current.thumb, at(event.clientX)); }} onPointerUp={event => {
      if (pointer.current?.id !== event.pointerId) return;
      pointer.current = null;
      if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
    }} onPointerCancel={() => { pointer.current = null; }} onLostPointerCapture={() => { pointer.current = null; }}>
      <div className="ui-range-slider__rail" ref={rail}><div className="ui-range-slider__selection" style={{ left: `${percent(bounds[0])}%`, width: `${percent(bounds[1]) - percent(bounds[0])}%` }} />
        {([0, 1] as const).map(thumb => <button key={thumb} ref={node => { thumbs.current[thumb] = node; }} type="button" role="slider" className="ui-range-slider__thumb" data-range-thumb={thumb} data-active={active === thumb || undefined}
          disabled={inactive} aria-label={labels[thumb]} aria-orientation="horizontal" aria-valuemin={thumb === 0 ? low : bounds[0]} aria-valuemax={thumb === 0 ? bounds[1] : high} aria-valuenow={bounds[thumb]} aria-valuetext={formatValue(bounds[thumb])} aria-describedby={hint ? `${id}-hint` : undefined} aria-invalid={error ? true : undefined}
          style={{ left: `${percent(bounds[thumb])}%` }} onFocus={() => setActive(thumb)} onKeyDown={event => keyDown(event, thumb)} />)}
      </div>
    </div>
    {hint && <p id={`${id}-hint`} className={`field-hint${error ? ' field-hint--error' : ''}`}>{hint}</p>}
  </div>;
}
