// Calendar drafts use UTC fields as wall-clock fields, independent of the
// browser zone. Only the picker boundary converts to/from actual instants.
export function wallDate(year: number, month: number, day = 1, hour = 0, minute = 0) {
  return new Date(Date.UTC(year, month, day, hour, minute));
}
export function wallTime(value: string | number, timeZone?: string) {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(new Date(value));
  const n = (type: string) => Number(parts.find(part => part.type === type)?.value);
  return wallDate(n('year'), n('month') - 1, n('day'), n('hour'), n('minute'));
}
export function wallInstant(date: Date, timeZone?: string, original?: string | null): string | null {
  const target = date.getTime();
  if (original && wallTime(original, timeZone).getTime() === target) return new Date(original).toISOString();
  let instant = target;
  for (let i = 0; i < 6; i++) {
    const actual = wallTime(instant, timeZone).getTime();
    if (actual === target) return new Date(instant).toISOString();
    instant += target - actual;
  }
  return null; // A nonexistent DST wall time must not silently move forward.
}
