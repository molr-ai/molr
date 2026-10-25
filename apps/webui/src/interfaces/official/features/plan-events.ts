import { useEffect, useRef, useState } from 'react';
import type { Plan, PlanEvent, PlanEventPage, PlanExecutionSummary } from '@molr/sdk';
import { poll } from '../../../polling';
import { api } from '@molr/sdk';
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
const eventPage = (page: PlanEventPage) => ({Items: page.Items.map(item => ({Key:item.Key, At:item.At, Plan:page.Plans[item.PlanKey]})), NextCursor:page.NextCursor});
const historyPage = (page: {Items:PlanExecutionSummary[];NextCursor:string}) => page;
export function usePlanEvents(path: string | null) { return usePlanPages(path,eventPage); }
export function usePlanHistory(path: string | null) { return usePlanPages(path,historyPage); }
function usePlanPages<T, P>(path: string | null, decode: (page:P)=>{Items:T[];NextCursor:string}) {
  const revision = useTopicVersion('plans');
  const [pages,setPages] = useState({path, count:1});
  const count=pages.path===path?pages.count:1;
  const [state,setState]=useState<{path:string|null;items:T[];more:boolean;loading:boolean;error?:unknown}>({path:null,items:[],more:false,loading:false});
  const [retry,setRetry]=useState(0);
  const refresh = useRef(() => {});
  useEffect(()=>{
    if(!path)return;
    setState(old=>({path,items:old.path===path?old.items:[],more:old.path===path&&old.more,loading:true}));
    const loop = poll(async signal => {
     try {
      const items:T[]=[];let cursor='';
      for(let i=0;i<count;i++){
        const page=decode(await api.get<P>(path+(cursor?`&cursor=${encodeURIComponent(cursor)}`:''),signal));
        items.push(...page.Items);cursor=page.NextCursor;if(!cursor)break;
      }
      if(!signal.aborted)setState({path,items,more:!!cursor,loading:false});
     } catch(error) { if(!signal.aborted)setState(old=>({...old,error,loading:false})); throw error; }
     return 30_000;
    }, false);
    refresh.current = () => loop.request(true);
    return ()=>{loop.stop(); refresh.current=()=>{};};
  },[path,count,decode]);
  useEffect(()=>refresh.current(),[path,count,revision,retry]);
  return {items:state.path===path?state.items:[],more:state.path===path&&state.more,loading:!!path&&(state.path!==path||state.loading),error:state.path===path?state.error:undefined,loadMore:()=>setPages({path,count:count+1}),reload:()=>setRetry(n=>n+1)};
}
