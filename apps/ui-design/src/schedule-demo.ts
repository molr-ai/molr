export type DemoSchedule = {
  id: string; kind: 'person' | 'assistant'; assistantId: string; title: string;
  date: string; time: string; endTime: string; repeat: 'once' | 'daily' | 'weekdays' | 'weekly';
  detail: string; location: string; enabled: boolean;
};
export function localDate(date = new Date()) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
}
export function scheduleOccursOn(item: DemoSchedule, date: string) {
  if (date < item.date) return false;
  if (item.repeat === 'once') return date === item.date;
  if (item.repeat === 'daily') return true;
  const day = new Date(`${date}T12:00:00`).getDay();
  if (item.repeat === 'weekdays') return day !== 0 && day !== 6;
  return day === new Date(`${item.date}T12:00:00`).getDay();
}
export function initialSchedules(): DemoSchedule[] {
  return [
    { id: 's1', kind: 'person', assistantId: '', title: '散步 / Evening walk', date: localDate(), time: '18:30', endTime: '19:00', repeat: 'daily', detail: '留一点时间给自己。 / A little time for yourself.', location: '附近公园 / Local park', enabled: true },
    { id: 's2', kind: 'assistant', assistantId: 'personal', title: '出行准备提醒 / Travel preparation', date: localDate(), time: '09:00', endTime: '', repeat: 'weekdays', detail: '汇总旅行资料，提醒还没有确认的事项。 / Summarize travel notes and flag unconfirmed items.', location: '', enabled: true },
  ];
}
