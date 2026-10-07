import { useId, useState } from 'react';
import { Button, PlanCalendar, Select, TextField, planDate } from '@molr/ui';
import { Plus } from 'lucide-react';
import type { InterfaceDemo } from './interface-demo';
import type { Locale } from './messages';
import type { DemoTodo, TodoUrgency } from './todo-demo';
import { todoUrgencies } from './todo-demo';
import type { DemoSchedule } from './schedule-demo';
import { scheduleOccursOn } from './schedule-demo';
import { productMessages } from './i18n';
import { planMessages } from './plan-messages';
import { statusLabels } from './interface-demo';
import { PageActions } from './PageActions';
type Entry = { kind: 'item'; value: DemoTodo } | { kind: 'event' | 'automatic'; value: DemoSchedule };
export function PlanPanel({ demo, locale, actionTarget, assistantId = '', onOpenExecution }: { demo: InterfaceDemo; locale: Locale; actionTarget: HTMLDivElement | null; assistantId?: string; onOpenExecution?: (id: string) => void }) {
  const t = planMessages[locale], common = productMessages[locale], id = useId();
  const [mode, setMode] = useState('list'), [date, setDate] = useState(planDate()), [kind, setKind] = useState('all'), [owner, setOwner] = useState(assistantId || 'all'), [status, setStatus] = useState('all'), [urgency, setUrgency] = useState('all');
  const [draft, setDraft] = useState<Entry | null>(null), [deleting, setDeleting] = useState<Entry | null>(null), [notice, setNotice] = useState(''), [error, setError] = useState('');
  const priority = common.todoPanel;
  const [later, setLater] = useState(false);
  const near = new Date(); near.setDate(near.getDate() + 30);
  const workPlans = demo.allWorks.filter(work => owner === 'all' || work.assistantId === owner).filter(work => status === 'all' || (work.status === 'complete') === (status === 'done'));
  const anchor = new Date(`${date}T12:00:00`);
  const ownerOptions = [{ value: 'person', label: t.me }, ...demo.assistants.map(a => ({ value: a.id, label: a.name }))];
  const entries: Entry[] = [...demo.todos.map(value => ({ kind: 'item' as const, value })), ...demo.schedules.map(value => ({ kind: value.kind === 'person' ? 'event' as const : 'automatic' as const, value }))];
  const filtered = entries.filter(row => (kind === 'all' || row.kind === kind) && (owner === 'all' || (row.value.assistantId || 'person') === owner) && (status === 'all' || row.kind === 'item' && row.value.completed === (status === 'done')) && (urgency === 'all' || row.kind === 'item' && row.value.urgency === urgency));
  const occurs = (row: Entry, day: string) => row.kind === 'item' ? row.value.due === day || !!row.value.start && row.value.start.slice(0, 10) <= day && planDate(new Date(new Date(row.value.end!).getTime() - 1)) >= day : scheduleOccursOn(row.value, day);
  const when = (row: Entry) => row.kind === 'item' ? row.value.start || row.value.due || '9999' : `${row.value.date}T${row.value.time}`;
  const rows = filtered.filter(row => mode === 'list' ? later || when(row) === '9999' || when(row).slice(0,10) <= planDate(near) : occurs(row, date) || row.kind === 'item' && !row.value.due && !row.value.start).sort((a, b) => (a.kind === 'item' ? todoUrgencies.indexOf(a.value.urgency) : 2) - (b.kind === 'item' ? todoUrgencies.indexOf(b.value.urgency) : 2) || when(a).localeCompare(when(b)));
  function start(type: Entry['kind'] = 'item', title = '') {
    setError(''); setNotice('');
    setDraft(type === 'item' ? { kind: type, value: { id: '', title, urgency: 'normal', owner: assistantId ? 'assistant' : 'person', assistantId, due: '', notes: '', completed: false } } : { kind: type, value: { id: '', title, kind: type === 'automatic' ? 'assistant' : 'person', assistantId: type === 'automatic' ? assistantId || demo.activeAssistantId : '', date, time: '09:00', endTime: type === 'event' ? '10:00' : '', repeat: 'once', detail: '', location: '', enabled: true } });
  }
  function field(patch: Partial<DemoTodo & DemoSchedule>) { setDraft(current => current ? { ...current, value: { ...current.value, ...patch } } as Entry : null); }
  function shift(offset: number) { const day = new Date(`${date}T12:00:00`); setDate(planDate(new Date(day.getFullYear(), day.getMonth() + offset, 1, 12))); }
  return <div className="plan-panel">
    <PageActions target={actionTarget}><Button size="sm" onClick={() => start('item')}><Plus size={16} />{t.new}</Button></PageActions>
    <p className="ip-lead">{assistantId ? `${demo.assistants.find(a => a.id === assistantId)?.name} · ${t.automaticHelp}` : t.hint}</p>
    <div className="ui-choice-list ui-choice-list--segmented" role="group" aria-label={t.view}>{['list', 'calendar'].map(value => <button key={value} type="button" aria-pressed={mode === value} onClick={() => setMode(value)}>{value === 'list' ? t.list : t.calendar}</button>)}</div>
    <div className="plan-toolbar">
      <Select size="sm" label={t.type} value={kind} onValueChange={setKind} options={[{ value: 'all', label: t.all }, ...(['item', 'event', 'automatic'] as const).map(value => ({ value, label: t[value] }))]} />
      <Select size="sm" label={t.owner} value={owner} onValueChange={setOwner} options={[{ value: 'all', label: t.all }, ...ownerOptions]} />
      <Select size="sm" label={t.status} value={status} onValueChange={setStatus} options={[{ value: 'all', label: t.all }, { value: 'open', label: t.open }, { value: 'done', label: t.done }]} />
      <Select size="sm" label={priority.filterUrgency} value={urgency} onValueChange={setUrgency} options={[{ value: 'all', label: t.all }, ...todoUrgencies.map(value => ({ value, label: priority[value] }))]} />
    </div>
    {mode === 'calendar' && <><h4>{new Date(`${date}T12:00:00`).toLocaleDateString(locale, { year: 'numeric', month: 'long' })}</h4><div className="plan-toolbar"><Button variant="ghost" onClick={() => shift(-3)}>{t.previous}</Button><TextField label={t.date} type="date" value={date} onChange={e => e.target.value && setDate(e.target.value)} /><Button variant="ghost" onClick={() => shift(3)}>{t.next}</Button><Button variant="secondary" onClick={() => setDate(planDate())}>{t.today}</Button></div><div className="plan-months">{[0,1,2].map(offset => { const month = planDate(new Date(anchor.getFullYear(),anchor.getMonth()+offset,1,12)); return <section key={month}><h4>{new Date(`${month}T12:00:00`).toLocaleDateString(locale,{year:'numeric',month:'long'})}</h4><PlanCalendar date={date} month={month} compact locale={locale} label={`${t.calendar} ${month.slice(0,7)}`} onSelect={setDate} entries={day => filtered.filter(row => occurs(row, day)).map(row => ({ id: `${row.kind}-${row.value.id}`, title: `${t[row.kind]} · ${row.value.title}`, kind: row.kind, muted: row.kind === 'item' ? row.value.completed : !row.value.enabled }))} /></section>; })}</div><p className="ip-muted">{date} · {t.item} / {t.event} / {t.automatic}</p><p className="ip-muted">{t.undated}: {filtered.filter(row => row.kind === 'item' && !row.value.due && !row.value.start).length} · <Button variant="ghost" size="sm" onClick={() => setMode('list')}>{t.list}</Button></p></>}
    {mode === 'list' && filtered.some(row => when(row) !== '9999' && when(row).slice(0,10) > planDate(near)) && <Button variant="ghost" size="sm" onClick={() => setLater(!later)}>{later ? t.hideLater : t.showLater}</Button>}
    {notice && <p role="status">{notice}</p>}
    {draft && <form className="ip-form schedule-editor" aria-label={t.editor} onSubmit={e => {
      e.preventDefault(); const ok = draft.kind === 'item' ? demo.saveTodo(draft.value) : demo.saveSchedule(draft.value);
      if (!ok) { setError(t.invalidTime); return; } setDraft(null); setNotice(t.saved); setKind('all'); setStatus('all'); setUrgency('all'); setOwner(assistantId || 'all'); if (draft.kind === 'item') { if (draft.value.start || draft.value.due) setDate((draft.value.start || draft.value.due).slice(0, 10)); } else setDate(draft.value.date);
    }}>
      <h4>{draft.value.id ? t.edit : t.new}</h4><label htmlFor={`${id}-title`}>{t.name}</label><input autoComplete="off" autoFocus id={`${id}-title`} value={draft.value.title} required maxLength={100} onChange={e => field({ title: e.target.value })} />
      {!draft.value.id && <Select label={t.type} value={draft.kind} onValueChange={(value: Entry['kind']) => start(value, draft.value.title)} options={(['item', 'event', 'automatic'] as const).map(value => ({ value, label: t[value] }))} />}
      {draft.kind === 'item' ? <>
        <label htmlFor={`${id}-due`}>{t.due}</label><input autoComplete="off" id={`${id}-due`} type="date" value={draft.value.due} onChange={e => field({ due: e.target.value })} />
        <label><input type="checkbox" checked={!!draft.value.start} onChange={e => field(e.target.checked ? { start: `${date}T09:00`, end: `${date}T10:00` } : { start: '', end: '' })} />{t.block}</label>
        {draft.value.start && <div className="schedule-time-fields"><label>{t.start}<input autoComplete="off" type="datetime-local" required value={draft.value.start} onChange={e => field({ start: e.target.value })} /></label><label>{t.end}<input autoComplete="off" type="datetime-local" required value={draft.value.end} min={draft.value.start} onChange={e => field({ end: e.target.value })} /></label></div>}
        <details open={!!draft.value.id}><summary>{t.details}</summary><Select label={t.owner} value={draft.value.assistantId || 'person'} onValueChange={value => field({ owner: value === 'person' ? 'person' : 'assistant', assistantId: value === 'person' ? '' : value })} options={ownerOptions} /><Select<TodoUrgency> label={priority.urgency} value={draft.value.urgency} onValueChange={value => field({ urgency: value })} options={todoUrgencies.map(value => ({ value, label: priority[value] }))} /><label>{t.notes}<textarea autoComplete="off" value={draft.value.notes} onChange={e => field({ notes: e.target.value })} /></label><p className="ip-muted">{t.assignment}</p></details>
      </> : <>
        {draft.kind === 'automatic' && <Select label={t.owner} value={draft.value.assistantId} onValueChange={value => field({ assistantId: value })} options={ownerOptions.slice(1)} />}
        <div className="schedule-time-fields"><label>{common.schedulePanel.startDate}<input autoComplete="off" type="date" required value={draft.value.date} onChange={e => field({ date: e.target.value })} /></label><label>{t.start}<input autoComplete="off" type="time" required value={draft.value.time} onChange={e => field({ time: e.target.value })} /></label>{draft.kind === 'event' && <label>{t.end}<input autoComplete="off" type="time" required value={draft.value.endTime} onChange={e => field({ endTime: e.target.value })} /></label>}</div>
        <Select label={t.repeat} value={draft.value.repeat} onValueChange={(value: DemoSchedule['repeat']) => field({ repeat: value })} options={(['once', 'daily', 'weekdays', 'weekly'] as const).map(value => ({ value, label: t[value] }))} />
        {draft.kind === 'event' && <label>{common.schedulePanel.location}<input autoComplete="off" value={draft.value.location} onChange={e => field({ location: e.target.value })} /></label>}
        <label>{draft.kind === 'automatic' ? t.instructions : t.notes}<textarea autoComplete="off" required={draft.kind === 'automatic'} value={draft.value.detail} onChange={e => field({ detail: e.target.value })} /></label>
        {draft.kind === 'automatic' && <p className="ip-muted">{t.automaticHelp}</p>}
      </>}
      {draft.kind !== 'item' && draft.value.id && draft.value.repeat !== 'once' && <p className="ip-muted">{t.seriesEdit}</p>}
      {error && <p role="alert">{error}</p>}<div className="ip-actions"><Button type="submit" size="sm">{t.save}</Button><Button variant="ghost" size="sm" onClick={() => setDraft(null)}>{t.cancel}</Button></div>
    </form>}
    <div className="plan-list">{rows.length ? rows.map(row => <article className="plan-row" key={`${row.kind}-${row.value.id}`} data-completed={row.kind === 'item' && row.value.completed}>
      <div className="plan-row-heading">{row.kind === 'item' && <input type="checkbox" aria-label={`${t.done}: ${row.value.title}`} checked={row.value.completed} onChange={() => demo.toggleTodo(row.value.id)} />}<h4>{row.value.title}</h4></div>
      <p className="ip-muted">{t[row.kind]} · {demo.assistants.find(a => a.id === row.value.assistantId)?.name ?? t.me}{row.kind === 'item' ? ` · ${priority[row.value.urgency]}` : ` · ${t[row.value.repeat]}${row.kind === 'automatic' ? ` · ${row.value.enabled ? t.enabled : t.paused}` : ''}`}</p>
      {row.kind === 'item' ? <>{row.value.due && <p className="ip-muted">{t.deadline}: {row.value.due}{!row.value.completed && row.value.due < planDate() && priority.overdue}</p>}{row.value.start && <p className="ip-muted">{t.scheduled}: {row.value.start.replace('T', ' ')} – {row.value.end?.replace('T', ' ')}</p>}<p>{row.value.notes}</p></> : <><p className="ip-muted">{mode === 'calendar' ? date : row.value.date} · {row.value.time}{row.value.endTime && ` – ${row.value.endTime}`} {row.value.location}</p><p>{row.value.detail}</p></>}
      <div className="ip-actions"><Button variant="ghost" size="sm" onClick={() => { setDraft(structuredClone(row)); setError(''); }}>{t.edit}</Button>{row.kind === 'automatic' && <Button variant="ghost" size="sm" onClick={() => demo.toggleSchedule(row.value.id)}>{row.value.enabled ? t.pause : t.enable}</Button>}<Button variant="ghost" size="sm" onClick={() => setDeleting(row)}>{t.remove}</Button></div>
      {deleting?.value.id === row.value.id && deleting.kind === row.kind && <div className="ip-confirm"><p>{t.deleteHelp}</p><Button size="sm" onClick={() => { if (row.kind === 'item') demo.deleteTodo(row.value.id); else demo.deleteSchedule(row.value.id); setDeleting(null); if (draft?.value.id === row.value.id) setDraft(null); }}>{t.confirm}</Button><Button variant="ghost" size="sm" onClick={() => setDeleting(null)}>{t.cancel}</Button></div>}
    </article>) : <p className="ip-empty">{t.empty}</p>}</div>{(kind === 'all' || kind === 'item') && urgency === 'all' && <div className="plan-list">{workPlans.map(work => <article key={work.id} className="plan-row"><h4>{onOpenExecution ? <Button variant="ghost" size="sm" onClick={() => onOpenExecution(work.id)}>{locale === 'zh' ? work.title : work.titleEn}</Button> : (locale === 'zh' ? work.title : work.titleEn)}</h4><p className="ip-muted">{demo.assistants.find(a => a.id === work.assistantId)?.name} · {t.once} · {t.undated}</p><details><summary>{t.executions} (1)</summary><p>{statusLabels[locale][work.status]} · v{work.revisions.length + 1}</p>{onOpenExecution && <Button variant="ghost" size="sm" onClick={() => onOpenExecution(work.id)}>{t.executionDetails}</Button>}</details></article>)}</div>}<p className="ip-muted">{t.prototype}</p>
  </div>;
}
