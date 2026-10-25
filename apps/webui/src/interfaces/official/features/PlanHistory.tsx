import { useEffect, useState } from 'react';
import { Button } from '@molr/ui';
import { Check, ChevronDown, Clock3, History } from 'lucide-react';
import type { Plan, PlanExecution, PlanExecutionSummary } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { planMessages } from '../../../plan-messages';
import { Loading } from '../components/common';
import { ExecutionDetails } from './ExecutionDetails';
import { usePlanHistory } from './plan-events';

export function PlanHistory({plan,selectedTaskID,onComplete}:{plan:Plan;selectedTaskID?:string;onComplete:(run:PlanExecution)=>void}) {
  const {locale}=useCopy(),t=planMessages[locale];
  const path=`/api/plans/${plan.Ref.Kind}/${plan.Ref.ID}`;
  const history=usePlanHistory(`${path}/history?limit=20`);
  // A task deep link can address an execution outside the first history page.
  const pinned=useQuery<PlanExecution>(selectedTaskID ? `${path}/executions/${selectedTaskID}` : null);
  useErrorToast(history.error || pinned.error,()=>{history.reload();pinned.reload();});
  const runs=pinned.data && !history.items.some(run=>run.ID===pinned.data!.ID) ? [pinned.data,...history.items] : history.items;
  return <section className="plans-history"><h3><History size={15}/>{t.executions}<span>{plan.ExecutionCount}</span></h3>
    {runs.map(run=><ExecutionRow key={`${path}/${run.ID}`} path={path} run={run} selected={run.ID===selectedTaskID} onComplete={onComplete}/>)}
    {history.loading && !runs.length ? <Loading/> : !runs.length && <div className="plans-history-empty"><Clock3 size={18}/><p>{t.noExecutions}</p></div>}
    {history.more && <Button size="sm" variant="secondary" loading={history.loading} onClick={history.loadMore}>{locale==='zh'?'加载更多记录':'Load more history'}</Button>}
  </section>;
}
function ExecutionRow({path,run,selected,onComplete}:{path:string;run:PlanExecutionSummary;selected:boolean;onComplete:(run:PlanExecution)=>void}) {
  const {locale,t:copy}=useCopy(),t=planMessages[locale];
  const [open,setOpen]=useState(selected);
  useEffect(()=>{if(selected)setOpen(true);},[selected]);
  const details=useQuery<PlanExecution>(open ? `${path}/executions/${run.ID}` : null);
  useErrorToast(details.error,details.reload);
  const at=run.OccurrenceAt || run.StartedAt,full=details.data;
  return <details className="plan-execution" open={open} onToggle={event=>setOpen(event.currentTarget.open)}>
    <summary><span className="plans-run-dot" data-state={run.State}/><span><strong>{new Date(at).toLocaleDateString(locale,{month:'short',day:'numeric'})}<small>{new Date(at).toLocaleTimeString(locale,{hour:'2-digit',minute:'2-digit'})}</small></strong><small>{copy.tasks[run.State as 'pending'] ?? run.State}</small></span><ChevronDown size={14}/></summary>
    {open && <div className="plans-run-content">{!full ? <Loading/> : <>
      <p className="plans-snapshot-source">{t.source}: {full.Title} · v{full.PlanVersion}</p><h4>{t.snapshot}</h4><p className="plan-instructions">{full.Instructions}</p>
      {full.Result && <div className="plans-result"><h4>{t.result}</h4><p>{full.Result}</p></div>}
      {!full.Task && full.State==='running' && <Button size="sm" onClick={()=>onComplete(full)}><Check size={14}/>{t.completeExecution}</Button>}
      {full.Task && <ExecutionDetails task={full.Task}/>}
    </>}</div>}
  </details>;
}
