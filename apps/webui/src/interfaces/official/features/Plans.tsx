import { useErrorToast } from '../../../error-toasts';
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { ModalPresence, Button, Checkbox, DateTimePicker, Dialog, Drawer, DropdownMenu, IconButton, Select, Textarea, TextField, planDate, useBreakpoint, useChoiceScroll } from '@molr/ui';
import { ArrowRight, Bot, CalendarDays, Check, ChevronDown, ChevronLeft, Ellipsis, GitBranch, History, List, Pause, Pencil, Play, Plus, Repeat2, SlidersHorizontal, Trash2, UserRound, X } from 'lucide-react';
import { api, APIError } from '@molr/sdk';
import type { Assistant, Schedule, Todo, Plan, PlanRef, PlanEvent } from '@molr/sdk';
import { useCopy, useQuery } from '../../../state';
import { Confirm, EditDialog, Loading, PageHeader, localDateTime, zonedInput, zonedInstant, useAction } from '../components/common';
import { planMessages } from '../../../plan-messages';
import { PlanHistory } from './PlanHistory';
import { DreamConflictNotice } from './DreamConflictNotice';
import { PlanCalendarToolbar } from './PlanCalendarToolbar';
import { PlanWeekCalendar, weekDays } from './PlanWeekCalendar';
import { PlanMonthCalendar, monthDays } from './PlanMonthCalendar';
import { calendarEvents, eventDone, eventKey, eventsOnDay, usePlanEvents } from './plan-events';
import './plans.css';

type Entry = { kind: 'item'; value: Todo } | { kind: 'event' | 'automatic'; value: Schedule };
const listScopes = ['upcoming', 'overdue', 'undated'] as const;
const refKey = (ref: PlanRef) => `${ref.Kind}-${ref.ID}`;
const entry = (plan: Plan): Entry => plan.Todo ? { kind: 'item', value: plan.Todo } : { kind: plan.Schedule!.Kind === 'personal' ? 'event' : 'automatic', value: plan.Schedule! };
const instantDay = (value: string) => planDate(new Date(value));
function occurrenceEnd(plan: Plan, at: string) { const s = plan.Schedule; return s?.EndAt ? new Date(new Date(at).getTime() + new Date(s.EndAt).getTime() - new Date(s.Trigger.StartAt).getTime()).toISOString() : null; }


