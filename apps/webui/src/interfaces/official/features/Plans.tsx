import { useMutation } from '../../../use-mutation';
import { useErrorToast } from '../../../error-toasts';
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { ModalPresence, Button, Checkbox, DateTimePicker, Dialog, Drawer, DropdownMenu, IconButton, Select, Textarea, TextField, planDate, useBreakpoint, useChoiceScroll } from '@molr/ui';
import { ArrowRight, Bot, CalendarDays, Check, ChevronDown, ChevronLeft, Ellipsis, GitBranch, History, List, Pause, Pencil, Play, Plus, Repeat2, SlidersHorizontal, Trash2, UserRound, X } from 'lucide-react';
import { api, APIError } from '@molr/sdk';
import type { Assistant, Schedule, Todo, Watch, Plan, PlanRef, PlanEvent, PlanExecution, PlanExecutionSummary } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { Confirm, EditDialog, Loading, PageHeader, localDateTime, zonedInput, zonedInstant, useAction } from '../components/common';
import { planMessages } from '../../../plan-messages';
import { PlanHistory } from './PlanHistory';
import { PlanDescription } from './PlanDescription';
import { PlanExecutionPanel } from './PlanExecutionPanel';
import { useNavigation } from '../../../navigation';
import { DreamConflictNotice } from './DreamConflictNotice';
import { PlanCalendarToolbar } from './PlanCalendarToolbar';
import { PlanWeekCalendar, weekDays } from './PlanWeekCalendar';
import { PlanMonthCalendar, monthDays } from './PlanMonthCalendar';
import { calendarEvents, eventDone, eventKey, eventsOnDay, occurrenceExecution } from './plan-events';
import { usePlanList } from './use-plan-list';
import './plans.css';

type Entry = {kind:'watch';value:Watch} | { kind: 'item'; value: Todo } | { kind: 'event' | 'automatic'; value: Schedule };
const listScopes = ['upcoming', 'overdue', 'undated', 'watch'] as const;
const refKey = (ref: PlanRef) => `${ref.Kind}-${ref.ID}`;
const entry = (plan: Plan): Entry => plan.Watch ? {kind:'watch',value:plan.Watch} : plan.Todo ? { kind: 'item', value: plan.Todo } : { kind: plan.Schedule!.Kind === 'personal_schedule' ? 'event' : 'automatic', value: plan.Schedule! };
const instantDay = (value: string) => planDate(new Date(value));
function occurrenceEnd(plan: Plan, at: string) { const s = plan.Schedule; return s?.EndAt ? new Date(new Date(at).getTime() + new Date(s.EndAt).getTime() - new Date(s.Trigger.StartAt).getTime()).toISOString() : null; }


