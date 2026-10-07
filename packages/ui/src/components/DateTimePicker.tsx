import { useId, useState } from 'react';
import { useUILocale } from '../locale';
import { Button } from './ui';
import { UIIcon } from './icons';
import { Popover } from './Popover';
import { DateTimeCalendar } from './DateTimeCalendar';
import { useChoiceScroll } from '../use-choice-scroll';

export type DateTimePreset = '1d' | '7d' | '1m' | 'forever';
export type DateTimePickerProps = {
  label: string;
  mode?: 'datetime' | 'date' | 'time' | 'month';
  timeZone?: string;
  /** ISO instant, or null for no expiry. The editor displays timeZone, or browser local time when omitted. */
  value: string | null;
  onValueChange: (value: string | null) => void;
  presets?: readonly DateTimePreset[];
  min?: string;
  disabled?: boolean;
  size?: 'sm' | 'md' | 'lg';
  description?: string;
  error?: string;
  className?: string;
};

const defaultPresets: readonly DateTimePreset[] = ['1d', '7d', '1m', 'forever'];

export function DateTimePicker({ label, value, onValueChange, mode = 'datetime', timeZone, presets = mode === 'datetime' ? defaultPresets : [], min, disabled, size = 'md', description, error, className = '' }: DateTimePickerProps) {
  const { messages, locale } = useUILocale();
  const t = messages.dateTime;
  const id = useId();
  const [choice, setChoice] = useState<{ value: string | null; preset: DateTimePreset | 'custom' } | null>(null);
  const selected = choice?.value === value ? choice.preset : value === null && presets.includes('forever') ? 'forever' : 'custom';
  const [open, setOpen] = useState(false);
  const scrollRef = useChoiceScroll(`${selected}:${value}`);
  const hint = error || description;
  const format: Intl.DateTimeFormatOptions = { timeZone, ...(mode === 'month' ? { year: 'numeric', month: 'long' } : mode === 'time' ? { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' } : mode === 'date' ? { year: 'numeric', month: 'numeric', day: 'numeric' } : { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }) };
  const dateLabel = selected === 'custom' && value ? new Intl.DateTimeFormat(locale, format).format(new Date(value)) : t.chooseDate;
  const dateTitle = value ? new Intl.DateTimeFormat(locale, {...format, year: mode === 'time' ? undefined : 'numeric' }).format(new Date(value)) : t.chooseDate;
  function choose(preset: DateTimePreset) {
    const date = new Date();
    if (preset === '1m') {
      // Clamp Jan 31 → Feb 28/29, instead of overflowing into March.
      const day = date.getDate();
      date.setDate(1);
      date.setMonth(date.getMonth() + 1);
      const lastDay = new Date(date.getFullYear(), date.getMonth() + 1, 0).getDate();
      date.setDate(Math.min(day, lastDay));
    } else if (preset !== 'forever') date.setDate(date.getDate() + (preset === '1d' ? 1 : 7));
    const next = preset === 'forever' ? null : date.toISOString();
    setChoice({ value: next, preset });
    onValueChange(next);
  }
  return <fieldset className={`ui-date-time-picker ui-date-time-picker--${size} ${className}`} disabled={disabled} aria-describedby={hint ? `${id}-hint` : undefined} aria-invalid={error ? true : undefined}>
    <legend>{label}</legend>
    <div ref={scrollRef} className="ui-date-time-picker__buttons" role="group" aria-label={t.presets}>
      {presets.map(preset => <Button key={preset} variant="secondary" size={size} disabled={disabled} aria-pressed={selected === preset} title={selected === preset && value ? dateTitle : undefined} onClick={() => choose(preset)}>{t[preset]}</Button>)}
      <span className="ui-date-time-picker__date">
        <Popover width={480} maxHeight={520} title={mode === 'time' ? t.timeOnly : mode === 'month' ? t.monthAndYear : t.dateAndTime} open={open && !disabled} onOpenChange={setOpen} align="end" trigger={<Button variant="secondary" size={size} disabled={disabled} aria-label={t.chooseDate} aria-pressed={selected === 'custom'} title={dateTitle}><UIIcon name="calendar" /><span>{dateLabel}</span></Button>}>
          {open && <DateTimeCalendar value={value} min={min} mode={mode} timeZone={timeZone} onCancel={() => setOpen(false)} onApply={next => { setChoice({ value: next, preset: 'custom' }); onValueChange(next); setOpen(false); }} />}
        </Popover>
      </span>
    </div>
    {hint && <p id={`${id}-hint`} className={`field-hint${error ? ' field-hint--error' : ''}`}>{hint}</p>}
  </fieldset>;
}
