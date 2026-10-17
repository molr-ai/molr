import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { Check, Clock3, ListTodo, Pause, Repeat2 } from 'lucide-react';
import { planDate } from '@molr/ui';
import type { PlanEvent } from '../../../api/types';
import { planMessages } from '../../../plan-messages';
import { eventDone, eventEnd, eventsOnDay } from './plan-events';

export function weekDays(date: string) {
  const monday = new Date(`${date}T12:00:00`);
  monday.setDate(monday.getDate() - (monday.getDay() + 6) % 7);
  return Array.from({ length: 7 }, (_, index) => {
    const day = new Date(monday); day.setDate(day.getDate() + index); return day;
  });
}
const minutes = (date: Date) => date.getHours() * 60 + date.getMinutes();

// Allocate columns per connected overlap group, including the minimum visible
// height of short events so that every event remains independently clickable.
function dayLayout(events: PlanEvent[], day: string) {
  const start = new Date(`${day}T00:00:00`), end = new Date(start); end.setDate(end.getDate() + 1);
  const rows = eventsOnDay(events, day).map(event => {
    const finish = eventEnd(event.Plan, event.At), at = new Date(event.At);
    const top = at < start ? 0 : Math.min(minutes(at), 1410);
    const bottom = finish ? Date.parse(finish) >= end.getTime() ? 1440 : minutes(new Date(finish)) : top;
    return { event, top, bottom: Math.min(1440, Math.max(top + 30, bottom)), column: 0, columns: 1 };
  }).sort((a, b) => a.top - b.top || b.bottom - a.bottom || a.event.Key.localeCompare(b.event.Key));
  let group: typeof rows = [], ends: number[] = [], groupEnd = -1;
  const finishGroup = () => { for (const row of group) row.columns = ends.length; };
  for (const row of rows) {
    if (row.top >= groupEnd) { finishGroup(); group = []; ends = []; }
    let column = ends.findIndex(end => end <= row.top);
    if (column < 0) column = ends.length;
    row.column = column; ends[column] = row.bottom; group.push(row);
    groupEnd = Math.max(...ends);
  }
  finishGroup(); return rows;
}

export function PlanWeekCalendar({ date, selectedDate, locale, events, selectedKey, onDay, onEvent }: {
  date: string; selectedDate: string; locale: 'zh' | 'en'; events: PlanEvent[]; selectedKey?: string;
  onDay: (day: string) => void; onEvent: (event: PlanEvent) => void;
}) {
  const t = planMessages[locale], days = weekDays(date), id = useId(), scroller = useRef<HTMLDivElement>(null);
  const [now, setNow] = useState(() => new Date());
  const week = planDate(days[0]);
  useEffect(() => { const timer = window.setInterval(() => setNow(new Date()), 30_000); return () => window.clearInterval(timer); }, []);
  useLayoutEffect(() => { if (scroller.current) scroller.current.scrollTop = 7 * 64 - 12; }, [week]);
  const time = (at: string) => new Date(at).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', hour12: false });
  const zone = Intl.DateTimeFormat(locale, { timeZoneName: 'shortOffset' }).formatToParts(now).find(part => part.type === 'timeZoneName')?.value;
  return <div className="plans-week-calendar" data-week={week}>
    <div className="plans-week-scroll" ref={scroller} role="region" aria-label={t.weekView} tabIndex={0}>
      <div className="plans-week-canvas">
        <div className="plans-week-heading">
          <span className="plans-week-zone" title={Intl.DateTimeFormat().resolvedOptions().timeZone}>{zone}</span>
          {days.map(day => {
            const key = planDate(day);
            return <button type="button" id={`${id}-${key}`} key={key} className="plans-week-date" data-selected={key === selectedDate} aria-current={key === planDate(now) ? 'date' : undefined}
              aria-label={day.toLocaleDateString(locale, { dateStyle: 'full' })} onClick={() => onDay(key)} onKeyDown={event => {
                const offset = event.key === 'ArrowLeft' ? -1 : event.key === 'ArrowRight' ? 1 : 0;
                if (!offset) return; event.preventDefault(); const next = new Date(day); next.setDate(next.getDate() + offset);
                document.getElementById(`${id}-${planDate(next)}`)?.focus();
              }}><span>{day.toLocaleDateString(locale, { weekday: 'short' })}</span><strong>{day.getDate()}</strong></button>;
          })}
        </div>
        <div className="plans-week-body">
          <div className="plans-week-hours" aria-hidden="true">{Array.from({ length: 24 }, (_, hour) => <span key={hour} style={{ top: `${hour / 24 * 100}%` }}>{String(hour).padStart(2, '0')}:00</span>)}</div>
          {days.map(day => {
            const key = planDate(day);
            return <div className="plans-week-day" data-day={key} data-today={key === planDate(now)} key={key}>
              {dayLayout(events, key).map(({ event, top, bottom, column, columns }) => {
                const finish = eventEnd(event.Plan, event.At), done = eventDone(event.Plan, event.At);
                const kind = event.Plan.Schedule?.Kind === 'assistant_plan' ? 'automatic' : event.Plan.Schedule ? 'event' : 'item';
                const Icon = done ? Check : event.Plan.Links.Paused ? Pause : kind === 'automatic' ? Repeat2 : kind === 'event' ? Clock3 : ListTodo;
                const label = `${event.Plan.Title} · ${t[kind]} · ${new Date(event.At).toLocaleString(locale)}${finish ? ` – ${new Date(finish).toLocaleString(locale)}` : ''}${done ? ` · ${t.done}` : ''}${event.Plan.Links.Paused ? ` · ${t.paused}` : ''}`;
                return <button type="button" key={event.Key} className="plans-week-event" data-kind={kind} data-completed={done} data-paused={event.Plan.Links.Paused} data-selected={event.Key === selectedKey} data-event-key={event.Key} data-compact={bottom - top === 30}
                  style={{ top: `${top / 1440 * 100}%`, height: `calc(${(bottom - top) / 1440 * 100}% - 2px)`, left: `calc(${column / columns * 100}% + 3px)`, width: `calc(${100 / columns}% - 6px)` }}
                  title={label} aria-label={label} onClick={() => onEvent(event)}>
                  <span className="plans-week-event-title"><Icon size={12} /><strong>{event.Plan.Title}</strong></span>
                  {bottom - top >= 45 && <time dateTime={event.At}>{time(event.At)}{finish ? ` – ${time(finish)}` : ''}</time>}
                </button>;
              })}
              {key === planDate(now) && <div className="plans-week-now" style={{ top: `${minutes(now) / 1440 * 100}%` }} aria-label={`${t.currentTime} ${time(now.toISOString())}`}><span>{time(now.toISOString())}</span></div>}
            </div>;
          })}
        </div>
      </div>
    </div>
    <div className="plans-week-legend">{(['item', 'event', 'automatic'] as const).map(kind => <span key={kind}><i data-kind={kind} />{t[kind]}</span>)}</div>
  </div>;
}
