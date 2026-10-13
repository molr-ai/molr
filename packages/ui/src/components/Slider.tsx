import { useId } from 'react';
import type { CSSProperties, ReactNode } from 'react';

export type SliderProps = {
  label: string;
  labelExtra?: ReactNode;
  value: number;
  onValueChange: (value: number) => void;
  min: number;
  max: number;
  step?: number;
  disabled?: boolean;
  formatValue?: (value: number) => string;
};

/** Single-value slider; both endpoints remain reachable with a partial last step. */
export function Slider({ label, labelExtra, value, onValueChange, min, max, step = 1, disabled, formatValue = String }: SliderProps) {
  const id = useId(), upper = Math.max(min, max), current = Math.max(min, Math.min(upper, value));
  const increment = step > 0 ? step : 1;
  const update = (next: number) => onValueChange(Math.max(min, Math.min(upper, next)));
  return <div className="ui-slider">
    <div className="ui-slider__header"><div className="ui-slider__label"><label htmlFor={id}>{label}</label>{labelExtra}</div><output htmlFor={id}>{formatValue(current)}</output></div>
    <input id={id} type="range" min={min} max={upper} step="any" value={current} disabled={disabled || min === upper}
      aria-valuetext={formatValue(current)} style={{ '--slider-progress': `${upper === min ? 100 : (current - min) / (upper - min) * 100}%` } as CSSProperties}
      onChange={event => { const next = Number(event.target.value); update(next >= upper ? upper : min + Math.round((next - min) / increment) * increment); }}
      onKeyDown={event => {
        const delta = event.key === 'ArrowRight' || event.key === 'ArrowUp' ? increment : event.key === 'ArrowLeft' || event.key === 'ArrowDown' ? -increment : event.key === 'PageUp' ? increment * 10 : event.key === 'PageDown' ? -increment * 10 : 0;
        if (!delta && event.key !== 'Home' && event.key !== 'End') return;
        event.preventDefault(); update(event.key === 'Home' ? min : event.key === 'End' ? upper : current + delta);
      }} />
    <div className="ui-slider__bounds" aria-hidden="true"><span>{formatValue(min)}</span><span>{formatValue(upper)}</span></div>
  </div>;
}