export function Plans({ assistants: availableAssistants, assistantID, selectedID: providedID, selectedTaskID: providedTaskID, embedded }: { assistants: Assistant[]; assistantID?: string; selectedID?: string; selectedTaskID?: string; embedded?: { docked?: boolean; onClose: () => void } }) {
  const { locale, t:copy } = useCopy(), t = planMessages[locale], action = useAction(), mobile = useBreakpoint('mobile');
  const navigation = useNavigation();
  const selectedID = providedID || (embedded ? navigation.planID : '');
  const selectedTaskID = navigation.planExecutionID || providedTaskID || '';
  const [selectedExecutionID,setSelectedExecutionID] = useState<string>();
  const appliedSelection = useRef('');
  const scopeID = useId(), listRef = useRef<HTMLDivElement>(null);
  const [view, setView] = useState(embedded ? 'list' : 'calendar'), [date, setDate] = useState(planDate()), [month, setMonth] = useState(planDate()), [owner, setOwner] = useState(assistantID || 'all'), [kind, setKind] = useState('all'), [status, setStatus] = useState('all');
  const [calendarMode, setCalendarMode] = useState<'week' | 'month'>('week');
  const [listScope, setListScope] = useState<(typeof listScopes)[number]>('upcoming'), [selectedAt, setSelectedAt] = useState<string>(), [dayAgenda, setDayAgenda] = useState('');
  const viewRef = useChoiceScroll(view);
  const [listFrom] = useState(() => new Date().toISOString());
  const backRef = useRef<HTMLButtonElement>(null), detailRef = useRef<HTMLDivElement>(null);
  const listScroll = useRef(0);
  const returnFocusKey = useRef('');
  const focusExecutionSection=useRef(false);
  const listOwner = embedded ? assistantID || '' : owner;
  const [expanded, setExpanded] = useState(''), [editing, setEditing] = useState<Entry | 'new' | null>(null), [deleting, setDeleting] = useState<Plan | null>(null), [configuring, setConfiguring] = useState<Plan | null>(null), [completing, setCompleting] = useState<{ plan: Plan; at?: string } | null>(null);
  const visibleDays=view === 'calendar' && calendarMode === 'week' ? weekDays(month) : monthDays(month);
  const from = new Date(visibleDays[0]);from.setHours(0,0,0,0);
  const until = new Date(visibleDays[visibleDays.length-1]);until.setDate(until.getDate()+1);until.setHours(0,0,0,0);
  const listing=!!embedded || view==='list',watchMode=listing && listScope==='watch';
  const listSource=watchMode?'watches':listScope==='upcoming'?'events':'plans';
  const list=usePlanList(!listing?null:watchMode?`/api/plans/watches?scope=${encodeURIComponent(listOwner)}&state=${status}&limit=${embedded?10:20}`
    :listScope==='upcoming'?`/api/plans/events?from=${encodeURIComponent(listFrom)}&limit=${embedded?10:50}&scope=${encodeURIComponent(listOwner)}&kind=${kind}&state=${status}`
    :`/api/plans?scope=${encodeURIComponent(listOwner)}${listScope==='undated'?'&mode=undated':''}`,listSource);
  const plans=useQuery<Plan[]>(!listing?`/api/plans?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(until.toISOString())}`:null);
  const selectedQuery = useQuery<Plan>(expanded ? `/api/plans/${expanded.replace('-', '/')}${selectedAt ? `?at=${encodeURIComponent(selectedAt)}` : ''}` : null);
  const related = useQuery<Plan[]>(expanded || configuring ? '/api/plans' : null);
  const selection = useQuery<{Plan:Plan;At:string|null}>(selectedID || selectedTaskID ? `/api/plans/selection?plan_id=${encodeURIComponent(selectedID || '')}&${selectedID?'execution_id':'task_id'}=${encodeURIComponent(selectedTaskID || '')}${!selectedTaskID && navigation.planAt?`&at=${encodeURIComponent(navigation.planAt)}`:''}` : null);
  const currentAssistant = useQuery<Assistant>(assistantID ? `/api/assistants/${assistantID}` : null);
  const assistants = currentAssistant.data && !availableAssistants.some(a => a.ID === assistantID) ? [...availableAssistants, currentAssistant.data] : availableAssistants;
  const all = [...new Map([...(plans.data ?? []), ...(list.data?.items.map(event=>event.Plan) ?? [])].map(plan=>[refKey(plan.Ref),plan])).values()], latest = (p: Plan) => p.Executions[0];
  const completed = (p: Plan) => p.Watch ? p.Watch.RunState==='completed' : latest(p)?.State === 'completed' || !p.Executions.length && !!p.Todo?.Completed;
  const filtered = all.filter(p => (listOwner === 'all' || (p.AssistantID || 'me') === listOwner) && (watchMode ? !!p.Watch : !p.Watch && (kind === 'all' || entry(p).kind === kind)) );
  const when = (p: Plan) => p.Todo?.StartAt || p.Todo?.DueAt || p.Schedule?.NextAt || p.Occurrences.find(at => instantDay(at) >= planDate()) || p.Schedule?.Trigger.StartAt || '';
  const matchesState=(p:Plan,at?:string)=>status==='all'||(status==='done'?eventDone(p,at):!eventDone(p,at));
  const scheduledEvents=calendarEvents(filtered).filter(e=>matchesState(e.Plan,e.At));
  const undated=filtered.filter(p=>p.Todo&&!when(p)&&matchesState(p));
  const overdue=filtered.filter(p=>!p.Schedule?.Trigger.Recurrence && !!when(p) && Date.parse(p.Todo?.EndAt||when(p))<Date.parse(listFrom) && !completed(p) && matchesState(p)).sort((a,b)=>Date.parse(when(a))-Date.parse(when(b)));
  const selectedDayEvents=eventsOnDay(scheduledEvents,dayAgenda||date);
  const state = (p: Plan) => p.Watch ? (t.watchStates[p.Watch.RunState as keyof typeof t.watchStates] ?? p.Watch.RunState) : p.State === 'paused' ? t.paused : t.enabled;
  const tone = (p: Plan) => p.State === 'paused' ? 'paused' : 'open';
  const executionState = state;
  const ownerName = (p: Plan) => assistants.find(a => a.ID === p.AssistantID)?.Name ?? (p.AssistantID ? t.assistant : t.me);
  const missingPlan = (error: unknown) => error instanceof APIError && error.status === 404;
  const unavailable = missingPlan(selectedQuery.error) || !!selection.error;
  const selected = unavailable ? undefined : selectedQuery.data ?? all.find(p => refKey(p.Ref) === expanded) ?? (selection.data && refKey(selection.data.Plan.Ref) === expanded ? selection.data.Plan : undefined);
  useLayoutEffect(() => {
    if (!embedded) return;
    if (!selected) {
      // Restore focus only after React has removed inert from the list.
      if (returnFocusKey.current) {
        document.getElementById(`${scopeID}-title-${returnFocusKey.current}`)?.focus({ preventScroll: true });
        returnFocusKey.current = '';
      }
      listRef.current?.scrollTo({ top: listScroll.current, behavior: 'instant' });
      return;
    }
    detailRef.current?.scrollTo({ top: 0, behavior: 'instant' });
    backRef.current?.focus({ preventScroll: true });
  }, [embedded && selected?.Ref.ID, selectedAt]);
  useLayoutEffect(()=>{
    if(!focusExecutionSection.current || !selectedQuery.data)return;
    const heading=document.getElementById(`${scopeID}-execution`)?.querySelector<HTMLElement>('h3');
    if(heading){heading.focus({preventScroll:true});heading.scrollIntoView({block:'nearest'});focusExecutionSection.current=false;}
  },[selectedQuery.data,selectedAt,selectedExecutionID]);
  const hasFilters = (!embedded && owner !== (assistantID || 'all')) || (!watchMode && kind !== 'all') || status !== 'all';
  const planScope=(p:Plan):typeof listScope=>p.Watch?'watch':!when(p)?'undated':!p.Schedule?.Trigger.Recurrence && Date.parse(p.Todo?.EndAt||when(p))<Date.parse(listFrom) && !completed(p)?'overdue':'upcoming';
  useEffect(() => {
    const request = `${selectedID || ''}/${selectedTaskID || ''}/${navigation.planAt}`;
    if (!selectedID && !selectedTaskID) { appliedSelection.current = ''; setExpanded('');setSelectedAt(undefined);setSelectedExecutionID(undefined);return; }
    if (appliedSelection.current === request) return;
    const match = selection.data?.Plan;
    if (match) {
      appliedSelection.current = request;
      const occurrenceAt=selectedTaskID ? selection.data?.At || undefined : navigation.planAt || undefined;
      const at=occurrenceAt || when(match);
      setExpanded(refKey(match.Ref));setSelectedAt(occurrenceAt);setSelectedExecutionID(selectedTaskID || undefined);
      if(selectedTaskID && navigation.planAt){appliedSelection.current=`${match.Ref.ID}/${selectedTaskID}/`;selectionURL(match,occurrenceAt,selectedTaskID,true);}
      if(embedded || match.Watch || !at){setListScope(planScope(match));setView('list');}else{setView('calendar');setMonth(instantDay(at));setDate(instantDay(at));}
    }
  }, [selection.data, selectedID, selectedTaskID,navigation.planAt]);
  useEffect(() => {
    if (selectedID || selectedTaskID) listRef.current?.querySelector('[data-selected="true"]')?.scrollIntoView({ block: 'nearest' });
  }, [expanded, selectedID, selectedTaskID]);
  const format = (at: string) => new Date(at).toLocaleString(locale, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  const shortDate = (at: string) => new Date(at).toLocaleDateString(locale, { month: 'short', day: 'numeric' });
  const time = (at: string) => new Date(at).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' });
  const occurrence = (p: Plan) => p.Schedule?.Trigger.Recurrence ? selectedAt : undefined;
  const repeat = (rule: string) => !rule ? t.once : rule === 'FREQ=DAILY' ? t.daily : rule === 'FREQ=WEEKLY' ? t.weekly : t.customRepeat;
  const pausePlan = useMutation([]);
  function pause(p: Plan) { if(p.Watch){void action.run(()=>(async()=>{const full=await api.get<Plan>(`/api/plans/watch/${p.Ref.ID}`);if(full.Watch!.Version!==p.Watch!.Version)throw new APIError('conflict');return pausePlan('/api/watches',{...watchRequest(full.Watch!),State:p.State === 'paused' ? 'active' : 'paused'},p.Watch!.Version,refKey(p.Ref));})());return;} void action.run(() => pausePlan('/api/plans/configure', { Ref: p.Ref, Dependencies: p.Links.Dependencies ?? [], Handoff: p.Links.Handoff, State: p.State === 'paused' ? 'active' : 'paused' }, p.Links.Version, refKey(p.Ref))); }
  const startPlan = useMutation([]);
  function start(p: Plan, selectedOccurrence?:string) {
    if(p.Watch){void action.run(()=>startPlan('/api/watches',{...watchRequest(p.Watch!),State:'active'},p.Watch!.Version,refKey(p.Ref)));return;}
    void action.run(async()=>{
      const run=await startPlan<PlanExecution>('/api/plans/execute',{Ref:p.Ref,Action:'start',OccurrenceAt:selectedOccurrence || occurrence(p) || null},p.Links.Version,refKey(p.Ref)+":"+(selectedOccurrence || occurrence(p) || ""));
      focusExecutionSection.current=true;choose(p,run.OccurrenceAt||undefined,run.ID);
    });
  }
  function executionActions(p:Plan,run?:PlanExecutionSummary,at?:string){
    if(p.Watch)return null;
    const occurrenceAt=run?.OccurrenceAt || at;
    if(p.Schedule?.Trigger.Recurrence && !occurrenceAt)return null;
    const active=run && ['pending','running','waiting','paused'].includes(run.TaskState || run.State);
    const done=run?.State==='completed' || !p.Executions.length && !!p.Todo?.Completed;
    const showStart=!active && !(!p.AssistantID && done),showComplete=!p.AssistantID && !done;
    if(!showStart && !showComplete)return null;
    return <div className="plans-primary-actions">{showStart && <Button size="sm" disabled={action.busy || !p.Ready} onClick={()=>start(p,occurrenceAt||undefined)}><Play size={14}/>{t.startExecution}</Button>}{showComplete && <Button size="sm" variant="secondary" disabled={action.busy || (!p.Ready && !active)} onClick={()=>setCompleting({plan:p,at:occurrenceAt||undefined})}><Check size={14}/>{t.completeExecution}</Button>}</div>;
  }

  function selectCalendarDate(value: string) { setMonth(value); setDate(value); setExpanded(''); setSelectedAt(undefined); setDayAgenda(''); }
  function selectDate(value: string) { setDate(value); setExpanded(''); setSelectedAt(undefined); setDayAgenda(value); }
  function selectionURL(p?:Plan, at?:string, executionID?:string, replace=false) {
    const url=new URL(location.href);
    if(embedded){
      const [path,search='']=url.hash.slice(1).split('?'),query=new URLSearchParams(search);
      for(const key of ['plan','at','task','execution'])query.delete(key);
      if(p){query.set('plan',p.Ref.ID);if(executionID)query.set('execution',executionID);else if(at)query.set('at',at);}
      url.hash=path+(query.size?`?${query}`:'');
    }else{
      const query=new URLSearchParams();if(executionID)query.set('execution',executionID);else if(at)query.set('at',at);
      url.hash=`/plans${p?`/${p.Ref.ID}`:''}${query.size?`?${query}`:''}`;
    }
    if(url.href!==location.href){history[replace?'replaceState':'pushState'](history.state,'',url);window.dispatchEvent(new HashChangeEvent('hashchange'));}
  }
  function choose(p:Plan,at?:string,executionID?:string,replace=false){
    const run=executionID ? p.Executions.find(run=>run.ID===executionID) : at ? occurrenceExecution(p,at) : undefined;
    executionID=executionID || run?.ID;at=run?.OccurrenceAt || at;
    appliedSelection.current=`${p.Ref.ID}/${executionID||''}/${executionID?'':at||''}`;
    setExpanded(refKey(p.Ref));setSelectedAt(at);setSelectedExecutionID(executionID);setDayAgenda('');selectionURL(p,at,executionID,replace);
  }
  useEffect(()=>{
    if(!selected || selected.Ref.ID!==selectedID || selectedTaskID || !navigation.planAt || selectedAt!==navigation.planAt)return;
    const run=occurrenceExecution(selected,selectedAt);
    if(run)choose(selected,run.OccurrenceAt || selectedAt,run.ID,true);
  },[selected,selectedID,selectedTaskID,navigation.planAt,selectedAt]);
  function selectEvent(event: PlanEvent) { focusExecutionSection.current=false;choose(event.Plan,event.At); }
  function selectPlan(p: Plan, at?: string) {
    focusExecutionSection.current=false;
    if (expanded === refKey(p.Ref) && selectedAt === at) { closeDetails(); return; }
    if (embedded) listScroll.current = listRef.current?.scrollTop ?? 0;
    choose(p,at);
  }
  useErrorToast(list.error || plans.error || (missingPlan(selectedQuery.error) ? null : selectedQuery.error) || related.error || (missingPlan(selection.error) ? null : selection.error) || action.error || currentAssistant.error, ()=>{list.reload();plans.reload();selectedQuery.reload();related.reload();selection.reload();});
  function selectView(value: string) {
    if (value === view) return;
    setView(value); setExpanded(''); setSelectedAt(undefined); setDayAgenda('');
  }
  function selectListScope(value: (typeof listScopes)[number]) {
    if (value === listScope && view === 'list') return;
    setView('list'); setListScope(value); setExpanded(''); setSelectedAt(undefined); setDayAgenda('');
    listRef.current?.scrollTo({ top: 0, behavior: 'instant' });
  }
  function clearFilters() { setOwner(assistantID || 'all'); setKind('all'); setStatus('all'); }
  function closeDetails() {
    focusExecutionSection.current=false;
    if (embedded) returnFocusKey.current = `${expanded}@${selectedAt || ''}`;
    setExpanded(''); setSelectedAt(undefined);setSelectedExecutionID(undefined);selectionURL();
  }
  function openRelated(ref: PlanRef) {
    const linked=(related.data ?? all).find(p=>refKey(p.Ref)===refKey(ref)),at=linked?when(linked):'';
    if (embedded && linked?.AssistantID !== assistantID) { window.location.hash = `/plans/${ref.ID}`; return; }
    setOwner('all');setKind('all');setStatus('all');if(linked)choose(linked);
    if(embedded || linked?.Watch || !at){if(linked)setListScope(planScope(linked));setView('list');}else{setView('calendar');setMonth(instantDay(at));setDate(instantDay(at));}
  }
  function menu(p: Plan) {
    const canDelete = !p.HasSuccessors;
    return <DropdownMenu label={t.actions} align="end" trigger={<IconButton className="plans-more" label={t.actions}><Ellipsis size={18} /></IconButton>} items={[
      { id: 'edit', label: t.edit, icon: <Pencil size={15} />, onSelect: () => { void action.run(async()=>{const full=await api.get<Plan>(`/api/plans/${p.Ref.Kind}/${p.Ref.ID}`);setEditing(entry(full));}); } },
      { id: 'handoff', disabled:!!p.Watch, label: t.handoff, icon: <GitBranch size={15} />, onSelect: () => setConfiguring(p) },
      { id: 'pause', label: p.State === 'paused' ? t.resumePlan : t.pausePlan, icon: p.State === 'paused' ? <Play size={15} /> : <Pause size={15} />, disabled: action.busy, onSelect: () => pause(p) },
      { id: 'delete', label: t.remove, icon: <Trash2 size={15} />, danger: true, separatorBefore: true, disabled: !canDelete, disabledReason: !canDelete ? t.deleteBlocked : undefined, onSelect: () => setDeleting(p) },
    ]} />;
  }
  function detail(p: Plan) {
    const key = refKey(p.Ref), at = occurrence(p), specific=!!selectedAt || !!selectedExecutionID;
    const successors = (related.data ?? []).filter(next => next.Links.Dependencies?.some(ref => refKey(ref) === key));
    const startAt = p.Todo?.StartAt || p.Schedule?.Trigger.StartAt;
    const endAt = p.Todo?.EndAt || (startAt ? occurrenceEnd(p, startAt) : null);
    return <section id={`${scopeID}-detail-${key}`} className="plan-expanded plans-detail" role="region" aria-label={`${p.Title} · ${t.planDetails}`}>
      <div className="plans-detail-body">
        <h2 className="plans-detail-title">{p.Title}</h2>
        {p.Schedule?.Kind==='assistant_schedule' && <DreamConflictNotice assistantID={p.AssistantID} startAt={at || startAt || undefined} timezone={p.Schedule.Trigger.Timezone} recurrence={at?'':p.Schedule.Trigger.Recurrence} disabled={p.State === 'paused' || !p.Schedule.NextAt} />}
        <section className="plans-plan-information" aria-label={t.planInfo}><h3>{t.planInfo}</h3>
        <dl className="plans-properties"><div><dt>{t.owner}</dt><dd><span className="plans-avatar" data-person={!p.AssistantID}>{p.AssistantID ? <Bot size={13} /> : <UserRound size={13} />}</span>{ownerName(p)}</dd></div><div><dt>{t.planState}</dt><dd><span className="plans-status" data-tone={p.State==='paused'?'paused':'open'}><i />{p.State==='paused'?t.paused:t.enabled}</span></dd></div>
          {startAt && <div><dt>{t.scheduled}</dt><dd>{format(startAt)}{endAt && <span className="plans-end">– {instantDay(endAt) === instantDay(startAt) ? time(endAt) : format(endAt)}</span>}</dd></div>}
          {!startAt && !p.Todo?.DueAt && !p.Watch && <div><dt>{t.scheduled}</dt><dd>{t.noDate}</dd></div>}
          {p.Schedule?.NextAt && <div><dt>{t.nextExecution}</dt><dd>{format(p.Schedule.NextAt)}</dd></div>}
          {p.Todo?.DueAt && <div><dt>{t.deadline}</dt><dd>{format(p.Todo.DueAt)}</dd></div>}
          {p.Schedule && <div><dt>{t.repeat}</dt><dd title={p.Schedule.Trigger.Recurrence}>{repeat(p.Schedule.Trigger.Recurrence)}<span className="plans-subtle">{p.Schedule.Trigger.Timezone}</span></dd></div>}
          {(p.Watch?.Instructions || p.Todo?.Notes || p.Schedule?.Instructions) && <div><dt>{t.planDescription}</dt><dd className="plans-description"><PlanDescription key={key} text={p.Watch?.Instructions || p.Todo?.Notes || p.Schedule?.Instructions || ''}/></dd></div>}
        </dl>
        {p.Watch && <WatchDetails watch={p.Watch}/>}
        {p.Watch && <div className="plans-primary-actions"><Button size="sm" disabled={action.busy || p.Watch.RunState==='running' || p.Watch.RunState==='starting'} onClick={()=>start(p)}><Play size={14}/>{t.resumeWatch}</Button></div>}
        {!specific && !p.Schedule?.Trigger.Recurrence && executionActions(p,latest(p))}
        {!!p.Links.Dependencies?.length && <section className="plans-related"><h3><GitBranch size={14} />{t.dependencies}</h3>{p.Links.Dependencies.map(ref => { const linked = (related.data ?? []).find(d => refKey(d.Ref) === refKey(ref)); return <button key={refKey(ref)} onClick={() => openRelated(ref)}><span className="plans-related-dot" data-done={linked && completed(linked)} /><span>{linked?.Title ?? ref.ID}</span><ArrowRight size={14} /></button>; })}</section>}
        {successors.length > 0 && <section className="plans-related"><h3><ArrowRight size={14} />{t.successors}</h3>{successors.map(next => <button key={refKey(next.Ref)} onClick={() => openRelated(next.Ref)}><span>{next.Title}</span><small>{state(next)}</small><ArrowRight size={14} /></button>)}</section>}
        </section>
        <div id={`${scopeID}-execution`}>{specific ? <PlanExecutionPanel plan={p} at={selectedAt} executionID={selectedExecutionID} actions={run=>executionActions(p,run,at)} onHistory={()=>{focusExecutionSection.current=true;choose(p);}}/>
          : <PlanHistory plan={p} onSelect={run=>{focusExecutionSection.current=true;choose(p,run.OccurrenceAt||undefined,run.ID);}}/>}
        </div>

      </div>
    </section>;
  }
  function row(p: Plan, eventAt?: string) {
    const key = eventKey(p,eventAt), isSelected = expanded === refKey(p.Ref) && selectedAt === eventAt, at = eventAt || when(p), type = entry(p).kind;
    return <article id={`${scopeID}-row-${key}`} key={key} className={`plan-row plans-row ${embedded ? 'assistant-plan-row' : 'interactive-list-row'}`} data-selected={isSelected} data-completed={eventDone(p,eventAt)} data-occurrence={eventAt} data-plan-id={p.Ref.ID}>
      <div className="plans-row-summary">
        <span className="plans-type-dot" data-kind={type} role="img" aria-label={t[type]} title={t[type]} />
        <div className="plans-row-copy"><h2><button className="row-open" id={`${scopeID}-title-${key}`} type="button" aria-expanded={isSelected} aria-controls={isSelected ? `${scopeID}-detail-${refKey(p.Ref)}` : undefined} onClick={() => selectPlan(p,eventAt)}>{p.Title}</button></h2><div className="plans-row-meta"><span className="plans-owner" title={ownerName(p)}><span className="plans-avatar" data-person={!p.AssistantID}>{p.AssistantID ? <Bot size={11} /> : <UserRound size={11} />}</span>{ownerName(p)}</span>{at && <span className="plans-row-date" data-overdue={Date.parse(at)<Date.parse(listFrom)&&!eventDone(p,eventAt)}>{p.Todo?.DueAt && !p.Todo.StartAt ? t.deadline : ''}{p.Todo?.DueAt && !p.Todo.StartAt ? ' · ' : ''}{instantDay(at) === planDate() ? t.today : shortDate(at)}{` · ${time(at)}`}</span>}{p.Schedule?.Trigger.Recurrence && <Repeat2 size={12} aria-label={repeat(p.Schedule.Trigger.Recurrence)} />}<span className="plans-row-kind">{t[type]}</span><span className="plans-row-mobile-status">{eventDone(p,eventAt)?t.done:p.Schedule?.Trigger.Recurrence?t.planned:executionState(p)}</span></div></div>
        <span className="plans-status plans-row-status" data-tone={eventDone(p,eventAt)?'done':tone(p)}><i />{eventDone(p,eventAt)?t.done:p.Schedule?.Trigger.Recurrence?t.planned:executionState(p)}</span>
        <Button size="sm" variant="ghost" className="plans-row-history" aria-label={`${t.executions} (${p.ExecutionCount})`} aria-expanded={expanded===refKey(p.Ref)&&!selectedAt&&!selectedExecutionID} onClick={() => {if(embedded)listScroll.current=listRef.current?.scrollTop??0;choose(p);}}><History size={14} /><span>{p.ExecutionCount}</span></Button>
        {menu(p)}
      </div>
    </article>;
  }
  const listRows=watchMode || listScope==='upcoming'?[...(list.data?.items??[])]:listScope==='overdue'?overdue.map(p=>({Key:eventKey(p,when(p)),At:when(p),Plan:p})):undated.map(p=>({Key:eventKey(p),At:'',Plan:p}));
  if (embedded && selected && !!selected.Watch===watchMode && !listRows.some(event => eventKey(event.Plan, event.At) === eventKey(selected, selectedAt || when(selected)))) {
    const at = selectedAt || when(selected);
    listRows.unshift({ Key: eventKey(selected, at), At: at, Plan: selected });
  }
  const filters = <div className="plans-filters"><SlidersHorizontal size={14} className="plans-filter-icon" aria-hidden="true" />{!embedded && <Select hideLabel size="sm" label={t.owner} value={owner} onValueChange={setOwner} options={[{ value: 'all', label: t.allOwners }, { value: 'me', label: t.me }, ...assistants.map(a => ({ value: a.ID, label: a.Name }))]} />}{!watchMode && <Select hideLabel size="sm" label={t.type} value={kind} onValueChange={setKind} options={[{ value: 'all', label: t.allTypes }, ...(['item', 'event', 'automatic'] as const).map(value => ({ value, label: t[value] }))]} />}<Select hideLabel size="sm" label={t.status} value={status} onValueChange={setStatus} options={[{ value: 'all', label: t.allStatuses }, { value: 'open', label: t.open }, { value: 'done', label: t.done }]} />{hasFilters && <IconButton label={t.clearFilters} onClick={clearFilters}><X size={14} /></IconButton>}</div>;
  const selectedDetail = selected ? selectedQuery.data ? detail(selectedQuery.data) : <Loading /> : null;
  const deletePlan = useMutation([deleting]);
  const workspace = <section className={`plans-workspace${embedded ? ' plans-workspace--embedded' : ''}`} data-view={view} data-detail={embedded && !!selected}>
    {unavailable && <p role="status">{t.unavailable}</p>}
    {!embedded && <PageHeader className="plans-header" title={t.title} icon={<CalendarDays />} actions={watchMode?undefined:<Button size="sm" onClick={() => setEditing('new')}><Plus size={15} />{t.new}</Button>}><span className="plans-total">{listing&&!list.data?'—':filtered.length}</span></PageHeader>}
    {(!embedded || !selected) && <div className="plans-commandbar">
      {mobile && !embedded && filters}
      <div ref={viewRef} className="ui-choice-list ui-choice-list--line plans-view" role="group" aria-label={t.view}>
        <button type="button" aria-label={t.list} aria-describedby={`${scopeID}-list-scope`} aria-pressed={view === 'list'} onClick={() => selectView('list')}>
          <List size={14} />{t.list}<span id={`${scopeID}-list-scope`} className="plans-current-scope">· {t[listScope]}</span>
        </button>
        <DropdownMenu label={t.eventRange} trigger={<button type="button" className="plans-list-menu" data-active={view === 'list'} aria-label={t.eventRange}><ChevronDown size={14} /></button>}
          items={listScopes.map(value => {
            const count = value === 'overdue' ? overdue.length : value === 'undated' ? undated.length : 0;
            return { id: value, label: `${t[value]}${count > 0 ? ` (${count})` : ''}`, selected: listScope === value, onSelect: () => selectListScope(value) };
          })} />
        {!embedded && <button type="button" className="plans-calendar-tab" aria-pressed={view === 'calendar'} onClick={() => selectView('calendar')}><CalendarDays size={14} />{t.calendar}</button>}
      </div>
      {(embedded || !mobile) && filters}
    </div>}
    <div className="plans-content">
      <div className="plans-master" ref={listRef} inert={!!embedded && !!selected} aria-hidden={embedded && selected ? true : undefined}>
        {!embedded && view === 'calendar' ? <section className="plans-calendar-section" data-mode={calendarMode}>
          <PlanCalendarToolbar date={month} mode={calendarMode} locale={locale} onDate={selectCalendarDate} onMode={mode=>{setCalendarMode(mode);setExpanded('');setSelectedAt(undefined);setDayAgenda('');}}/>
          {plans.loading && !plans.data ? <Loading /> : calendarMode === 'week' ? <PlanWeekCalendar date={month} selectedDate={date} locale={locale} events={scheduledEvents} selectedKey={selected ? eventKey(selected,selectedAt) : undefined} onDay={selectDate} onEvent={selectEvent}/> : <PlanMonthCalendar month={month} date={date} locale={locale} events={scheduledEvents} onDay={selectDate} onEvent={selectEvent} moreLabel={(count,day)=>t.moreEvents(count,day)}/>}

        </section> : !list.data ? list.loading ? <Loading/> : <div className="plans-empty" role="status"><p>{t.listUnavailable}</p><Button size="sm" variant="secondary" onClick={list.reload}>{copy.retry}</Button></div> : <>
          <div className="plan-list plans-list" role={watchMode?'region':undefined} aria-label={watchMode?t.watch:t.eventList}>{listRows.map(event=>row(event.Plan,event.At||undefined))}</div>
          {list.data.more&&<Button className="plans-load-more" size="sm" variant="secondary" loading={list.loadingMore} onClick={list.loadMore}>{watchMode?t.loadMoreWatches:embedded?t.loadMoreItems:t.loadMoreEvents}</Button>}
          {!listRows.length&&<div className="plans-empty"><span className="plans-empty-icon"><CalendarDays size={32}/></span><h2>{hasFilters ? t.empty : watchMode ? t.noWatches : listScope === 'upcoming' ? t.noUpcoming : listScope === 'overdue' ? t.noOverdue : t.noUndated}</h2>{!hasFilters && listScope === 'upcoming' && <p>{t.noUpcomingHelp}</p>}{hasFilters ? <><p>{t.filteredEmpty}</p><Button size="sm" variant="secondary" onClick={clearFilters}>{t.clearFilters}</Button></> : embedded || watchMode ? null : listScope === 'overdue' ? <Button size="sm" variant="secondary" onClick={() => selectListScope('upcoming')}>{t.viewUpcoming}</Button> : <Button size="sm" onClick={() => setEditing('new')}><Plus size={14}/>{t.new}</Button>}</div>}
        </>}
      </div>
      {embedded && <div className="plans-panel-detail" ref={detailRef} inert={!selected} aria-hidden={!selected}>{selectedDetail}</div>}
    </div>
    <ModalPresence>{dayAgenda&&<Dialog open onOpenChange={open=>{if(!open)setDayAgenda('');}} title={`${dayAgenda} · ${t.selectedDay}`} size="lg" className="plans-day-agenda"><div className="plan-list plans-list">{selectedDayEvents.map(event=>row(event.Plan,event.At))}{!selectedDayEvents.length&&<p className="plans-day-empty">{t.dayEmpty}</p>}</div><Button size="sm" onClick={()=>setEditing('new')}><Plus size={14}/>{t.new}</Button></Dialog>}</ModalPresence>
    <ModalPresence>{!embedded&&selected&&<Drawer open onOpenChange={open=>{if(!open)closeDetails();}} title={t.planDetails} closeLabel={t.closeDetails} className="plans-details-drawer">{selectedDetail}</Drawer>}</ModalPresence>
    <ModalPresence>{editing && (editing!=='new' && editing.kind==='watch' ? <WatchEditor watch={editing.value} onClose={()=>setEditing(null)}/> : <PlanEditor entry={editing === 'new' ? undefined : editing} assistantID={assistantID} assistants={assistants} date={date} onClose={() => setEditing(null)} />)}</ModalPresence>
    <ModalPresence>{configuring && related.data && <HandoffEditor plan={configuring} all={related.data ?? []} onClose={() => setConfiguring(null)} />}</ModalPresence>
    <ModalPresence>{completing && <CompletionEditor plan={completing.plan} occurrence={completing.at} onComplete={run=>choose(completing.plan,run.OccurrenceAt||undefined,run.ID)} onClose={() => setCompleting(null)} />}</ModalPresence>
    <ModalPresence>{deleting && <Confirm title={t.remove} description={t.deleteHelp} danger onClose={() => setDeleting(null)} onConfirm={async () => { await deletePlan(deleting.Watch ? '/api/watches/delete' : deleting.Todo ? '/api/todos/delete' : '/api/schedules/delete', { ID: deleting.Ref.ID }, (deleting.Watch || deleting.Todo || deleting.Schedule)!.Version); closeDetails(); }} />}</ModalPresence>
  </section>;
  return embedded ? <Drawer className="assistant-plans" docked={embedded.docked} open onOpenChange={open => { if (!open) embedded.onClose(); }} side={mobile ? 'bottom' : 'right'} title={selected ? t.planDetails : t.title}
    headerContent={selected ? <div className="plans-panel-heading"><IconButton ref={backRef} label={t.backToList} onClick={closeDetails}><ChevronLeft size={18} /></IconButton><span>{t.planDetails}</span></div> : undefined}>
    {workspace}
  </Drawer> : workspace;
}
function HandoffEditor({plan,all,onClose}:{plan:Plan;all:Plan[];onClose:()=>void}){
 const {locale}=useCopy(),t=planMessages[locale];const [dependencies,setDependencies]=useState((plan.Links.Dependencies??[]).map(refKey)),[handoff,setHandoff]=useState(plan.Links.Handoff);
 const saveHandoff = useMutation([plan.Ref.Kind, plan.Ref.ID, dependencies, handoff, plan.State]);
  return <EditDialog title={t.handoff} onClose={onClose} onSubmit={()=>saveHandoff('/api/plans/configure',{Ref:plan.Ref,Dependencies:dependencies.map(key=>all.find(p=>refKey(p.Ref)===key)!.Ref),Handoff:dependencies.length&&plan.AssistantID?handoff:'notify',State:plan.State},plan.Links.Version)}><Select multiple label={t.dependencies} value={dependencies} onValueChange={setDependencies} options={all.filter(p=>refKey(p.Ref)!==refKey(plan.Ref)&&!p.Watch&&!p.Schedule?.Trigger.Recurrence).map(p=>({value:refKey(p.Ref),label:p.Title}))}/>{plan.AssistantID&&plan.Todo&&<Select label={t.handoff} value={handoff} onValueChange={setHandoff} options={[{value:'notify',label:t.notify},{value:'automatic',label:t.autoHandoff}]}/>}<p className="muted">{t.handoffHelp}</p></EditDialog>;
}
function CompletionEditor({plan,occurrence,onClose,onComplete}:{plan:Plan;occurrence?:string;onClose:()=>void;onComplete:(run:PlanExecution)=>void}){const {locale}=useCopy(),t=planMessages[locale],[result,setResult]=useState('');const completePlan = useMutation([plan.Ref.Kind, plan.Ref.ID, occurrence, result]);
  return <EditDialog title={t.completeExecution} submitLabel={t.completeExecution} onClose={onClose} onSubmit={async()=>{const run=await completePlan<PlanExecution>('/api/plans/execute',{Ref:plan.Ref,Action:'complete',OccurrenceAt:occurrence||null,Result:result},plan.Links.Version);onComplete(run);}}><Textarea label={t.result} value={result} onChange={e=>setResult(e.target.value)} maxLength={8192}/></EditDialog>;}
function PlanEditor({ entry, assistants, assistantID, date, onClose }: { entry?: Exclude<Entry,{kind:'watch'}>; assistants: Assistant[]; assistantID?: string; date: string; onClose: () => void }) {
  const { locale } = useCopy(), t = planMessages[locale];
  const todo = entry?.kind === 'item' ? entry.value : undefined, schedule = entry && entry.kind !== 'item' ? entry.value : undefined;
  const [kind, setKind] = useState<Exclude<Entry['kind'],'watch'>>(entry?.kind ?? 'item'), [title, setTitle] = useState(entry?.value.Title ?? ''), [notes, setNotes] = useState(todo?.Notes ?? schedule?.Instructions ?? ''), [owner, setOwner] = useState(entry?.value.AssistantID ?? assistantID ?? '');
  const [due, setDue] = useState(localDateTime(todo?.DueAt)), [block, setBlock] = useState(!!todo?.StartAt);
  const [timezone, setTimezone] = useState(schedule?.Trigger.Timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone);
  const [start, setStart] = useState(schedule ? zonedInput(schedule.Trigger.StartAt, schedule.Trigger.Timezone) : todo?.StartAt ? localDateTime(todo.StartAt) : `${date}T09:00`), [end, setEnd] = useState(schedule?.EndAt ? zonedInput(schedule.EndAt, schedule.Trigger.Timezone) : todo?.EndAt ? localDateTime(todo.EndAt) : schedule ? '' : `${date}T10:00`);
  const [repeat, setRepeat] = useState(schedule?.Trigger.Recurrence ?? '');
  const options = [{ value: '', label: t.once }, { value: 'FREQ=DAILY', label: t.daily }, { value: 'FREQ=WEEKLY', label: t.weekly }]; if (repeat && !options.some(o => o.value === repeat)) options.push({ value: repeat, label: repeat });
  const inputZone=kind==='item'?Intl.DateTimeFormat().resolvedOptions().timeZone:timezone;
  let validZone=true;try{new Intl.DateTimeFormat('en',{timeZone:inputZone});}catch{validZone=false;}
  const instant=(value:string)=>{try{return value?zonedInstant(value,inputZone):null;}catch{return null;}};
  const startValue=instant(start),endValue=instant(end),dueValue=due?new Date(due).toISOString():null;
  const savePlan = useMutation([todo?.ID, todo?.Completed, schedule?.ID, schedule?.State, schedule?.Trigger.MissedPolicy, schedule?.Trigger.MaxCatchUp, kind, title, notes, owner, due, block, start, end, timezone, repeat]);
  return <EditDialog title={entry ? t.edit : t.new} onClose={onClose} onSubmit={async () => {
    if (kind === 'item') {
      if (block && (!start || !end || new Date(end) <= new Date(start))) throw new Error(t.invalidTime);
      await savePlan(todo ? '/api/todos/update' : '/api/todos', { ...(todo ? { ID: todo.ID, Completed: todo.Completed } : {}), Title: title.trim(), Notes: notes, AssistantID: owner, DueAt: due ? new Date(due).toISOString() : null, StartAt: block ? new Date(start).toISOString() : null, EndAt: block ? new Date(end).toISOString() : null }, todo?.Version);
    } else {
      if (kind === 'automatic' && !owner) throw new APIError('invalid_argument');
      const startAt = zonedInstant(start, timezone), endAt = kind === 'event' && end ? zonedInstant(end, timezone) : null;
      if (endAt && endAt <= startAt) throw new Error(t.invalidTime);
      await savePlan(schedule ? '/api/schedules/update' : '/api/schedules', { ...(schedule ? { ID: schedule.ID, State: schedule.State } : {}), Title: title.trim(), Kind: kind === 'event' ? 'personal_schedule' : 'assistant_schedule', AssistantID: kind === 'event' ? '' : owner, Instructions: notes, EndAt: endAt, Trigger: { StartAt: startAt, Timezone: timezone, Recurrence: repeat, MissedPolicy: schedule?.Trigger.MissedPolicy ?? 'coalesce', MaxCatchUp: schedule?.Trigger.MaxCatchUp ?? 0 } }, schedule?.Version);
    }
  }}>
    {kind==='automatic' && <DreamConflictNotice assistantID={owner} startAt={startValue || undefined} timezone={timezone} recurrence={repeat} />}
    <TextField label={t.name} value={title} onChange={e => setTitle(e.target.value)} required maxLength={512} />
    {!entry && <Select label={t.type} value={kind} onValueChange={value => { setKind(value); if (value === 'automatic' && !owner) setOwner(assistants.find(a => a.State === 'active')?.ID ?? ''); }} options={(['item', 'event', 'automatic'] as const).map(value => ({ value, label: t[value], disabled: value === 'automatic' && !assistants.some(a => a.State === 'active') }))} />}
    {kind === 'item' && <><DateTimePicker label={t.due} value={dueValue} presets={[]} onValueChange={value=>setDue(localDateTime(value))}/>{due&&<Button size="sm" variant="ghost" onClick={()=>setDue('')}>{locale==='zh'?'清除日期':'Clear date'}</Button>}<Checkbox label={t.block} checked={block} onChange={e => setBlock(e.target.checked)} /></>}
    {(kind !== 'item' || block) && <><DateTimePicker label={t.start} value={startValue} timeZone={validZone?inputZone:undefined} disabled={!validZone} presets={[]} error={start&&!startValue?t.invalidTime:undefined} onValueChange={value=>setStart(value?zonedInput(value,inputZone):'')} />{kind !== 'automatic' && <DateTimePicker label={t.end} value={endValue} timeZone={validZone?inputZone:undefined} disabled={!validZone} presets={[]} error={end&&!endValue?t.invalidTime:undefined} onValueChange={value=>setEnd(value?zonedInput(value,inputZone):'')} />}</>}
    {kind !== 'item' && <><TextField label={t.timezone} error={!validZone?t.invalidTime:undefined} value={timezone} onChange={e => setTimezone(e.target.value)} required /><Select label={t.repeat} value={repeat} onValueChange={setRepeat} options={options} /></>}
    {schedule?.Trigger.Recurrence && <p className="muted">{t.seriesEdit}</p>}
    <details open={kind !== 'item' || !!entry}><summary>{t.details}</summary>{kind !== 'event' && <Select label={t.owner} value={owner} onValueChange={setOwner} options={[...(kind === 'item' ? [{ value: '', label: t.me }] : []), ...assistants.filter(a => a.State === 'active' || a.ID === owner).map(a => ({ value: a.ID, label: a.Name }))]} />}<Textarea label={kind === 'automatic' ? t.instructions : t.notes} value={notes} onChange={e => setNotes(e.target.value)} required={kind === 'automatic'} maxLength={8192} /><p className="muted">{kind === 'automatic' ? t.automaticHelp : kind === 'item' ? t.assignment : t.event}</p></details>
  </EditDialog>;
}

function watchRequest(w:Watch){return {ID:w.ID,AssistantID:w.AssistantID,Title:w.Title,Instructions:w.Instructions,Watcher:w.Watcher,StartAt:w.StartAt,EndAt:w.EndAt,State:w.State};}
function WatchEditor({watch:w,onClose}:{watch:Watch;onClose:()=>void}){
 const {locale}=useCopy(),t=planMessages[locale];
 const [title,setTitle]=useState(w.Title),[instructions,setInstructions]=useState(w.Instructions),[end,setEnd]=useState(w.EndAt);
 const save=useMutation([w.ID,w.Version,title,instructions,end]);
 return <EditDialog title={t.edit} onClose={onClose} onSubmit={()=>save('/api/watches',{...watchRequest(w),Title:title,Instructions:instructions,EndAt:end},w.Version)}>
  <TextField label={t.name} value={title} onChange={e=>setTitle(e.target.value)} required maxLength={4096}/>
  <Textarea label={t.watchInstructions} value={instructions} onChange={e=>setInstructions(e.target.value)} required maxLength={65536}/>
  <DateTimePicker label={t.watchEnd} value={end} onValueChange={setEnd} presets={[]}/>
 </EditDialog>;
}
function WatchDetails({watch:w}:{watch:Watch}){
 const {locale}=useCopy(),t=planMessages[locale],action=useAction();const [logs,setLogs]=useState('');
 useErrorToast(action.error);
 const reasons:Record<string,string>=locale==='zh'?{authorization_unavailable:'授权失效，需要重新授权',execution_limit:'已达到授权运行时限',watch_protocol_invalid:'Watcher 输出不符合协议',watch_queue_full:'待处理事件已达到上限',watch_health_timeout:'未按时收到有效检查进度',watch_start_timeout:'Watcher 未按时就绪',watch_platform_unsupported:'当前计算环境不支持 watcher',watch_exited:'Watcher 意外退出'}:{authorization_unavailable:'Authorization unavailable',execution_limit:'Authorized execution deadline reached',watch_protocol_invalid:'Invalid watcher protocol',watch_queue_full:'Pending event capacity reached',watch_health_timeout:'Health deadline exceeded',watch_start_timeout:'Watcher did not become ready',watch_platform_unsupported:'Watcher unsupported by this compute environment',watch_exited:'Watcher exited unexpectedly'};
 return <section className="plans-watch-protocol">
  <dl className="plans-properties">
   {w.LastHealthyAt&&<div><dt>{t.lastHealthy}</dt><dd>{new Date(w.LastHealthyAt).toLocaleString(locale)}</dd></div>}
   {w.EndAt&&<div><dt>{t.watchEnd}</dt><dd>{new Date(w.EndAt).toLocaleString(locale)}</dd></div>}
   {w.Reason&&<div><dt>{t.status}</dt><dd>{reasons[w.Reason]??w.Reason}</dd></div>}
  </dl>
  {!w.Watcher.health_seconds&&<p className="muted">{t.watchUnknownHealth}</p>}
  {w.LastInterruptionAt&&<p className="muted">{t.watchInterrupted}</p>}
  {!!w.Recent?.length&&<details><summary>{t.watchEvents} ({w.Recent.length})</summary>{[...w.Recent].reverse().map(v=><div key={v.Key}><time>{new Date(v.At).toLocaleString(locale)}</time><pre>{JSON.stringify(v.Data,null,2)}</pre></div>)}</details>}
  <details><summary>{t.watchProgram}</summary><p>{t.watchCompute}: {w.Watcher.compute_id}</p><pre>{w.Watcher.script}</pre>
   {(w.RunID||w.LastRunID)&&<Button size="sm" variant="secondary" loading={action.busy} onClick={()=>void action.run(async()=>{const out=await api.read<{Output:string}>(`/api/assistants/${w.AssistantID}/shell-tasks`,{action:'read',task_id:w.RunID||w.LastRunID,stream:'stderr',cursor:0});setLogs(out.Output);})}>{locale==='zh'?'读取日志':'Read logs'}</Button>}
   {logs&&<pre>{logs}</pre>}
  </details>
 </section>;
}
