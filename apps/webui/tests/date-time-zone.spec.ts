import {test,expect} from '@playwright/test';
import {wallDate,wallTime,wallInstant} from '../../../packages/ui/src/components/date-time-zone';
test('picker wall time preserves timezone, rejects DST gaps, and retains ambiguous instants',()=>{
 const instant='2026-09-30T09:15:00Z';expect(wallTime(instant,'Asia/Shanghai').getUTCHours()).toBe(17);expect(wallInstant(wallDate(2026,8,30,17,15),'Asia/Shanghai')).toBe('2026-09-30T09:15:00.000Z');
 expect(wallInstant(wallDate(2026,2,8,2,30),'America/New_York')).toBeNull();
 const repeated='2026-11-01T06:30:00Z';expect(wallInstant(wallTime(repeated,'America/New_York'),'America/New_York',repeated)).toBe('2026-11-01T06:30:00.000Z');
 expect(wallInstant(wallDate(2026,2,8,2,30),'UTC')).toBe('2026-03-08T02:30:00.000Z');
});

test('wallInstant reuses one formatter across original checks and DST iterations', () => {
 const original = Intl.DateTimeFormat;
 let constructions = 0;
 Intl.DateTimeFormat = new Proxy(original, {
  construct(target, args) { constructions++; return Reflect.construct(target, args); },
 });
 try {
  expect(wallInstant(wallDate(2026,2,8,2,30),'America/New_York','2026-03-07T07:30:00Z')).toBeNull();
  expect(constructions).toBe(1);
  const repeated = '2026-11-01T06:30:00Z';
  expect(wallInstant(wallDate(2026,10,1,1,30),'America/New_York',repeated)).toBe('2026-11-01T06:30:00.000Z');
  expect(constructions).toBe(2);
 } finally {
  Intl.DateTimeFormat = original;
 }
});
