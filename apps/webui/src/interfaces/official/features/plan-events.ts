import { useEffect, useState } from 'react';
import type { Plan, PlanEvent } from '../../../api/types';
import { api } from '../../../api/client';
import { useTopicVersion } from '../../../state';

export const planKey = (plan: Plan) => `${plan.Ref.Kind}-${plan.Ref.ID}`;
export function eventKey(plan: Plan, at?: string) { return `${planKey(plan)}@${at ?? ''}`; }
export function eventEnd(plan: Plan, at: string) {
  if (plan.Todo) return plan.Todo.EndAt ?? null;
  return plan.Schedule?.EndAt ? new Date(Date.parse(at) + Date.parse(plan.Schedule.EndAt) - Date.parse(plan.Schedule.Trigger.StartAt)).toISOString() : null;
}
export function eventDone(plan: Plan, at?: string) {
  const run = plan.Schedule?.Trigger.Recurrence ? plan.Executions.find(run => !!run.OccurrenceAt && !!at && Date.parse(run.OccurrenceAt) === Date.parse(at)) : plan.Executions[0];
  return run ? run.State === 'completed' : !!plan.Todo?.Completed;
}
export function calendarEvents(plans: Plan[]): PlanEvent[] {
  return plans.flatMap(plan => {
    const dates = plan.Todo ? [plan.Todo.StartAt || plan.Todo.DueAt].filter((at): at is string => !!at) : plan.Occurrences;
    return dates.map(At => ({ Key: eventKey(plan, At), At, Plan: plan }));
  }).sort((a,b) => Date.parse(a.At)-Date.parse(b.At) || a.Key.localeCompare(b.Key));
}
export function eventsOnDay(events: PlanEvent[], day: string) {
  const start = new Date(`${day}T00:00:00`), end = new Date(start); end.setDate(end.getDate()+1);
  return events.filter(e => { const finish=eventEnd(e.Plan,e.At); return Date.parse(e.At)<end.getTime() && (finish ? Date.parse(finish)>start.getTime() : Date.parse(e.At)>=start.getTime()); });
}

// Reload all displayed pages on a plan change so removed/revised occurrences
// cannot survive in an appended page. Every cursor is scoped by the Runtime.
export function usePlanEvents(path: string | null) {
  const revision = useTopicVersion('plans');
  const [pages,setPages] = useState({path, count:1});
  const count=pages.path===path?pages.count:1;
  const [state,setState]=useState<{path:string|null;items:PlanEvent[];more:boolean;loading:boolean;error?:unknown}>({path:null,items:[],more:false,loading:false});
  const [retry,setRetry]=useState(0);
  useEffect(()=>{
    if(!path)return;
    const controller=new AbortController();
    setState(old=>({path,items:old.path===path?old.items:[],more:false,loading:true}));
    void (async()=>{
      const items:PlanEvent[]=[];let cursor='';
      for(let i=0;i<count;i++){
        const page=await api.get<{Items:PlanEvent[];NextCursor:string}>(path+(cursor?`&cursor=${encodeURIComponent(cursor)}`:''),controller.signal);
        items.push(...page.Items);cursor=page.NextCursor;if(!cursor)break;
      }
      if(!controller.signal.aborted)setState({path,items,more:!!cursor,loading:false});
    })().catch(error=>{if(!controller.signal.aborted)setState(old=>({...old,error,loading:false}));});
    return ()=>controller.abort();
  },[path,count,revision,retry]);
  return {items:state.path===path?state.items:[],more:state.path===path&&state.more,loading:!!path&&(state.path!==path||state.loading),error:state.path===path?state.error:undefined,loadMore:()=>setPages({path,count:count+1}),reload:()=>setRetry(n=>n+1)};
}
