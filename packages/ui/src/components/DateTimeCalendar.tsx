import { useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';
import { useUILocale } from '../locale';
import { Button } from './ui';
import { Select } from './Select';
import { UIIcon } from './icons';
import { TimeWheel } from './TimeWheel';
import { wallDate, wallTime, wallInstant } from './date-time-zone';

function key(date: Date) { return `${date.getUTCFullYear()}-${date.getUTCMonth()}-${date.getUTCDate()}`; }
function month(date: Date) { return wallDate(date.getUTCFullYear(), date.getUTCMonth(), 1); }
function isDisabledDay(date: Date, minimum: number, timeZone?: string) {
  return minimum !== -Infinity && Date.parse(wallInstant(wallDate(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate() + 1), timeZone) ?? '') <= minimum;
}

/** The picker editor uses the same buttons, selects and overlay as other UI. */
export function DateTimeCalendar({ value, min, timeZone, mode = 'datetime', onApply, onCancel }: { value: string | null; min?: string; timeZone?: string; mode?: 'datetime' | 'date' | 'time' | 'month'; onApply: (value: string) => void; onCancel: () => void }) {
  const { locale, messages } = useUILocale();
  const t = messages.dateTime;
  const minimum = min ? Date.parse(min) : -Infinity;
  const [draft, setDraft] = useState(() => wallTime(Math.ceil(Math.max(value ? Date.parse(value) : Date.now(), minimum) / 60_000) * 60_000, timeZone));
  const [view, setView] = useState(() => month(draft));
  const [focusRequest, setFocusRequest] = useState<string | null>(null);
  const grid = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    if (focusRequest) grid.current?.querySelector<HTMLButtonElement>(`[data-date="${focusRequest}"]`)?.focus();
  }, [focusRequest, view]);
  const today = wallTime(Date.now(), timeZone);
  const viewYear = view.getUTCFullYear(), viewMonth = view.getUTCMonth();
  const days = useMemo(() => {
    const start = wallDate(viewYear, viewMonth);
    const first = wallDate(viewYear, viewMonth, 1 - (start.getUTCDay() + 6) % 7);
    return Array.from({ length: 42 }, (_, index) => wallDate(first.getUTCFullYear(), first.getUTCMonth(), first.getUTCDate() + index));
  }, [viewYear, viewMonth]);
  const showDays = mode === 'date' || mode === 'datetime';
  const disabledDays = useMemo(() => new Set(showDays ? days.filter(date => isDisabledDay(date, minimum, timeZone)).map(key) : []), [days, minimum, timeZone, showDays]);
  const labels = useMemo(() => {
    const monthFormatter = new Intl.DateTimeFormat(locale, { month: 'short', timeZone: 'UTC' });
    const weekdayFormatter = new Intl.DateTimeFormat(locale, { weekday: 'narrow', timeZone: 'UTC' });
    return {
      months: Array.from({ length: 12 }, (_, value) => ({ value: String(value), label: monthFormatter.format(wallDate(2024, value)) })),
      weekdays: Array.from({ length: 7 }, (_, index) => weekdayFormatter.format(wallDate(2024, 0, 1 + index))),
      dayFormatter: new Intl.DateTimeFormat(locale, { dateStyle: 'full', timeZone: 'UTC' }),
    };
  }, [locale]);
  const dayLabels = useMemo(() => new Map(showDays ? days.map(date => [key(date), labels.dayFormatter.format(date)]) : []), [days, labels, showDays]);
  const selectedKey = key(draft);
  const focusKey = focusRequest ?? (days.some(date => key(date) === selectedKey) ? selectedKey : key(days.find(date => date.getUTCMonth() === view.getUTCMonth() && !disabledDays.has(key(date))) ?? days[0]));
  const yearStart = Math.min(new Date().getUTCFullYear() - 5, view.getUTCFullYear() - 5);
  const yearEnd = Math.max(new Date().getUTCFullYear() + 20, view.getUTCFullYear() + 5);
  function navigate(date: Date) { setFocusRequest(null); setView(month(date)); if (mode === 'month') setDraft(month(date)); }
  function choose(date: Date) {
    const next = wallDate(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate(), draft.getUTCHours(), draft.getUTCMinutes());
    setDraft(next); navigate(next);
  }
  function move(event: KeyboardEvent, date: Date) {
    const next = new Date(date);
    switch (event.key) {
      case 'ArrowLeft': next.setUTCDate(next.getUTCDate() - 1); break;
      case 'ArrowRight': next.setUTCDate(next.getUTCDate() + 1); break;
      case 'ArrowUp': next.setUTCDate(next.getUTCDate() - 7); break;
      case 'ArrowDown': next.setUTCDate(next.getUTCDate() + 7); break;
      case 'Home': next.setUTCDate(next.getUTCDate() - (next.getUTCDay() + 6) % 7); break;
      case 'End': next.setUTCDate(next.getUTCDate() + 6 - (next.getUTCDay() + 6) % 7); break;
      case 'PageUp': case 'PageDown': {
        const day = next.getUTCDate();
        next.setUTCDate(1); next.setUTCMonth(next.getUTCMonth() + (event.key === 'PageUp' ? -1 : 1) * (event.shiftKey ? 12 : 1));
        next.setUTCDate(Math.min(day, wallDate(next.getUTCFullYear(), next.getUTCMonth() + 1, 0).getUTCDate()));
        break;
      }
      default: return;
    }
    event.preventDefault();
    // Keyboard navigation may target a date outside the cached visible grid.
    if (!isDisabledDay(next, minimum, timeZone)) { setView(month(next)); setFocusRequest(key(next)); }
  }
  function time(part: 'hour' | 'minute', value: number) {
    setDraft(previous => {
      const next = new Date(previous);
      if (part === 'hour') next.setUTCHours(value); else next.setUTCMinutes(value);
      return next;
    });
  }
  const picked = mode === 'month' ? month(draft) : mode === 'date' ? wallDate(draft.getUTCFullYear(), draft.getUTCMonth(), draft.getUTCDate()) : draft;
  const instant = wallInstant(picked, timeZone, value);
  const invalid = !instant || Date.parse(instant) < minimum;
  return <div className="ui-date-calendar" data-mode={mode}>
    <div className="ui-date-calendar__columns">
      {mode !== 'time' && <div className="ui-date-calendar__date">
        <div className="ui-date-calendar__navigation">
          <Button size="sm" variant="ghost" aria-label={t.previousMonth} onClick={() => navigate(wallDate(view.getUTCFullYear(), view.getUTCMonth() - 1, 1))}><UIIcon name="chevron-left" /></Button>
          <Select size="sm" label={t.month} hideLabel value={String(view.getUTCMonth())} onValueChange={value => navigate(wallDate(view.getUTCFullYear(), Number(value), 1))} options={labels.months} />
          <Select size="sm" searchable label={t.year} hideLabel value={String(view.getUTCFullYear())} onValueChange={value => navigate(wallDate(Number(value), view.getUTCMonth(), 1))} options={Array.from({ length: yearEnd - yearStart + 1 }, (_, index) => ({ value: String(yearStart + index), label: String(yearStart + index) }))} />
          <Button size="sm" variant="ghost" aria-label={t.nextMonth} onClick={() => navigate(wallDate(view.getUTCFullYear(), view.getUTCMonth() + 1, 1))}><UIIcon name="chevron-right" /></Button>
        </div>
        {mode !== 'month' && <div role="grid" aria-label={t.calendar} className="ui-date-calendar__grid" ref={grid}>
          <div role="row" className="ui-date-calendar__weekdays">{labels.weekdays.map((label, index) => <span role="columnheader" key={index}>{label}</span>)}</div>
          {Array.from({ length: 6 }, (_, row) => <div role="row" key={row}>{days.slice(row * 7, row * 7 + 7).map(date => <div role="gridcell" aria-selected={key(date) === selectedKey} key={key(date)}><Button variant="ghost" size="sm" className="ui-date-calendar__day" data-outside={date.getUTCMonth() !== view.getUTCMonth() || undefined} data-date={key(date)} tabIndex={key(date) === focusKey ? 0 : -1} aria-label={dayLabels.get(key(date))} aria-pressed={key(date) === selectedKey} aria-current={key(date) === key(today) ? 'date' : undefined} disabled={disabledDays.has(key(date))} onClick={() => choose(date)} onKeyDown={event => move(event, date)}>{date.getUTCDate()}</Button></div>)}</div>)}
        </div>}
      </div>}
      {(mode === 'datetime' || mode === 'time') && <div className="ui-date-calendar__time">
        <TimeWheel label={t.hour} count={24} value={draft.getUTCHours()} onValueChange={value => time('hour', value)} />
        <TimeWheel label={t.minute} count={60} value={draft.getUTCMinutes()} onValueChange={value => time('minute', value)} />
      </div>}
    </div>
    {invalid && <p role="alert" className="field-hint field-hint--error">{instant ? t.beforeMinimum : t.invalidTime}</p>}
    <div className="ui-date-calendar__actions">{mode !== 'time' && <Button size="sm" variant="ghost" onClick={() => choose(wallTime(Math.max(Date.now(), minimum), timeZone))}>{t.today}</Button>}<span /><Button size="sm" variant="secondary" onClick={onCancel}>{t.cancel}</Button><Button size="sm" disabled={invalid} onClick={() => instant && onApply(instant)}>{t.apply}</Button></div>
  </div>;
}
