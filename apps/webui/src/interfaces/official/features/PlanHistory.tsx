import { Button, EmptyState } from '@molr/ui';
import { ChevronRight, History } from 'lucide-react';
import type { Plan, PlanExecutionSummary } from '@molr/sdk';
import { useCopy } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { planMessages } from '../../../plan-messages';
import { Loading } from '../components/common';
import { usePlanHistory } from './plan-events';
import { executionStart } from './PlanExecutionPanel';

export function PlanHistory({plan,onSelect}:{plan:Plan;onSelect:(run:PlanExecutionSummary)=>void}) {
  const {locale,t:copy}=useCopy(),t=planMessages[locale];
  const history=usePlanHistory(`/api/plans/${plan.Ref.Kind}/${plan.Ref.ID}/history?limit=20`);
  useErrorToast(history.error,history.reload);
  return <section className="plans-history" aria-label={t.executions}><h3 tabIndex={-1}><History size={15}/>{t.executions}{plan.ExecutionCount>0 && <span>{plan.ExecutionCount}</span>}</h3>
    {history.items.map(run=>{
      const started=executionStart(run),actual=started || run.CompletedAt || run.StartedAt,at=run.OccurrenceAt || actual;
      const format=(value:string)=>new Date(value).toLocaleString(locale,{month:'short',day:'numeric',hour:'2-digit',minute:'2-digit',second:'2-digit'});
      return <button type="button" className="plans-history-entry" key={run.ID} data-execution-id={run.ID} onClick={()=>onSelect(run)}>
        <span className="plans-run-dot" data-state={run.State}/><span><strong>{run.OccurrenceAt && `${t.scheduled} · `}<time dateTime={at}>{format(at)}</time></strong><small>{copy.tasks[run.State as 'pending'] ?? run.State}</small>{run.OccurrenceAt && <small>{started?t.startTime:run.CompletedAt?t.endTime:t.triggeredAt} · <time dateTime={actual}>{format(actual)}</time></small>}</span><ChevronRight size={14}/>
      </button>;
    })}
    {!history.items.length && (history.loading ? <Loading/> : history.error ? <EmptyState className="plans-history-empty" title={t.historyUnavailable} action={<Button size="sm" variant="secondary" onClick={history.reload}>{copy.retry}</Button>}/> : <EmptyState className="plans-history-empty" illustration={<History/>} title={t.noExecutionRecords} description={t.executionRecordsHelp}/>)}
    {history.more && <Button size="sm" variant="secondary" loading={history.loading} onClick={history.loadMore}>{locale==='zh'?'加载更多记录':'Load more history'}</Button>}
  </section>;
}
