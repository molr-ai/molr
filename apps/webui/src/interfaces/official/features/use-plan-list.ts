import { useEffect, useRef, useState } from 'react';
import { api, APIError } from '@molr/sdk';
import type { Plan, PlanEvent, PlanEventPage } from '@molr/sdk';
import { poll } from '../../../polling';
import { useTopicVersion } from '../../../state';

type Source='events'|'watches'|'plans';
type Page={items:PlanEvent[];more:boolean};
async function readPage(path:string,source:Source,signal:AbortSignal){
  if(source==='events'){
    const page=await api.get<PlanEventPage>(path,signal);
    return {items:page.Items.map(item=>({Key:item.Key,At:item.At,Plan:page.Plans[item.PlanKey]})),cursor:page.NextCursor};
  }
  const page=await api.get<Plan[]|{Items:Plan[];NextCursor:string}>(path,signal);
  const items=Array.isArray(page)?page:page.Items;
  return {items:items.map(Plan=>({Key:`${Plan.Ref.Kind}-${Plan.Ref.ID}`,At:'',Plan})),cursor:Array.isArray(page)?'':page.NextCursor};
}

// One active list source. Refresh complete pages in place; filters never reuse other scopes' data.
export function usePlanList(path:string|null,source:Source){
  const key=JSON.stringify([path,source]),revision=useTopicVersion('plans');
  const [pages,setPages]=useState({key,count:1}),count=pages.key===key?pages.count:1;
  const [state,setState]=useState<{key:string;data?:Page;loading:boolean;error?:unknown}>({key:'',loading:false});
  const [retry,setRetry]=useState(0),refresh=useRef(()=>{});
  useEffect(()=>{
    if(!path)return;
    const loop=poll(async signal=>{
      setState(old=>({key,data:old.key===key?old.data:undefined,loading:true}));
      try{
        const items:PlanEvent[]=[];let cursor='';
        for(let i=0;i<count;i++){
          const page=await readPage(path+(cursor?`&cursor=${encodeURIComponent(cursor)}`:''),source,signal);
          items.push(...page.items);cursor=page.cursor;if(!cursor)break;
        }
        if(!signal.aborted)setState({key,loading:false,data:{items,more:!!cursor}});
      }catch(error){
        if(!signal.aborted)setState(old=>({key,loading:false,error,data:error instanceof APIError && [401,403,404].includes(error.status)?undefined:old.key===key?old.data:undefined}));
        throw error;
      }
      return false;
    },false);
    refresh.current=()=>loop.request(true);
    return()=>{loop.stop();refresh.current=()=>{};};
  },[key,path,source,count]);
  useEffect(()=>refresh.current(),[key,count,revision,retry]);
  return {
    data:path && state.key===key?state.data:undefined,
    loading:!!path && (state.key!==key||state.loading),
    error:path && state.key===key?state.error:undefined,
    loadMore:()=>setPages({key,count:count+1}),reload:()=>setRetry(n=>n+1),
  };
}
