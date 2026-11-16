import { useEffect, useState, type ReactNode } from 'react';
import { Badge } from '@molr/ui';
import type { Plan, PlanExecution, PlanExecutionSummary } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { useErrorToast } from '../../../error-toasts';
import { planMessages } from '../../../plan-messages';
import { Loading } from '../components/common';
import { PlanDescription } from './PlanDescription';
import { ExecutionDetails, ExecutionSpend } from './ExecutionDetails';
import { occurrenceExecution } from './plan-events';

export function executionStart(run: PlanExecutionSummary) {
  return run.RunStartedAt || (!run.TaskState && (!run.CompletedAt || Date.parse(run.StartedAt)!==Date.parse(run.CompletedAt)) ? run.StartedAt : undefined);
}
function useOccurrenceDue(at?:string) {
  const [now,setNow]=useState(Date.now);
  useEffect(()=>{
    if(!at)return;
    const deadline=Date.parse(at);
    if(!Number.isFinite(deadline))return;
    let timer:number|undefined;
    function syncOccurrenceTime(){
      window.clearTimeout(timer);
      if(document.visibilityState==='hidden')return;
      const current=Date.now();setNow(current);
      if(deadline>current)timer=window.setTimeout(syncOccurrenceTime,Math.min(deadline-current,2_147_483_647));
    }
    syncOccurrenceTime();
    document.addEventListener('visibilitychange',syncOccurrenceTime);
    return()=>{window.clearTimeout(timer);document.removeEventListener('visibilitychange',syncOccurrenceTime);};
  },[at]);
  return !!at && Date.parse(at)<=now;
}
export function PlanExecutionPanel({plan, at, executionID, actions, onHistory}: {plan:Plan; at?:string; executionID?:string; actions:(run?:PlanExecutionSummary)=>ReactNode; onHistory:()=>void}) {
  const {locale,t:copy}=useCopy(),t=planMessages[locale];
  const summary=executionID ? plan.Executions.find(run=>run.ID===executionID) : at ? occurrenceExecution(plan,at) : undefined;
  const id=executionID || summary?.ID;
  const details=useQuery<PlanExecution>(id ? `/api/plans/${plan.Ref.Kind}/${plan.Ref.ID}/executions/${id}` : null);
  useErrorToast(details.error,details.reload);
  const run=details.data || summary;
  const scheduledOccurrence=!!plan.Schedule && !!at;
  const due=useOccurrenceDue(scheduledOccurrence && !id ? at : undefined);
  const format=(value:string)=>new Date(value).toLocaleString(locale,{year:'numeric',month:'short',day:'numeric',hour:'2-digit',minute:'2-digit',second:'2-digit'});
  const started=run && executionStart(run),finished=run?.CompletedAt;
  const elapsed=started && finished ? Math.max(0,Math.round((Date.parse(finished)-Date.parse(started))/1000)) : null;
  const actualAt=started || finished;
  return <section className="plans-execution-panel" aria-label={t.currentExecution} data-execution-id={run?.ID}>
    <div className="plans-execution-heading"><h3 tabIndex={-1}>{t.currentExecution}</h3><button type="button" className="plans-history-link" onClick={onHistory}>{t.viewExecutions}</button></div>
    {details.error ? <p>{t.executionUnavailable}</p> : <>
      <dl className="plans-properties plans-execution-properties">
        <div><dt>{t.executionState}</dt><dd><Badge>{run ? copy.tasks[run.State as 'pending'] ?? run.State : id ? copy.loading : scheduledOccurrence ? due ? t.notExecuted : t.notStarted : t.noExecutions}</Badge></dd></div>
        {at && <div><dt>{t.scheduled}</dt><dd><time dateTime={at}>{format(at)}</time></dd></div>}
        {run && <>
          <div><dt>{t.startTime}</dt><dd>{started ? <time dateTime={started}>{format(started)}</time> : t.notRecorded}</dd></div>
          {finished && <div><dt>{t.endTime}</dt><dd><time dateTime={finished}>{format(finished)}</time></dd></div>}
          {elapsed !== null && <div><dt>{t.duration}</dt><dd>{elapsed < 60 ? `${elapsed} ${locale==='zh'?'秒':'s'}` : `${Math.floor(elapsed/60)} ${locale==='zh'?'分':'min'} ${elapsed%60} ${locale==='zh'?'秒':'s'}`}</dd></div>}
          {!actualAt && <div><dt>{t.triggeredAt}</dt><dd><time dateTime={run.StartedAt}>{format(run.StartedAt)}</time></dd></div>}
        </>}
        {details.data?.Task && <ExecutionSpend task={details.data.Task}/>}
      </dl>
      {(!id || run) && actions(run)}
      {id && !details.data ? <Loading/> : details.data && <div className="plans-run-content">
        {details.data.Result && <dl className="plans-properties plans-result"><div><dt>{t.result}</dt><dd className="plans-description"><PlanDescription key={details.data.ID} text={details.data.Result}/></dd></div></dl>}
        {details.data.Task && <ExecutionDetails key={details.data.ID} task={details.data.Task} execution={details.data}/>}
        <details key={`snapshot-${details.data.ID}`} className="plans-run-disclosure"><summary>{t.snapshot}</summary>
          <dl className="plans-properties"><div><dt>{t.source}</dt><dd className="plans-snapshot-source">{details.data.Title} · v{details.data.PlanVersion}</dd></div>
          <div><dt>{t.instructions}</dt><dd className="plans-description"><PlanDescription text={details.data.Instructions}/></dd></div></dl>
        </details>
      </div>}
    </>}
  </section>;
}
