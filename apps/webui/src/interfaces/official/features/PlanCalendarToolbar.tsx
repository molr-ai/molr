import { useRef, useState } from 'react';
import { Button, DateTimeCalendar, IconButton, Popover, planDate, useUILocale } from '@molr/ui';
import { ChevronDown, ChevronLeft, ChevronRight } from 'lucide-react';
import { planMessages } from '../../../plan-messages';
import { weekDays } from './PlanWeekCalendar';

type CalendarMode = 'week' | 'month';
export function PlanCalendarToolbar({ date, mode, locale, onDate, onMode }: {
  date: string; mode: CalendarMode; locale: 'zh' | 'en';
  onDate: (date: string) => void; onMode: (mode: CalendarMode) => void;
}) {
  const t = planMessages[locale], { messages } = useUILocale();
  const [open, setOpen] = useState(false), trigger = useRef<HTMLButtonElement>(null);
  const anchor = new Date(`${date}T12:00:00`), days = weekDays(date);
  const today = planDate();
  const currentPeriod = mode === 'week'
    ? planDate(days[0]) === planDate(weekDays(today)[0])
    : date.slice(0, 7) === today.slice(0, 7);
  const period = mode === 'week'
    ? new Intl.DateTimeFormat(locale, { year: 'numeric', month: 'short', day: 'numeric' }).formatRange(days[0], days[6])
    : anchor.toLocaleDateString(locale, { year: 'numeric', month: 'long' });
  function shift(offset: number) {
    onDate(planDate(mode === 'week'
      ? new Date(anchor.getFullYear(), anchor.getMonth(), anchor.getDate() + offset * 7, 12)
      : new Date(anchor.getFullYear(), anchor.getMonth() + offset, 1, 12)));
  }
  function close() { setOpen(false); trigger.current?.focus({ preventScroll: true }); }
  return <div className="plans-calendar-toolbar">
    <div className="plans-calendar-return">{!currentPeriod && <Button size="sm" variant="ghost" onClick={() => onDate(planDate())}>{mode === 'week' ? t.returnToWeek : t.returnToMonth}</Button>}</div>
    <div className="plans-calendar-navigation">
      <IconButton size="sm" label={mode === 'week' ? t.previousWeek : t.previous} onClick={() => shift(-1)}><ChevronLeft size={16} /></IconButton>
      <Popover width={400} maxHeight={520} title={mode === 'week' ? messages.dateTime.dateAndTime : messages.dateTime.monthAndYear} open={open} onOpenChange={setOpen}
        trigger={<Button ref={trigger} className="plans-calendar-period-trigger" variant="ghost" size="sm" aria-label={`${mode === 'week' ? t.date : t.month}: ${period}`}><span>{period}</span><ChevronDown size={14} aria-hidden="true" /></Button>}>
        {open && <DateTimeCalendar value={anchor.toISOString()} mode={mode === 'week' ? 'date' : 'month'} onCancel={close} onApply={value => { onDate(planDate(new Date(value))); close(); }} />}
      </Popover>
      <IconButton size="sm" label={mode === 'week' ? t.nextWeek : t.next} onClick={() => shift(1)}><ChevronRight size={16} /></IconButton>
    </div>
    <div className="ui-choice-list ui-choice-list--segmented ui-choice-list--sm plans-calendar-modes" role="group" aria-label={t.calendarMode}>
      {(['week', 'month'] as const).map(value => <button type="button" key={value} aria-pressed={mode === value} onClick={() => { setOpen(false); onMode(value); }}>{value === 'week' ? t.week : t.monthView}</button>)}
    </div>
  </div>;
}