export function Plans({ assistants: availableAssistants, assistantID, selectedID, selectedTaskID, embedded }: { assistants: Assistant[]; assistantID?: string; selectedID?: string; selectedTaskID?: string; embedded?: { docked?: boolean; onClose: () => void } }) {
  const { locale, t: copy } = useCopy(), t = planMessages[locale], action = useAction(), mobile = useBreakpoint('mobile');
  const appliedSelection = useRef('');
  const scopeID = useId(), listRef = useRef<HTMLDivElement>(null);
  const [view, setView] = useState(embedded ? 'list' : 'calendar'), [date, setDate] = useState(planDate()), [month, setMonth] = useState(planDate()), [owner, setOwner] = useState(assistantID || 'all'), [kind, setKind] = useState('all'), [status, setStatus] = useState('all');
  const [calendarMode, setCalendarMode] = useState<'week' | 'month'>('week');
  const [listScope, setListScope] = useState<(typeof listScopes)[number]>('upcoming'), [selectedAt, setSelectedAt] = useState<string>(), [dayAgenda, setDayAgenda] = useState('');
  const viewRef = useChoiceScroll(view);
  const [listFrom] = useState(() => new Date().toISOString());
  const backRef = useRef<HTMLButtonElement>(null), detailRef = useRef<HTMLDivElement>(null);
  const listScroll = useRef(0);
  const listOwner = embedded ? assistantID || '' : owner;
  const [expanded, setExpanded] = useState(''), [editing, setEditing] = useState<Entry | 'new' | null>(null), [deleting, setDeleting] = useState<Plan | null>(null), [configuring, setConfiguring] = useState<Plan | null>(null), [completing, setCompleting] = useState<{ plan: Plan; at?: string } | null>(null);
  const visibleDays=view === 'calendar' && calendarMode === 'week' ? weekDays(month) : monthDays(month);
  const from = new Date(visibleDays[0]);from.setHours(0,0,0,0);
  const until = new Date(visibleDays[visibleDays.length-1]);until.setDate(until.getDate()+1);until.setHours(0,0,0,0);
  const upcoming=usePlanEvents((embedded || view==='list' && listScope==='upcoming') && (!embedded || assistantID) ? `/api/plans/events?from=${encodeURIComponent(listFrom)}&limit=${embedded ? 10 : 50}&scope=${encodeURIComponent(listOwner)}&kind=${embedded ? 'all' : kind}&state=${embedded ? 'all' : status}` : null);
  const plans = useQuery<Plan[]>(!embedded && view === 'calendar' ? `/api/plans?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(until.toISOString())}` : embedded || listScope === 'undated' ? `/api/plans?mode=undated&scope=${encodeURIComponent(listOwner)}` : listScope === 'overdue' ? '/api/plans' : null);
  const selectedQuery = useQuery<Plan>(expanded ? `/api/plans/${expanded.replace('-', '/')}${selectedAt ? `?at=${encodeURIComponent(selectedAt)}` : ''}` : null);
  const related = useQuery<Plan[]>(expanded || configuring ? '/api/plans' : null);
  const selection = useQuery<{Plan:Plan;At:string|null}>(selectedID || selectedTaskID ? `/api/plans/selection?plan_id=${encodeURIComponent(selectedID || '')}&task_id=${encodeURIComponent(selectedTaskID || '')}` : null);
  const currentAssistant = useQuery<Assistant>(assistantID ? `/api/assistants/${assistantID}` : null);
  const assistants = currentAssistant.data && !availableAssistants.some(a => a.ID === assistantID) ? [...availableAssistants, currentAssistant.data] : availableAssistants;
  const all = [...new Map([...(plans.data ?? []), ...upcoming.items.map(event=>event.Plan)].map(plan=>[refKey(plan.Ref),plan])).values()], latest = (p: Plan) => p.Executions[0];
  const completed = (p: Plan) => latest(p)?.State === 'completed' || !p.Executions.length && !!p.Todo?.Completed;
  const filtered = all.filter(p => (listOwner === 'all' || (p.AssistantID || 'me') === listOwner) && (embedded || kind === 'all' || entry(p).kind === kind) );
  const when = (p: Plan) => p.Todo?.StartAt || p.Todo?.DueAt || p.Schedule?.NextAt || p.Occurrences.find(at => instantDay(at) >= planDate()) || p.Schedule?.Trigger.StartAt || '';
  const matchesState=(p:Plan,at?:string)=>status==='all'||(status==='done'?eventDone(p,at):!eventDone(p,at));
  const scheduledEvents=calendarEvents(filtered).filter(e=>matchesState(e.Plan,e.At));
  const undated=filtered.filter(p=>p.Todo&&!when(p)&&matchesState(p));
  const overdue=filtered.filter(p=>!p.Schedule?.Trigger.Recurrence && !!when(p) && Date.parse(p.Todo?.EndAt||when(p))<Date.parse(listFrom) && !completed(p) && matchesState(p)).sort((a,b)=>Date.parse(when(a))-Date.parse(when(b)));
  const selectedDayEvents=eventsOnDay(scheduledEvents,dayAgenda||date);
  const state = (p: Plan) => p.Links.Paused ? t.paused : p.BlockedBy.length ? t.blocked : !p.Schedule?.Trigger.Recurrence && completed(p) ? t.ended : p.Links.Dependencies?.length && !p.Executions.length && p.Ready ? t.ready : when(p) ? t.planned : t.unscheduled;
  const tone = (p: Plan) => p.Links.Paused ? 'paused' : p.BlockedBy.length ? 'blocked' : completed(p) ? 'done' : p.Links.Dependencies?.length && !p.Executions.length && p.Ready ? 'ready' : 'open';
  const executionState = (p: Plan) => latest(p) && ['pending', 'running', 'waiting', 'paused'].includes(latest(p).State) ? (copy.tasks[latest(p).State as 'pending'] ?? latest(p).State) : state(p);
  const ownerName = (p: Plan) => assistants.find(a => a.ID === p.AssistantID)?.Name ?? (p.AssistantID ? t.assistant : t.me);
  const selected = selectedQuery.data ?? all.find(p => refKey(p.Ref) === expanded) ?? (selection.data && refKey(selection.data.Plan.Ref) === expanded ? selection.data.Plan : undefined);
  useLayoutEffect(() => {
    if (!embedded) return;
    if (!selected) { listRef.current?.scrollTo({ top: listScroll.current, behavior: 'instant' }); return; }
    detailRef.current?.scrollTo({ top: 0, behavior: 'instant' });
    backRef.current?.focus({ preventScroll: true });
  }, [embedded && selected?.Ref.ID, selectedAt]);
  const hasFilters = !embedded && (owner !== (assistantID || 'all') || kind !== 'all' || status !== 'all');
  useEffect(() => {
    const request = `${selectedID || ''}/${selectedTaskID || ''}`;
    if (!selectedID && !selectedTaskID) { appliedSelection.current = ''; return; }
    if (appliedSelection.current === request) return;
    const match = selection.data?.Plan;
    if (match) {
      appliedSelection.current = request;
      const at=selection.data?.At || when(match);
      setExpanded(refKey(match.Ref));setSelectedAt(at||undefined);
      if(embedded){setView('list');}else if(at){setView('calendar');setMonth(instantDay(at));setDate(instantDay(at));}else{setListScope('undated');setView('list');}
    }
  }, [selection.data, selectedID, selectedTaskID]);
  useEffect(() => {
    if (selectedID || selectedTaskID) listRef.current?.querySelector('[data-selected="true"]')?.scrollIntoView({ block: 'nearest' });
  }, [expanded, selectedID, selectedTaskID]);
  const format = (at: string) => new Date(at).toLocaleString(locale, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  const shortDate = (at: string) => new Date(at).toLocaleDateString(locale, { month: 'short', day: 'numeric' });
  const time = (at: string) => new Date(at).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' });
  const occurrence = (p: Plan) => p.Schedule?.Trigger.Recurrence ? selectedAt : undefined;
  const repeat = (rule: string) => !rule ? t.once : rule === 'FREQ=DAILY' ? t.daily : rule === 'FREQ=WEEKLY' ? t.weekly : t.customRepeat;
  function pause(p: Plan) { void action.run(() => api.mutate('/api/plans/configure', { Ref: p.Ref, Dependencies: p.Links.Dependencies ?? [], Handoff: p.Links.Handoff, Paused: !p.Links.Paused }, p.Links.Version)); }
  function start(p: Plan) { void action.run(() => api.mutate('/api/plans/execute', { Ref: p.Ref, Action: 'start', OccurrenceAt: occurrence(p) || null }, p.Links.Version), () => setExpanded(refKey(p.Ref))); }
  function selectCalendarDate(value: string) { setMonth(value); setDate(value); setExpanded(''); setSelectedAt(undefined); setDayAgenda(''); }
  function selectDate(value: string) { setDate(value); setExpanded(''); setSelectedAt(undefined); setDayAgenda(value); }
  function selectEvent(event: PlanEvent) { setExpanded(refKey(event.Plan.Ref)); setSelectedAt(event.At); setDayAgenda(''); }
  function selectPlan(p: Plan, at?: string) {
    if (expanded === refKey(p.Ref) && selectedAt === at) { closeDetails(); return; }
    if (embedded) listScroll.current = listRef.current?.scrollTop ?? 0;
    setExpanded(refKey(p.Ref)); setSelectedAt(at); setDayAgenda('');
  }
  useErrorToast(plans.error || upcoming.error || selectedQuery.error || related.error || selection.error || action.error || currentAssistant.error, ()=>{plans.reload();upcoming.reload();selectedQuery.reload();related.reload();selection.reload();});
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
    const key = `${expanded}@${selectedAt || ''}`;
    setExpanded(''); setSelectedAt(undefined);
    if (embedded) requestAnimationFrame(() => {
      document.getElementById(`${scopeID}-title-${key}`)?.focus({ preventScroll: true });
      listRef.current?.scrollTo({ top: listScroll.current, behavior: 'instant' });
    });
  }
  function openRelated(ref: PlanRef) {
    const linked=(related.data ?? all).find(p=>refKey(p.Ref)===refKey(ref)),at=linked?when(linked):'';
    if (embedded && linked?.AssistantID !== assistantID) { window.location.hash = `/plans/${ref.ID}`; return; }
    setOwner('all');setKind('all');setStatus('all');setExpanded(refKey(ref));setSelectedAt(at||undefined);
    if(embedded){setView('list');}else if(at){setView('calendar');setMonth(instantDay(at));setDate(instantDay(at));}else{setListScope('undated');setView('list');}
  }
  function menu(p: Plan) {
    const canDelete = !p.ExecutionCount && !p.HasSuccessors;
    return <DropdownMenu label={t.actions} align="end" trigger={<IconButton className="plans-more" label={t.actions}><Ellipsis size={18} /></IconButton>} items={[
      { id: 'edit', label: t.edit, icon: <Pencil size={15} />, onSelect: () => { void action.run(async()=>{const full=await api.get<Plan>(`/api/plans/${p.Ref.Kind}/${p.Ref.ID}`);setEditing(entry(full));}); } },
      { id: 'handoff', label: t.handoff, icon: <GitBranch size={15} />, onSelect: () => setConfiguring(p) },
      { id: 'pause', label: p.Links.Paused ? t.resumePlan : t.pausePlan, icon: p.Links.Paused ? <Play size={15} /> : <Pause size={15} />, disabled: action.busy, onSelect: () => pause(p) },
      { id: 'delete', label: t.remove, icon: <Trash2 size={15} />, danger: true, separatorBefore: true, disabled: !canDelete, onSelect: () => setDeleting(p) },
    ]} />;
  }
  function detail(p: Plan) {
    const key = refKey(p.Ref), runs = p.Executions, at = occurrence(p), run = at ? runs.find(r => !!r.OccurrenceAt && Date.parse(r.OccurrenceAt) === Date.parse(at)) : latest(p);
    const active = run && ['pending', 'running', 'waiting', 'paused'].includes(run.State), done = run?.State === 'completed' || !runs.length && !!p.Todo?.Completed;
    const successors = (related.data ?? []).filter(next => next.Links.Dependencies?.some(ref => refKey(ref) === key));
    const startAt = at || p.Todo?.StartAt || p.Schedule?.Trigger.StartAt;
    const endAt = p.Todo?.EndAt || (startAt ? occurrenceEnd(p, startAt) : null);
    return <section id={`${scopeID}-detail-${key}`} className="plan-expanded plans-detail" role="region" aria-label={`${p.Title} · ${t.planDetails}`}>
      <div className="plans-detail-body">
        <h2 className="plans-detail-title">{p.Title}</h2>
        {p.Schedule?.Kind==='assistant_plan' && <DreamConflictNotice assistantID={p.AssistantID} startAt={startAt || undefined} timezone={p.Schedule.Trigger.Timezone} recurrence={at?'':p.Schedule.Trigger.Recurrence} disabled={p.Links.Paused || !p.Schedule.Enabled} />}
        <dl className="plans-properties"><div><dt>{t.owner}</dt><dd><span className="plans-avatar" data-person={!p.AssistantID}>{p.AssistantID ? <Bot size={13} /> : <UserRound size={13} />}</span>{ownerName(p)}</dd></div><div><dt>{t.status}</dt><dd><span className="plans-status" data-tone={tone(p)}><i />{state(p)}</span></dd></div>{latest(p) && <div><dt>{t.latestExecution}</dt><dd>{copy.tasks[latest(p).State as 'pending'] ?? latest(p).State}</dd></div>}
          {startAt && <div><dt>{t.scheduled}</dt><dd>{format(startAt)}{endAt && <span className="plans-end">– {instantDay(endAt) === instantDay(startAt) ? time(endAt) : format(endAt)}</span>}</dd></div>}
          {p.Todo?.DueAt && <div><dt>{t.deadline}</dt><dd>{format(p.Todo.DueAt)}</dd></div>}
          {p.Schedule && <div><dt>{t.repeat}</dt><dd title={p.Schedule.Trigger.Recurrence}>{repeat(p.Schedule.Trigger.Recurrence)}<span className="plans-subtle">{p.Schedule.Trigger.Timezone}</span></dd></div>}
        </dl>
        {(p.Todo?.Notes || p.Schedule?.Instructions) && <p className="plans-description">{p.Todo?.Notes || p.Schedule?.Instructions}</p>}
        <div className="plans-primary-actions"><Button size="sm" disabled={action.busy || !p.Ready || !!active || (!p.AssistantID && !!done) || !!p.Schedule?.Trigger.Recurrence && !at} onClick={() => start(p)}><Play size={14} />{t.startExecution}</Button>{!p.AssistantID && <Button size="sm" variant="secondary" disabled={action.busy || (!p.Ready && !active) || !!done || !!p.Schedule?.Trigger.Recurrence && !at} onClick={() => setCompleting({ plan: p, at })}><Check size={14} />{t.completeExecution}</Button>}</div>
        {!!p.Links.Dependencies?.length && <section className="plans-related"><h3><GitBranch size={14} />{t.dependencies}</h3>{p.Links.Dependencies.map(ref => { const linked = (related.data ?? []).find(d => refKey(d.Ref) === refKey(ref)); return <button key={refKey(ref)} onClick={() => openRelated(ref)}><span className="plans-related-dot" data-done={linked && completed(linked)} /><span>{linked?.Title ?? ref.ID}</span><ArrowRight size={14} /></button>; })}</section>}
        {successors.length > 0 && <section className="plans-related"><h3><ArrowRight size={14} />{t.successors}</h3>{successors.map(next => <button key={refKey(next.Ref)} onClick={() => openRelated(next.Ref)}><span>{next.Title}</span><small>{state(next)}</small><ArrowRight size={14} /></button>)}</section>}
        <PlanHistory plan={p} selectedTaskID={selection.data?.Plan.Ref.ID === p.Ref.ID ? selectedTaskID : undefined} onComplete={run=>setCompleting({plan:p,at:run.OccurrenceAt || undefined})}/>

        {(p.ExecutionCount > 0 || successors.length > 0) && <p className="plans-history-note">{t.preserveHistory}</p>}
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
        <Button size="sm" variant="ghost" className="plans-row-history" aria-label={`${t.executions} (${p.ExecutionCount})`} aria-expanded={isSelected} onClick={() => selectPlan(p,eventAt)}><History size={14} /><span>{p.ExecutionCount}</span></Button>
        {menu(p)}
      </div>
    </article>;
  }
  const listRows=embedded ? [...upcoming.items, ...undated.map(p=>({Key:eventKey(p),At:'',Plan:p}))].filter(event => event.Plan.AssistantID === assistantID).slice(0,10) : listScope==='upcoming'?upcoming.items:listScope==='overdue'?overdue.map(p=>({Key:eventKey(p,when(p)),At:when(p),Plan:p})):undated.map(p=>({Key:eventKey(p),At:'',Plan:p}));
  if (embedded && selected && !listRows.some(event => eventKey(event.Plan, event.At) === eventKey(selected, selectedAt || when(selected)))) {
    const at = selectedAt || when(selected);
    listRows.unshift({ Key: eventKey(selected, at), At: at, Plan: selected });
    listRows.splice(10);
  }
  const filters = <div className="plans-filters"><SlidersHorizontal size={14} className="plans-filter-icon" aria-hidden="true" /><Select hideLabel size="sm" label={t.owner} value={owner} onValueChange={setOwner} options={[{ value: 'all', label: t.allOwners }, { value: 'me', label: t.me }, ...assistants.map(a => ({ value: a.ID, label: a.Name }))]} /><Select hideLabel size="sm" label={t.type} value={kind} onValueChange={setKind} options={[{ value: 'all', label: t.allTypes }, ...(['item', 'event', 'automatic'] as const).map(value => ({ value, label: t[value] }))]} /><Select hideLabel size="sm" label={t.status} value={status} onValueChange={setStatus} options={[{ value: 'all', label: t.allStatuses }, { value: 'open', label: t.open }, { value: 'done', label: t.done }]} />{hasFilters && <IconButton label={t.clearFilters} onClick={clearFilters}><X size={14} /></IconButton>}</div>;
  const selectedDetail = selected ? selectedQuery.data ? detail(selectedQuery.data) : <Loading /> : null;
  const workspace = <section className={`plans-workspace${embedded ? ' plans-workspace--embedded' : ''}`} data-view={view} data-detail={embedded && !!selected}>
    {!embedded && <PageHeader className="plans-header" title={t.title} icon={<CalendarDays />} actions={<Button size="sm" onClick={() => setEditing('new')}><Plus size={15} />{t.new}</Button>}><span className="plans-total">{filtered.length}</span></PageHeader>}
    {!embedded && <div className="plans-commandbar">
      {mobile && filters}
      <div ref={viewRef} className="ui-choice-list ui-choice-list--line plans-view" role="group" aria-label={t.view}>
        <button type="button" aria-label={t.list} aria-describedby={`${scopeID}-list-scope`} aria-pressed={view === 'list'} onClick={() => selectView('list')}>
          <List size={14} />{t.list}<span id={`${scopeID}-list-scope`} className="plans-current-scope">· {t[listScope]}</span>
        </button>
        <DropdownMenu label={t.eventRange} trigger={<button type="button" className="plans-list-menu" data-active={view === 'list'} aria-label={t.eventRange}><ChevronDown size={14} /></button>}
          items={listScopes.map(value => {
            const count = value === 'overdue' ? overdue.length : value === 'undated' ? undated.length : 0;
            return { id: value, label: `${t[value]}${count > 0 ? ` (${count})` : ''}`, selected: listScope === value, onSelect: () => selectListScope(value) };
          })} />
        <button type="button" className="plans-calendar-tab" aria-pressed={view === 'calendar'} onClick={() => selectView('calendar')}><CalendarDays size={14} />{t.calendar}</button>
      </div>
      {!mobile && filters}
    </div>}
    <div className="plans-content">
      <div className="plans-master" ref={listRef} inert={!!embedded && !!selected} aria-hidden={embedded && selected ? true : undefined}>
        {!embedded && view === 'calendar' ? <section className="plans-calendar-section" data-mode={calendarMode}>
          <PlanCalendarToolbar date={month} mode={calendarMode} locale={locale} onDate={selectCalendarDate} onMode={mode=>{setCalendarMode(mode);setExpanded('');setSelectedAt(undefined);setDayAgenda('');}}/>
          {plans.loading && !plans.data ? <Loading /> : calendarMode === 'week' ? <PlanWeekCalendar date={month} selectedDate={date} locale={locale} events={scheduledEvents} selectedKey={selected ? eventKey(selected,selectedAt) : undefined} onDay={selectDate} onEvent={selectEvent}/> : <PlanMonthCalendar month={month} date={date} locale={locale} events={scheduledEvents} onDay={selectDate} onEvent={selectEvent} moreLabel={(count,day)=>t.moreEvents(count,day)}/>}

        </section> : <>
          {!listRows.length && (plans.loading && !plans.data || listScope === 'upcoming' && upcoming.loading) ? <Loading /> : <div className="plan-list plans-list" aria-label={t.eventList}>{listRows.map(event=>row(event.Plan,event.At||undefined))}</div>}
          {!embedded&&listScope==='upcoming'&&upcoming.more&&<Button className="plans-load-more" variant="secondary" loading={upcoming.loading} onClick={upcoming.loadMore}>{t.loadMoreEvents}</Button>}
          {!listRows.length&&!plans.loading&&!(listScope==='upcoming'&&upcoming.loading)&&<div className="plans-empty"><span className="plans-empty-icon"><CalendarDays size={32}/></span><h2>{hasFilters ? t.empty : listScope === 'upcoming' ? t.noUpcoming : listScope === 'overdue' ? t.noOverdue : t.noUndated}</h2>{!hasFilters && listScope === 'upcoming' && <p>{t.noUpcomingHelp}</p>}{embedded ? null : hasFilters ? <><p>{t.filteredEmpty}</p><Button size="sm" variant="secondary" onClick={clearFilters}>{t.clearFilters}</Button></> : listScope === 'overdue' ? <Button size="sm" variant="secondary" onClick={() => selectListScope('upcoming')}>{t.viewUpcoming}</Button> : <Button size="sm" onClick={() => setEditing('new')}><Plus size={14}/>{t.new}</Button>}</div>}
        </>}
      </div>
      {embedded && <div className="plans-panel-detail" ref={detailRef} inert={!selected} aria-hidden={!selected}>{selectedDetail}</div>}
    </div>
    <ModalPresence>{dayAgenda&&<Dialog open onOpenChange={open=>{if(!open)setDayAgenda('');}} title={`${dayAgenda} · ${t.selectedDay}`} size="lg" className="plans-day-agenda"><div className="plan-list plans-list">{selectedDayEvents.map(event=>row(event.Plan,event.At))}{!selectedDayEvents.length&&<p className="plans-day-empty">{t.dayEmpty}</p>}</div><Button size="sm" onClick={()=>setEditing('new')}><Plus size={14}/>{t.new}</Button></Dialog>}</ModalPresence>
    <ModalPresence>{!embedded&&selected&&<Drawer open onOpenChange={open=>{if(!open)closeDetails();}} title={t.planDetails} closeLabel={t.closeDetails} className="plans-details-drawer">{selectedDetail}</Drawer>}</ModalPresence>
    <ModalPresence>{editing && <PlanEditor entry={editing === 'new' ? undefined : editing} assistantID={assistantID} assistants={assistants} date={date} onClose={() => setEditing(null)} />}</ModalPresence>
    <ModalPresence>{configuring && related.data && <HandoffEditor plan={configuring} all={related.data ?? []} onClose={() => setConfiguring(null)} />}</ModalPresence>
    <ModalPresence>{completing && <CompletionEditor plan={completing.plan} occurrence={completing.at} onClose={() => setCompleting(null)} />}</ModalPresence>
    <ModalPresence>{deleting && <Confirm title={t.remove} description={t.deleteHelp} danger onClose={() => setDeleting(null)} onConfirm={() => api.mutate(deleting.Todo ? '/api/todos/delete' : '/api/schedules/delete', { ID: deleting.Ref.ID }, (deleting.Todo || deleting.Schedule)!.Version)} />}</ModalPresence>
  </section>;
  return embedded ? <Drawer className="assistant-plans" docked={embedded.docked} open onOpenChange={open => { if (!open) embedded.onClose(); }} side={mobile ? 'bottom' : 'right'} title={selected ? t.planDetails : t.title}
    headerContent={selected ? <div className="plans-panel-heading"><IconButton ref={backRef} label={t.backToList} onClick={closeDetails}><ChevronLeft size={18} /></IconButton><span>{t.planDetails}</span></div> : undefined}>
    {workspace}
  </Drawer> : workspace;
}
function HandoffEditor({plan,all,onClose}:{plan:Plan;all:Plan[];onClose:()=>void}){
 const {locale}=useCopy(),t=planMessages[locale];const [dependencies,setDependencies]=useState((plan.Links.Dependencies??[]).map(refKey)),[handoff,setHandoff]=useState(plan.Links.Handoff);
 return <EditDialog title={t.handoff} onClose={onClose} onSubmit={()=>api.mutate('/api/plans/configure',{Ref:plan.Ref,Dependencies:dependencies.map(key=>all.find(p=>refKey(p.Ref)===key)!.Ref),Handoff:dependencies.length&&plan.AssistantID?handoff:'notify',Paused:plan.Links.Paused},plan.Links.Version)}><Select multiple label={t.dependencies} value={dependencies} onValueChange={setDependencies} options={all.filter(p=>refKey(p.Ref)!==refKey(plan.Ref)&&!p.Schedule?.Trigger.Recurrence).map(p=>({value:refKey(p.Ref),label:p.Title}))}/>{plan.AssistantID&&plan.Todo&&<Select label={t.handoff} value={handoff} onValueChange={setHandoff} options={[{value:'notify',label:t.notify},{value:'automatic',label:t.autoHandoff}]}/>}<p className="muted">{t.handoffHelp}</p></EditDialog>;
}
function CompletionEditor({plan,occurrence,onClose}:{plan:Plan;occurrence?:string;onClose:()=>void}){const {locale}=useCopy(),t=planMessages[locale],[result,setResult]=useState('');return <EditDialog title={t.completeExecution} submitLabel={t.completeExecution} onClose={onClose} onSubmit={()=>api.mutate('/api/plans/execute',{Ref:plan.Ref,Action:'complete',OccurrenceAt:occurrence||null,Result:result},plan.Links.Version)}><Textarea label={t.result} value={result} onChange={e=>setResult(e.target.value)} maxLength={8192}/></EditDialog>;}
function PlanEditor({ entry, assistants, assistantID, date, onClose }: { entry?: Entry; assistants: Assistant[]; assistantID?: string; date: string; onClose: () => void }) {
  const { locale } = useCopy(), t = planMessages[locale];
  const todo = entry?.kind === 'item' ? entry.value : undefined, schedule = entry && entry.kind !== 'item' ? entry.value : undefined;
  const [kind, setKind] = useState<Entry['kind']>(entry?.kind ?? 'item'), [title, setTitle] = useState(entry?.value.Title ?? ''), [notes, setNotes] = useState(todo?.Notes ?? schedule?.Instructions ?? ''), [owner, setOwner] = useState(entry?.value.AssistantID ?? assistantID ?? '');
  const [due, setDue] = useState(localDateTime(todo?.DueAt)), [block, setBlock] = useState(!!todo?.StartAt);
  const [timezone, setTimezone] = useState(schedule?.Trigger.Timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone);
  const [start, setStart] = useState(schedule ? zonedInput(schedule.Trigger.StartAt, schedule.Trigger.Timezone) : todo?.StartAt ? localDateTime(todo.StartAt) : `${date}T09:00`), [end, setEnd] = useState(schedule?.EndAt ? zonedInput(schedule.EndAt, schedule.Trigger.Timezone) : todo?.EndAt ? localDateTime(todo.EndAt) : schedule ? '' : `${date}T10:00`);
  const [repeat, setRepeat] = useState(schedule?.Trigger.Recurrence ?? '');
  const options = [{ value: '', label: t.once }, { value: 'FREQ=DAILY', label: t.daily }, { value: 'FREQ=WEEKLY', label: t.weekly }]; if (repeat && !options.some(o => o.value === repeat)) options.push({ value: repeat, label: repeat });
  const inputZone=kind==='item'?Intl.DateTimeFormat().resolvedOptions().timeZone:timezone;
  let validZone=true;try{new Intl.DateTimeFormat('en',{timeZone:inputZone});}catch{validZone=false;}
  const instant=(value:string)=>{try{return value?zonedInstant(value,inputZone):null;}catch{return null;}};
  const startValue=instant(start),endValue=instant(end),dueValue=due?new Date(due).toISOString():null;
  return <EditDialog title={entry ? t.edit : t.new} onClose={onClose} onSubmit={async () => {
    if (kind === 'item') {
      if (block && (!start || !end || new Date(end) <= new Date(start))) throw new Error(t.invalidTime);
      await api.mutate(todo ? '/api/todos/update' : '/api/todos', { ...(todo ? { ID: todo.ID, Completed: todo.Completed } : {}), Title: title.trim(), Notes: notes, AssistantID: owner, DueAt: due ? new Date(due).toISOString() : null, StartAt: block ? new Date(start).toISOString() : null, EndAt: block ? new Date(end).toISOString() : null }, todo?.Version);
    } else {
      if (kind === 'automatic' && !owner) throw new APIError('invalid_argument');
      const startAt = zonedInstant(start, timezone), endAt = kind === 'event' && end ? zonedInstant(end, timezone) : null;
      if (endAt && endAt <= startAt) throw new Error(t.invalidTime);
      await api.mutate(schedule ? '/api/schedules/update' : '/api/schedules', { ...(schedule ? { ID: schedule.ID, Enabled: schedule.Enabled } : {}), Title: title.trim(), Kind: kind === 'event' ? 'personal' : 'assistant_plan', AssistantID: kind === 'event' ? '' : owner, Instructions: notes, EndAt: endAt, Trigger: { StartAt: startAt, Timezone: timezone, Recurrence: repeat, MissedPolicy: schedule?.Trigger.MissedPolicy ?? 'coalesce', MaxCatchUp: schedule?.Trigger.MaxCatchUp ?? 0 } }, schedule?.Version);
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
