import { useLayoutEffect, useRef, useState } from 'react';
import type { CSSProperties, KeyboardEvent } from 'react';
import { Card, IconButton, SegmentedControl, Tooltip, useBreakpoint } from '@molr/ui';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { useCopy } from '../state';
import { amountLabel, amountMicros, calendarDate, compactTokens, dateAdd, emptyUsage } from './usage-analytics';
import type { UsageAnalyticsView, UsageDay } from './usage-analytics';

export function UsageCalendar({ data, selected, onSelect }: { data: UsageAnalyticsView; selected: string; onSelect: (day: string) => void }) {
 const { locale } = useCopy(), zh = locale === 'zh', mobile = useBreakpoint('mobile');
 const [metric, setMetric] = useState('tokens'), [month, setMonth] = useState('');
 const gridRef = useRef<HTMLDivElement>(null);
 const [visibleWeeks, setVisibleWeeks] = useState(1);
 useLayoutEffect(() => {
  const grid = gridRef.current;
  if (mobile || !grid) return;
  const observer = new ResizeObserver(([entry]) => {
   const style = getComputedStyle(grid);
   const cellSize = parseFloat(style.getPropertyValue('--usage-cell-size'));
   const gap = parseFloat(style.columnGap);
   setVisibleWeeks(Math.max(1, Math.floor((entry.contentRect.width + gap) / (cellSize + gap))));
  });
  observer.observe(grid);
  return () => observer.disconnect();
 }, [mobile, data.Daily.length]);
 const today = calendarDate(new Date(data.AsOf), data.Timezone);
 const months = [...new Set(data.Daily.map(d => d.Date.slice(0, 7)))];
 const shownMonth = months.includes(month) ? month : months.includes(today.slice(0, 7)) ? today.slice(0, 7) : months.at(-1)!;
 const days = mobile ? data.Daily.filter(d => d.Date.startsWith(shownMonth)) : data.Daily;
 const value = (d: UsageDay) => { try { return metric === 'tokens' ? d.TotalTokens : amountMicros(d.Spent, data); } catch { return 0; } };
 const max = Math.max(0, ...data.Daily.map(value));
 const thresholds = [1, 2, 3, 4].map(v => Math.ceil(max * v / 4));
 const level = (d: UsageDay) => !value(d) ? 0 : Math.min(4, Math.ceil(value(d) / (max || 1) * 4));
 const detail = (d: UsageDay) => {
  if (d.Date > today) return `${d.Date}\n${zh ? '未来日期' : 'Future date'}`;
  if (d.Date < data.From || d.Date >= data.Until) return `${d.Date}\n${zh ? '不在统计范围内' : 'Outside selected range'}`;
  const lines = [d.Date, metric === 'tokens' ? `${d.TotalTokens.toLocaleString(locale)} Token` : amountLabel(d.Spent, data, locale)];
  if (metric === 'tokens' && d.MissingTokenCalls) lines.push(zh ? '部分 Token 用量未知' : 'Some token usage is unknown');
  if (metric === 'spend' && d.UnknownCostCalls) lines.push(zh ? '部分费用未知' : 'Some costs are unknown');
  if (d.ActiveCalls) lines.push(zh ? '包含执行中的调用' : 'Includes calls in progress');
  return lines.join('\n');
 };
 const first = mobile ? `${shownMonth}-01` : days[0]?.Date;
 if (!first) return null;
 const offset = (new Date(`${first}T12:00:00Z`).getUTCDay() + 6) % 7;
 const last = mobile ? dateAdd(`${Number(shownMonth.slice(0, 4)) + (shownMonth.endsWith('-12') ? 1 : 0)}-${String(Number(shownMonth.slice(5)) % 12 + 1).padStart(2, '0')}-01`, -1) : days.at(-1)!.Date;
 const cells: (UsageDay | null)[] = Array.from({ length: offset }, () => null);
 const byDate = new Map(days.map(d => [d.Date, d]));
 for (let d = first; d <= last; d = dateAdd(d, 1)) cells.push(byDate.get(d) ?? { ...emptyUsage, Date: d });
 const totalWeeks = Math.ceil(cells.length / 7);
 const weeks = mobile ? totalWeeks : Math.min(totalWeeks, visibleWeeks);
 // Keep the latest elapsed week visible when the selected range includes future dates.
 const latestDay = cells.reduce((latest, d, i) => d && d.Date <= today ? i : latest, -1);
 const endWeek = Math.min(totalWeeks, Math.max(weeks, Math.floor(latestDay / 7) + 1));
 const visibleCells = mobile ? cells : cells.slice((endWeek - weeks) * 7, endWeek * 7);
 const firstVisibleDay = visibleCells.findIndex(d => d !== null);
 const monthLabels = visibleCells.flatMap((d, i) => d && (d.Date.endsWith('-01') || i === firstVisibleDay) ? [{ date: d.Date, week: Math.floor(i / 7) }] : []);
 const shownLabels = monthLabels.filter((label, i) => i > 0 || monthLabels.length === 1 || monthLabels[1].week - label.week >= 2);
 const keyMove = (e: KeyboardEvent<HTMLButtonElement>) => {
  const delta: Record<string, number> = mobile ? { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 } : { ArrowLeft: -7, ArrowRight: 7, ArrowUp: -1, ArrowDown: 1 };
  if (!(e.key in delta)) return;
  e.preventDefault();
  const target = dateAdd(e.currentTarget.dataset.date!, delta[e.key]);
  e.currentTarget.parentElement?.querySelector<HTMLButtonElement>(`button[data-date="${target}"]:not(:disabled)`)?.focus();
 };
 return <Card role="region" className="usage-calendar" aria-label={zh ? '每日消耗' : 'Daily usage'}>
  <div className="usage-section-heading"><div><h3>{zh ? '每日消耗' : 'Daily usage'}</h3><p>{zh ? '点击日期，查看当天明细' : 'Select a date to inspect its breakdown'}</p></div><SegmentedControl hideLabel label={zh ? '日历指标' : 'Calendar metric'} value={metric} onValueChange={setMetric} options={[{ value: 'tokens', label: 'Token' }, { value: 'spend', label: zh ? '金额' : 'Spend' }]} /></div>
  {mobile && <div className="usage-month-nav"><IconButton variant="ghost" size="sm" label={zh ? '上个月' : 'Previous month'} disabled={months.indexOf(shownMonth) === 0} onClick={() => setMonth(months[months.indexOf(shownMonth) - 1])}><ChevronLeft size={16} /></IconButton><strong>{shownMonth}</strong><IconButton variant="ghost" size="sm" label={zh ? '下个月' : 'Next month'} disabled={months.indexOf(shownMonth) === months.length - 1} onClick={() => setMonth(months[months.indexOf(shownMonth) + 1])}><ChevronRight size={16} /></IconButton></div>}
  <div className={`usage-calendar-layout ${mobile ? 'is-month' : ''}`} style={{ '--usage-weeks': weeks } as CSSProperties}>
   {!mobile && <div className="usage-month-labels" style={{ '--usage-weeks': weeks } as CSSProperties}>{shownLabels.map(({ date, week }) => <span key={date} style={{ gridColumn: week + 1 }}>{new Intl.DateTimeFormat(locale, { month: 'short', timeZone: 'UTC' }).format(new Date(`${date}T12:00:00Z`))}</span>)}</div>}
   <div className="usage-weekdays" aria-hidden="true">{(zh ? ['一', '二', '三', '四', '五', '六', '日'] : ['M', 'T', 'W', 'T', 'F', 'S', 'S']).map((d, i) => <span key={i}>{d}</span>)}</div>
   <div ref={gridRef} className="usage-calendar-grid" style={{ '--usage-weeks': weeks } as CSSProperties}>
    {visibleCells.map((d, i) => !d ? <span key={`empty-${i}`} /> : <Tooltip key={d.Date} content={<span className="usage-day-tooltip">{detail(d)}</span>}><button type="button" className="usage-day" data-level={level(d)} data-date={d.Date} data-unknown={Boolean(metric === 'tokens' ? d.MissingTokenCalls : d.UnknownCostCalls)} data-active={d.ActiveCalls > 0} disabled={d.Date > today || d.Date < data.From || d.Date >= data.Until} aria-label={detail(d)} aria-pressed={selected === d.Date} onKeyDown={keyMove} onClick={() => onSelect(selected === d.Date ? '' : d.Date)}>{mobile ? Number(d.Date.slice(-2)) : null}{(metric === 'tokens' ? d.MissingTokenCalls : d.UnknownCostCalls) > 0 && <span className="usage-day-mark" aria-hidden="true">?</span>}</button></Tooltip>)}
   </div>
  </div>
  <div className="usage-calendar-legend"><span>{zh ? '少' : 'Less'}</span>{[0, 1, 2, 3, 4].map(n => <Tooltip key={n} content={n === 0 ? '0' : `${n === 1 ? '> 0' : `> ${metric === 'tokens' ? thresholds[n - 2].toLocaleString(locale) : amountLabel([{ Currency: data.Currency, Micros: thresholds[n - 2] }], data, locale)}`} – ${metric === 'tokens' ? thresholds[n - 1].toLocaleString(locale) + ' Token' : amountLabel([{ Currency: data.Currency, Micros: thresholds[n - 1] }], data, locale)}`}><span tabIndex={0} className="usage-legend-cell" data-level={n} /></Tooltip>)}<span>{zh ? '多' : 'More'}</span><span className="usage-legend-range">{metric === 'tokens' ? `0 – ${compactTokens(max, locale)} Token` : `0 – ${amountLabel([{ Currency: data.Currency, Micros: max }], data, locale)}`}</span>{data.Summary.MissingTokenCalls + data.Summary.UnknownCostCalls > 0 && <span>{zh ? '? 含未知用量' : '? Includes unknown usage'}</span>}</div>
 </Card>;
}
