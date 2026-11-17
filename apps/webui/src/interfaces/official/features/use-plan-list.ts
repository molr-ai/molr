import { useCallback, useEffect, useState, useSyncExternalStore } from 'react';
import { api } from '@molr/sdk';
import type { Plan, PlanEvent, PlanEventPage } from '@molr/sdk';
import { useData } from '../../../state';

type Source='events'|'watches'|'plans';
type Page={items:PlanEvent[];more:boolean;pages:number};
async function readPage(path:string,source:Source,signal:AbortSignal){
  if(source==='events'){
    const page=await api.get<PlanEventPage>(path,signal);
    return {items:page.Items.map(item=>({Key:item.Key,At:item.At,Plan:page.Plans[item.PlanKey]})),cursor:page.NextCursor};
  }
  const page=await api.get<Plan[]|{Items:Plan[];NextCursor:string}>(path,signal);
  const items=Array.isArray(page)?page:page.Items;
  return {items:items.map(Plan=>({Key:`${Plan.Ref.Kind}-${Plan.Ref.ID}`,At:'',Plan})),cursor:Array.isArray(page)?'':page.NextCursor};
}

// Keep snapshots in the session query store, including across page navigation.
// One active source refreshes its loaded pages serially on committed changes.
export function usePlanList(path:string|null,source:Source){
  const {queries,topics}=useData();
  const key=path?`plan-list:${JSON.stringify([path,source])}`:null;
  const [pages,setPages]=useState<{key:string|null;count:number}>({key:null,count:1});
  const count=pages.key===key?pages.count:queries.snapshot<Page>(key).data?.pages??1;
  const subscribe=useCallback((listener:()=>void)=>key?queries.subscribe(key,listener):()=>{},[queries,key]);
  const snapshot=useCallback(()=>queries.snapshot<Page>(key),[queries,key]);
  const state=useSyncExternalStore(subscribe,snapshot);
  const load=useCallback(async(signal:AbortSignal)=>{
    const items:PlanEvent[]=[];let cursor='';
    for(let i=0;i<count;i++){
      const page=await readPage(path!+(cursor?`&cursor=${encodeURIComponent(cursor)}`:''),source,signal);
      items.push(...page.items);cursor=page.cursor;if(!cursor)break;
    }
    return {items,more:!!cursor,pages:count};
  },[path,source,count]);
  useEffect(()=>{
    if(!key)return;
    const refresh=()=>queries.refresh(key,topics.version('plans'),load);
    const unsubscribe=topics.subscribe('plans',refresh);
    // Changing the page count must refresh even without a new domain event.
    queries.refresh(key,topics.version('plans'),load,true);
    return unsubscribe;
  },[queries,topics,key,load]);
  const reload=useCallback(()=>{if(key)queries.refresh(key,topics.version('plans'),load,true);},[queries,topics,key,load]);
  return {
    data:state.data,error:state.error,loading:!!path && (state.loading||!state.started),
    loadingMore:state.loading && !!state.data && count>state.data.pages,
    loadMore:()=>setPages({key,count:count+1}),reload,
  };
}
