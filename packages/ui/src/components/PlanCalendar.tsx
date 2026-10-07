import { useId } from 'react';
export type PlanCalendarEntry = { id: string; title: string; kind: string; muted?: boolean };
export function planDate(date = new Date()) { return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`; }
export function planMonthDays(date: string) {
  const selected = new Date(`${date}T12:00:00`), first = new Date(selected.getFullYear(), selected.getMonth(), 1, 12);
  first.setDate(first.getDate() - (first.getDay() + 6) % 7);
  return Array.from({ length: 42 }, (_, i) => { const day = new Date(first); day.setDate(day.getDate() + i); return day; });
}
export function PlanCalendar({ date, month = date, compact = false, locale, entries, onSelect, label }: { date: string; month?: string; compact?: boolean; locale: string; entries: (date: string) => PlanCalendarEntry[]; onSelect: (date: string) => void; label: string }) {
  const id = useId(), days = planMonthDays(month);
  return <div className={`plan-calendar${compact ? " plan-calendar--compact" : ""}`} aria-label={label}>
    <div className="plan-calendar-weekdays" aria-hidden="true">{days.slice(0, 7).map(day => <span key={day.getDay()}>{day.toLocaleDateString(locale, { weekday: 'short' })}</span>)}</div>
    <div className="plan-calendar-grid">{days.map(day => { const key = planDate(day), rows = entries(key); return <button type="button" key={key} id={`${id}-${key}`} className="plan-calendar-day" data-outside={key.slice(0, 7) !== month.slice(0, 7)} aria-label={`${key}, ${rows.length}`} aria-pressed={key === date} aria-current={key === planDate() ? 'date' : undefined} onClick={() => onSelect(key)} onKeyDown={event => {
      const offset = ({ ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 } as Record<string, number>)[event.key];
      if (!offset) return; event.preventDefault(); const next = new Date(day); next.setDate(next.getDate() + offset); const value = planDate(next); onSelect(value); requestAnimationFrame(() => document.getElementById(`${id}-${value}`)?.focus());
    }}><span>{day.getDate()}</span><span className="plan-calendar-entries">{rows.slice(0, 2).map(row => <span key={row.id} data-kind={row.kind} data-muted={row.muted} title={row.title}><i aria-hidden="true" /><span>{row.title}</span></span>)}</span>{rows.length > 0 && <small>{rows.length}</small>}</button>; })}</div>
  </div>;
}
