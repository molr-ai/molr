import { DismissibleAlert } from '../components/DismissibleAlert';
import type { DreamWindow } from '../../../api/types';
import { useCopy, useQuery } from '../../../state';
import { ErrorNotice } from '../components/common';

export function DreamConflictNotice({ assistantID, startAt, timezone = 'UTC', recurrence = '', disabled = false }: { assistantID?: string; startAt?: string; timezone?: string; recurrence?: string; disabled?: boolean }) {
  const { locale } = useCopy(), zh = locale === 'zh';
  const path = !disabled && assistantID && startAt ? `/api/dreams/conflicts?assistant_id=${encodeURIComponent(assistantID)}&start_at=${encodeURIComponent(startAt)}&timezone=${encodeURIComponent(timezone)}&recurrence=${encodeURIComponent(recurrence)}` : null;
  const conflicts = useQuery<DreamWindow[]>(path);
  if (conflicts.error) return <ErrorNotice error={conflicts.error} retry={conflicts.reload} />;
  if (!conflicts.data?.length) return null;
  const format = (at: string, zone: string) => new Intl.DateTimeFormat(locale, { timeZone: zone, month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(new Date(at));
  return <DismissibleAlert noticeKey={JSON.stringify([path, conflicts.data.map(w => [w.AssistantID, w.Start, w.End])])} tone="warning" className="plans-dream-conflict" title={zh ? '与做梦时段重叠' : 'Overlaps a dream window'}>
    <p>{zh ? '该助理做梦期间不会处理其他事件，这次安排可能延后执行。' : 'This assistant cannot process other events while dreaming. This occurrence may run late.'}</p>
    {!!recurrence && <p>{zh ? '检查接下来 90 天的重复安排：' : 'Conflicts within 90 days of the next check period:'}</p>}
    <ul>{conflicts.data.slice(0, 3).map(w => <li key={`${w.AssistantID}-${w.Start}`}>{format(w.Start, w.Timezone)}–{new Intl.DateTimeFormat(locale, { timeZone: w.Timezone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(new Date(w.End))} · {w.Timezone}</li>)}</ul>
    {conflicts.data.length > 3 && <p>{zh ? `另有 ${conflicts.data.length - 3} 个重叠时段` : `${conflicts.data.length - 3} more overlapping windows`}</p>}
  </DismissibleAlert>;
}
