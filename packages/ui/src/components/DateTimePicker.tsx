import { useId, useState } from 'react';
import { useUILocale } from '../locale';
import { Button } from './ui';
import { UIIcon } from './icons';
import { Popover } from './Popover';
import { DateTimeCalendar } from './DateTimeCalendar';
import { wallDate, wallTime, wallInstant } from './date-time-zone';
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
  const [invalidPreset, setInvalidPreset] = useState<{ value: string | null; timeZone?: string } | null>(null);
  const scrollRef = useChoiceScroll(`${selected}:${value}`);
  const validationError = error || (invalidPreset && invalidPreset.value === value && invalidPreset.timeZone === timeZone ? t.invalidTime : undefined);
  const hint = validationError || description;
  const format: Intl.DateTimeFormatOptions = { timeZone, ...(mode === 'month' ? { year: 'numeric', month: 'long' } : mode === 'time' ? { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' } : mode === 'date' ? { year: 'numeric', month: 'numeric', day: 'numeric' } : { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }) };
  const dateLabel = selected === 'custom' && value ? new Intl.DateTimeFormat(locale, format).format(new Date(value)) : t.chooseDate;
  const dateTitle = value ? new Intl.DateTimeFormat(locale, {...format, year: mode === 'time' ? undefined : 'numeric' }).format(new Date(value)) : t.chooseDate;
  function choose(preset: DateTimePreset) {
    let next: string | null = null;
    if (preset !== 'forever') {
      const now = new Date(), date = wallTime(now.getTime(), timeZone);
      if (preset === '1m') {
        // Clamp month-end in the requested timezone, not the browser timezone.
        const day = date.getUTCDate();
        date.setUTCDate(1);
        date.setUTCMonth(date.getUTCMonth() + 1);
        const lastDay = wallDate(date.getUTCFullYear(), date.getUTCMonth() + 1, 0).getUTCDate();
        date.setUTCDate(Math.min(day, lastDay));
      } else date.setUTCDate(date.getUTCDate() + (preset === '1d' ? 1 : 7));
      const instant = wallInstant(date, timeZone);
      if (!instant) { setInvalidPreset({ value, timeZone }); return; }
      next = new Date(Date.parse(instant) + now.getUTCSeconds() * 1000 + now.getUTCMilliseconds()).toISOString();
    }
    setInvalidPreset(null);
    setChoice({ value: next, preset });
    onValueChange(next);
  }
  return <fieldset className={`ui-date-time-picker ui-date-time-picker--${size} ${className}`} disabled={disabled} aria-describedby={hint ? `${id}-hint` : undefined} aria-invalid={validationError ? true : undefined}>
    <legend>{label}</legend>
    <div ref={scrollRef} className="ui-date-time-picker__buttons" role="group" aria-label={t.presets}>
      {presets.map(preset => <Button key={preset} variant="secondary" size={size} disabled={disabled} aria-pressed={selected === preset} title={selected === preset && value ? dateTitle : undefined} onClick={() => choose(preset)}>{t[preset]}</Button>)}
      <span className="ui-date-time-picker__date">
        <Popover width={480} maxHeight={520} title={mode === 'time' ? t.timeOnly : mode === 'month' ? t.monthAndYear : t.dateAndTime} open={open && !disabled} onOpenChange={setOpen} align="end" trigger={<Button variant="secondary" size={size} disabled={disabled} aria-label={t.chooseDate} aria-pressed={selected === 'custom'} title={dateTitle}><UIIcon name="calendar" /><span>{dateLabel}</span></Button>}>
          {open && <DateTimeCalendar value={value} min={min} mode={mode} timeZone={timeZone} onCancel={() => setOpen(false)} onApply={next => { setInvalidPreset(null); setChoice({ value: next, preset: 'custom' }); onValueChange(next); setOpen(false); }} />}
        </Popover>
      </span>
    </div>
    {hint && <p id={`${id}-hint`} role={validationError ? 'alert' : undefined} className={`field-hint${validationError ? ' field-hint--error' : ''}`}>{hint}</p>}
  </fieldset>;
}
