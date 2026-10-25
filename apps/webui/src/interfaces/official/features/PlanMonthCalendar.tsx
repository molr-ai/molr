import { useId, useLayoutEffect, useRef, useState } from 'react';
import type { CSSProperties } from 'react';
import { planDate } from '@molr/ui';
import type { PlanEvent } from '@molr/sdk';
import { eventDone, eventsOnDay } from './plan-events';

export function monthDays(month: string) {
  const first=new Date(`${month.slice(0,7)}-01T12:00:00`), end=new Date(first.getFullYear(),first.getMonth()+1,0,12);
  first.setDate(first.getDate()-(first.getDay()+6)%7);
  const last=new Date(end);last.setDate(last.getDate()+6-(last.getDay()+6)%7);
  const days:Date[]=[];for(const date=new Date(first);date<=last;date.setDate(date.getDate()+1))days.push(new Date(date));
  return days;
}
export function PlanMonthCalendar({month,date,locale,events,onDay,onEvent,moreLabel}:{month:string;date:string;locale:string;events:PlanEvent[];onDay:(day:string)=>void;onEvent:(event:PlanEvent)=>void;moreLabel:(count:number,day:string)=>string}) {
  const id=useId(),days=monthDays(month),weeks=days.length/7;
  return <div className="plans-month-calendar" data-month={month.slice(0,7)} style={{'--calendar-weeks':weeks} as CSSProperties} aria-label={new Date(`${month.slice(0,7)}-01T12:00:00`).toLocaleDateString(locale,{year:'numeric',month:'long'})}>
    <div className="plans-month-weekdays" aria-hidden="true">{days.slice(0,7).map(day=><span key={day.getDay()}>{day.toLocaleDateString(locale,{weekday:'short'})}</span>)}</div>
    <div className="plans-month-grid">{days.map(day=>{const key=planDate(day);return <Day key={key} id={`${id}-${key}`} day={key} dayNumber={day.getDate()} outside={key.slice(0,7)!==month.slice(0,7)} selected={key===date} locale={locale} entries={eventsOnDay(events,key)} onDay={()=>onDay(key)} onEvent={onEvent} moreLabel={moreLabel} onNavigate={offset=>{const next=new Date(day);next.setDate(next.getDate()+offset);document.getElementById(`${id}-${planDate(next)}`)?.focus();}}/>;})}</div>
  </div>;
}
function Day({id,day,dayNumber,outside,selected,locale,entries,onDay,onEvent,onNavigate,moreLabel}:{id:string;day:string;dayNumber:number;outside:boolean;selected:boolean;locale:string;entries:PlanEvent[];onDay:()=>void;onEvent:(event:PlanEvent)=>void;onNavigate:(offset:number)=>void;moreLabel:(count:number,day:string)=>string}) {
  const area=useRef<HTMLDivElement>(null),[capacity,setCapacity]=useState(3);
  useLayoutEffect(()=>{
    const node=area.current;if(!node)return;
    const update=()=>{const style=getComputedStyle(node),row=parseFloat(style.getPropertyValue('--event-row-height')),gap=parseFloat(style.rowGap)||0;setCapacity(Math.max(3,Math.floor((node.clientHeight+gap)/(row+gap))));};
    update();const observer=new ResizeObserver(update);observer.observe(node);return()=>observer.disconnect();
  },[]);
  const overflow=entries.length>capacity,visible=entries.slice(0,overflow?capacity-1:capacity),remaining=entries.length-visible.length;
  return <div className="plans-month-day" data-day={day} data-outside={outside} data-selected={selected} data-capacity={capacity} data-event-count={entries.length}>
    <button id={id} type="button" className="plans-month-date" aria-label={`${day}, ${entries.length}`} aria-current={day===planDate()?'date':undefined} onClick={onDay} onKeyDown={event=>{const offset=({ArrowLeft:-1,ArrowRight:1,ArrowUp:-7,ArrowDown:7} as Record<string,number>)[event.key];if(offset){event.preventDefault();onNavigate(offset);}}}>{dayNumber}</button>
    <div className="plans-month-events" ref={area}>{visible.map(event=><button type="button" className="plans-month-event" key={event.Key} data-kind={event.Plan.Schedule?.Kind??'item'} data-completed={eventDone(event.Plan,event.At)} data-paused={event.Plan.Links.Paused} title={`${new Date(event.At).toLocaleString(locale)} · ${event.Plan.Title}`} onClick={()=>onEvent(event)}><time dateTime={event.At}>{new Date(event.At).toLocaleTimeString(locale,{hour:'2-digit',minute:'2-digit',hour12:false})}</time><span>{event.Plan.Title}</span></button>)}{overflow&&<button type="button" className="plans-month-more" aria-label={moreLabel(remaining,day)} onClick={onDay}>+{remaining}</button>}</div>
  </div>;
}
