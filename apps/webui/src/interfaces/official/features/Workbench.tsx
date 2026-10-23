import { useErrorToast } from '../../../error-toasts';
import type { ReactNode } from 'react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { ModalPresence, Alert, Badge, Button, CodeBlock, EmptyState, Progress, SearchField, Select, Tabs, useBreakpoint } from '@molr/ui';
import { FlaskConical, RefreshCw, RotateCcw, Square, X, ArrowDownToLine, Braces, Layers, ChevronRight } from 'lucide-react';
import { api, APIError } from '../../../api/client';
import type { Assistant, AgentStatus, DebugPayload, DebugRun } from '../../../api/types';
import { useCopy, useData, useConnection, useQuery } from '../../../state';
import { Confirm, Loading, PageHeader } from '../components/common';
import { MoneyText } from '../components/Money';
import { useTrajectory } from './use-trajectory';
import { projectTrajectory, type TrajectoryRecord } from './trajectory';
import { trajectoryAxisItems, trajectoryAxisFilter, axisRangeIndices } from './trajectory-axis';
import type { AxisMode, AxisRange, AxisItem } from './trajectory-axis';
import { TrajectoryTimeline } from './TrajectoryTimeline';
import { TriggerContent } from './TriggerContent';
import { TrajectoryLedger } from './TrajectoryLedger';
import './workbench.css';
import { contextRoleShares } from './context-role-shares';
import { DreamInspection } from './DreamInspection';

type Props = { turnID?: string; eventID?: string; timezone?: string; available: boolean; assistants: Assistant[]; assistantID: string; onSelectAssistant: (id: string) => void; onSettings: () => void };
type Selection = { runID: string; sequence: number; recordID: string } | 'checkpoint' | null;
export function Workbench(props: Props) {
  const { t } = useCopy();
  if (!props.available) return <section className="page page--with-header"><PageHeader title={t.workbench.title} icon={<FlaskConical />} /><div className="page-body"><EmptyState title={t.workbench.disabled} description={t.workbench.runtimeDisabled} illustration={<FlaskConical />} action={<Button variant="secondary" onClick={props.onSettings}>{t.nav.settings}</Button>} /></div></section>;
  const assistantID = props.assistantID || props.assistants.find(item => item.State === 'active')?.ID || props.assistants[0]?.ID || '';
  if (!assistantID) return <section className="page page--with-header"><PageHeader title={t.workbench.title} icon={<FlaskConical />} /><div className="page-body"><EmptyState title={t.assistants.none} description={t.workbench.empty} /></div></section>;
  return <WorkbenchSession key={`${assistantID}:${props.turnID ?? ''}:${props.eventID ?? ''}`} {...props} assistantID={assistantID} />;
}
function WorkbenchSession({ assistants, assistantID, onSelectAssistant, onSettings, timezone, turnID, eventID }: Props) {
  const { t } = useCopy(), { invalidateTopics } = useData();
  const trajectory = useTrajectory(assistantID), compact = useBreakpoint('wide');
  const [follow, setFollow] = useState(!turnID && !eventID), [selection, setSelection] = useState<Selection>(null), [focusID, setFocusID] = useState<{ id: string }>();
  const [query, setQuery] = useState(''), [filter, setFilter] = useState('all'), [raw, setRaw] = useState(false), [range, setRange] = useState<AxisRange | null>(null), [axisMode, setAxisMode] = useState<AxisMode>('time');
  const [streaming, setStreaming] = useState(false), [revoked, setRevoked] = useState(false), [confirmStop, setConfirmStop] = useState(false);
  const status = useQuery<AgentStatus>(`/api/assistants/${assistantID}/status`);
  const jobs = useQuery<Array<{ State: string; Event: { Kind: string } }>>(`/api/jobs?assistant_id=${assistantID}&state=queued&state=retry`, true);
  const records = useMemo(() => projectTrajectory(trajectory.runs, trajectory.entries), [trajectory.runs, trajectory.entries]);
  const visible = useMemo(() => raw ? projectTrajectory(trajectory.runs, trajectory.entries, true) : records, [trajectory.runs, trajectory.entries, raw, records]);
  const axisItems = useMemo(() => trajectoryAxisItems(trajectory.runs, trajectory.entries, records), [trajectory.runs, trajectory.entries, records]);
  const axisFilter = useMemo(() => trajectoryAxisFilter(axisItems, range), [axisItems, range]);
  useEffect(() => { if (range?.mode === 'event' && !axisRangeIndices(axisItems, range)) setRange(null); }, [axisItems, range]);
  const triggers = t.workbench.triggers as Record<string, string>, states = t.workbench.states as Record<string, string>;
  const pending = jobs.data?.filter(job => ['queued', 'retry'].includes(job.State)) ?? [];
  const selectedID = selection && selection !== 'checkpoint' ? selection.recordID : undefined;
  const selectRecord = (record: TrajectoryRecord) => { setFollow(false); setSelection({ runID: record.run.ID, sequence: record.step.Sequence, recordID: record.id }); };
  const setAxisRange = (next: AxisRange | null) => { setRange(next); setFocusID(undefined); setFollow(false); };
  useEffect(() => {
    const source = new EventSource('/api/debug/stream');
    source.onopen = () => { setStreaming(true); invalidateTopics(['debug']); };
    source.onerror = () => setStreaming(false);
    source.addEventListener('change', () => invalidateTopics(['debug', 'jobs', 'status']));
    source.addEventListener('disabled', () => { source.close(); setRevoked(true); invalidateTopics(['settings']); });
    return () => source.close();
  }, [invalidateTopics]);
  const [turnLocated, setTurnLocated] = useState(false), [turnMissing, setTurnMissing] = useState(false);
  useEffect(() => {
    if ((!turnID && !eventID) || turnLocated || trajectory.loading || trajectory.loadingOlder || trajectory.error) return;
    // Runs contains all retained summaries, even when its steps are on older pages.
    const run = trajectory.runs.find(run => turnID ? run.ExecutionID === turnID : run.EventID === eventID);
    if (!run) { setTurnMissing(true); setTurnLocated(true); return; }
    const entries = trajectory.entries.filter(entry => entry.RunID === run.ID);
    const start = entries.find(entry => entry.Step.Kind === 'run.start');
    if (!start && trajectory.older) { trajectory.loadOlder(); return; }
    if (!entries.length) { setTurnMissing(true); setTurnLocated(true); return; }
    const id = `run:${run.ID}`;
    setFollow(false); setSelection({ runID: run.ID, sequence: start?.Step.Sequence ?? entries[0].Step.Sequence, recordID: id });
    setFocusID({ id }); setTurnLocated(true);
  }, [turnID, eventID, turnLocated, trajectory.loading, trajectory.loadingOlder, trajectory.error, trajectory.runs, trajectory.entries, trajectory.older, trajectory.loadOlder]);
  useErrorToast(trajectory.error, trajectory.reload);
  if (revoked || (trajectory.error instanceof APIError && [401, 403].includes(trajectory.error.status))) return <section className="page page--with-header"><PageHeader title={t.workbench.title} icon={<FlaskConical />} /><div className="page-body"><EmptyState title={t.workbench.disabled} description={t.workbench.accessDenied} action={<Button onClick={onSettings}>{t.nav.settings}</Button>} /></div></section>;
  const changeFollow = (value: boolean) => { setFollow(value); if (value) { setQuery(''); setFilter('all'); setRange(null); setFocusID(undefined); } };
  return <section className="page workbench">
    <PageHeader className="workbench-header" title={t.workbench.title} icon={<FlaskConical />} actions={<div className="workbench-header-actions">
        <span className="workbench-live" data-connected={streaming} title={`${t.workbench.agentState}: ${t.chat[status.data?.State ?? 'idle']}`}><i /><span>{streaming ? t.workbench.live : t.reconnecting}</span></span>
        {['working', 'dreaming'].includes(status.data?.State ?? '') && <Button size="sm" variant="ghost" aria-label={t.assistants.stop} onClick={() => setConfirmStop(true)}><Square size={14} /><span>{t.assistants.stop}</span></Button>}
        <Button size="sm" variant="ghost" aria-label={t.workbench.checkpoint} title={t.workbench.checkpoint} onClick={() => { setFollow(false); setSelection('checkpoint'); }}><Layers size={15} /><span>{t.workbench.checkpointShort}</span></Button>
        <Button className="workbench-refresh" variant="ghost" size="sm" aria-label={t.refresh} title={t.refresh} onClick={() => { trajectory.reload(); invalidateTopics(['debug', 'jobs', 'status']); }}><RefreshCw size={15} /></Button>
      </div>}>
      <span className="workbench-header-separator" aria-hidden="true">/</span>
      <Select hideLabel size="sm" label={t.workbench.assistant} value={assistantID} placeholder={assistants.find(item => item.ID === assistantID)?.Name} onValueChange={onSelectAssistant} searchable options={assistants.filter(item => item.State === 'active').map(item => ({ value: item.ID, label: item.Name }))} />
    </PageHeader>
    <div className="trajectory-controls">
      <div className="ui-choice-list ui-choice-list--line trajectory-filters" role="group" aria-label={t.workbench.filter}>{[{ value: 'all', label: t.all }, { value: 'model', label: t.workbench.models }, { value: 'tool', label: t.workbench.toolsShort }].map(item => <button key={item.value} type="button" aria-pressed={filter === item.value} disabled={raw} onClick={() => { setFilter(item.value); setFollow(false); setFocusID(undefined); }}>{item.label}</button>)}</div>
      <Button className="trajectory-raw-toggle" variant="ghost" size="sm" role="switch" aria-label={t.workbench.allEvents} title={t.workbench.allEvents} aria-checked={raw} onClick={() => { setRaw(!raw); setFilter('all'); setFollow(false); setFocusID(undefined); }}><Braces size={15} /><span>{t.workbench.eventsShort}</span></Button>
      <Button className="workbench-follow" variant="ghost" size="sm" role="switch" aria-label={t.workbench.follow} title={t.workbench.followShort} aria-checked={follow} onClick={() => changeFollow(!follow)}><ArrowDownToLine size={15} /><span>{t.workbench.followShort}</span></Button>
      <SearchField hideLabel label={t.workbench.searchTrajectory} placeholder={t.workbench.searchTrajectory} value={query} onValueChange={value => { setQuery(value); setFollow(false); setFocusID(undefined); }} />
    </div>
    <TrajectoryTimeline items={axisItems} mode={axisMode} onModeChange={value => { setAxisMode(value); setRange(null); }} selected={selectedID} range={range} timezone={timezone} onRange={setAxisRange} onSelect={(item: AxisItem) => {
      setRaw(false); setRange(null); setQuery(''); setFilter('all'); setFollow(false);
      if (item.record) selectRecord(item.record);
      else setSelection({ runID: item.run.ID, sequence: 1, recordID: item.id });
      setFocusID({ id: item.id });
    }} />
    {(turnID || eventID) && !turnLocated && !trajectory.error && <p className="workbench-turn-notice muted" role="status">{t.workbench.locatingTurn}</p>}
    {turnMissing && <Alert title={t.workbench.turnNotCaptured} />}
    {pending.length > 0 && <details className="workbench-queued"><summary>{t.workbench.queued} · {pending.length}</summary>{pending.map((job, index) => <p key={index}>{triggers[job.Event.Kind] ?? job.Event.Kind} · {states[job.State] ?? job.State}</p>)}</details>}
    <div className={`workbench-grid${selection ? ' workbench-grid--inspecting' : ''}`}>
      <div className="workbench-main" inert={compact && !!selection}>
        {trajectory.loading ? <Loading /> : <TrajectoryLedger runs={trajectory.runs} entries={trajectory.entries} records={visible} query={query} filter={filter} axisFilter={axisFilter} follow={follow} onFollow={changeFollow} onSelect={selectRecord} onSelectRun={runID => { setFollow(false); setSelection({ runID, sequence: 1, recordID: `run:${runID}` }); }} selected={selectedID} focusID={focusID} hasOlder={!!trajectory.older} loadingOlder={trajectory.loadingOlder} loadOlder={trajectory.loadOlder} resetAt={trajectory.resetAt} cleanup={trajectory.cleanup} timezone={timezone} />}
      </div>
      {selection && <WorkbenchInspector key={selection === 'checkpoint' ? selection : `${selection.runID}:${selection.sequence}`} selection={selection} assistant={assistants.find(item => item.ID === assistantID)} onClose={() => setSelection(null)} />}
    </div>
    <ModalPresence>{confirmStop && <Confirm title={t.assistants.stop} description={t.workbench.stopHelp} danger onClose={() => setConfirmStop(false)} onConfirm={() => api.post(`/api/assistants/${assistantID}/stop`)} />}</ModalPresence>
  </section>;
}
function WorkbenchInspector({ selection, assistant, onClose }: { selection: Exclude<Selection, null>; assistant?: Assistant; onClose: () => void }) {
  const { t } = useCopy(), { invalidateTopics } = useData(), { connection } = useConnection();
  const checkpoint = selection === 'checkpoint', runID = checkpoint ? '' : selection.runID;
  const sequence = checkpoint ? 0 : selection.sequence;
  const [tab, setTab] = useState(checkpoint ? 'context' : 'overview');
  const [confirmReset, setConfirmReset] = useState<number | null>(null);
  const close = useRef<HTMLButtonElement>(null);
  useEffect(() => { const previous = document.activeElement; close.current?.focus({ preventScroll: true }); return () => { if (previous instanceof HTMLElement && previous.isConnected) previous.focus({ preventScroll: true }); }; }, []);
  const run = useQuery<DebugRun>(runID ? `/api/debug/runs/${runID}` : null);
  const steps = run.data?.Steps ?? [], step = steps.find(row => row.Sequence === sequence);
  const detail = useQuery<DebugPayload>(checkpoint && assistant ? `/api/debug/assistants/${assistant.ID}/checkpoint` : runID ? `/api/debug/runs/${runID}/steps/${sequence}` : null);
  const modelStep = steps.find(row => row.Kind === 'model.start' && row.OperationID === step?.OperationID) ?? steps.filter(row => row.Kind === 'model.start' && row.Sequence <= sequence).at(-1);
  const checkpointStep = step?.Kind === 'checkpoint.saved' || step?.Kind === 'context.build';
  const previousStep = steps.filter(row => (checkpointStep ? ['checkpoint.saved', 'context.build'] : ['model.start', 'context.build']).includes(row.Kind) && row.Sequence < (checkpointStep ? sequence : modelStep?.Sequence ?? 0)).at(-1);
  const contextQuery = useQuery<DebugPayload>(!checkpoint && !checkpointStep && modelStep && modelStep.Sequence !== sequence && ['context', 'diff'].includes(tab) ? `/api/debug/runs/${runID}/steps/${modelStep.Sequence}` : null);
  const previous = useQuery<DebugPayload>(!checkpoint && previousStep && tab === 'diff' ? `/api/debug/runs/${runID}/steps/${previousStep.Sequence}` : null);
  const wireStep = steps.find(row => row.OperationID === modelStep?.OperationID && row.Kind === (tab === 'request' ? 'model.request' : 'model.response'));
  const wire = useQuery<DebugPayload>(!checkpoint && wireStep && ['request', 'response'].includes(tab) ? `/api/debug/runs/${runID}/steps/${wireStep.Sequence}` : null);
  const completionStep = modelStep && steps.find(row => row.OperationID === modelStep.OperationID && row.Kind === 'model.complete');
  const completion = useQuery<DebugPayload>(completionStep && completionStep.Sequence !== sequence && tab === 'response' ? `/api/debug/runs/${runID}/steps/${completionStep.Sequence}` : null);
  const toolStart = step?.Kind === 'tool.complete' ? steps.find(row => row.OperationID === step.OperationID && row.Kind === 'tool.start') : undefined;
  const input = useQuery<DebugPayload>(toolStart && tab === 'overview' ? `/api/debug/runs/${runID}/steps/${toolStart.Sequence}` : null);
  const contextData = checkpoint || checkpointStep || hasContext(detail.data) ? detail.data : contextQuery.data;
  const labels = t.workbench.kinds as Record<string, string>;
  useErrorToast(run.error || detail.error || contextQuery.error || previous.error || wire.error || completion.error || input.error, () => invalidateTopics(['debug', 'debug-payload']));
  return <section className="workbench-inspector" aria-label={t.workbench.details} onKeyDown={event => { if (event.key === 'Escape') { event.stopPropagation(); onClose(); } }}>
    <div className="workbench-inspector-heading"><div className="workbench-inspector-title"><h2>{checkpoint ? t.workbench.checkpoint : step ? labels[step.Kind] ?? step.Kind : t.workbench.details}</h2>{step && <div className="workbench-inspector-subtitle"><span>{t.workbench.stepColumn} {sequence}</span><Badge size="sm" tone={step.State === 'succeeded' ? 'positive' : ['failed', 'interrupted'].includes(step.State) ? 'danger' : 'neutral'}>{(t.workbench.states as Record<string, string>)[step.State] ?? step.State}</Badge></div>}</div><Button ref={close} size="sm" variant="ghost" aria-label={t.close} onClick={onClose}><X size={16} /></Button></div>
    {checkpoint && assistant?.State === 'active' && <Button size="sm" variant="secondary" disabled={detail.loading || !!detail.error || typeof detail.data?.Data?.Sequence !== 'number'} onClick={() => { const value = detail.data?.Data?.Sequence; if (typeof value === 'number') setConfirmReset(value); }}><RotateCcw size={14} />{t.workbench.resetContext}</Button>}
    {run.data?.Incomplete && <p className="muted">{t.workbench.incompleteHelp}</p>}
    {detail.loading && !detail.data && <Loading />}
    {detail.data && <Tabs label={t.workbench.details} value={tab} onValueChange={setTab} items={(['overview', 'context', 'diff', 'request', 'response', 'raw'] as const).filter(value => !checkpoint || ['overview', 'context', 'raw'].includes(value)).map(value => ({ value, label: t.workbench.tabs[value], content: value !== tab ? null : <div className="workbench-detail-view">{value === 'context' ? <ContextView payload={contextData} /> : value === 'diff' ? <ContextDiff current={contextData} previous={previous.data} loading={previous.loading || contextQuery.loading} /> : value === 'response' ? <ResponseView key={modelStep?.OperationID ?? sequence} payload={completionStep?.Sequence === sequence ? detail.data : completion.data} wire={wire.data} loading={completion.loading || wire.loading} /> : value === 'request' ? wire.data ? <WireView payload={wire.data} /> : wire.loading ? <Loading /> : <InspectionEmpty>{t.workbench.notCaptured}</InspectionEmpty> : value === 'raw' ? <PayloadView payload={detail.data} /> : <Overview operationID={checkpoint ? undefined : step?.OperationID} payload={detail.data!} run={run.data} connection={connection} toolInput={toolStart ? input.data ?? null : undefined} />}</div> }))} />}
    <ModalPresence>{confirmReset !== null && assistant && <Confirm title={t.workbench.resetContext} description={t.workbench.resetContextHelp(assistant.Name)} danger onClose={() => setConfirmReset(null)} onConfirm={async () => { await api.mutate(`/api/debug/assistants/${assistant.ID}/context/reset`, { ExpectedSequence: confirmReset }); invalidateTopics(['debug', 'jobs', 'status', 'assistants']); }} />}</ModalPresence>
  </section>;
}
function InspectionSection({ title, count, children, collapsible = false }: { title: string; count?: number; children: ReactNode; collapsible?: boolean }) {
  if (collapsible) return <InspectionDisclosure title={title} count={count} open>{children}</InspectionDisclosure>;
  return <section className="workbench-content-block"><div className="workbench-section-heading"><h3>{title}</h3>{count !== undefined && <span className="workbench-count">{count}</span>}</div>{children}</section>;
}
function InspectionDisclosure({ title, count, children, open = false, className = '' }: { title: string; count?: number; children: ReactNode; open?: boolean; className?: string }) {
  return <details className={`workbench-disclosure ${className}`} open={open}><summary><ChevronRight size={14} aria-hidden="true" /><h3>{title}</h3>{count !== undefined && <span className="workbench-count">{count}</span>}</summary><div className="workbench-disclosure-body">{children}</div></details>;
}
function InspectionEmpty({ children }: { children: ReactNode }) {
  return <p className="workbench-empty">{children}</p>;
}
function PayloadView({ payload }: { payload?: DebugPayload | null }) {
  const { t } = useCopy();
  if (!payload?.Data) return <InspectionEmpty>{payload?.Status === 'capacity_reached' ? t.workbench.capacityReached : payload?.Status === 'too_large' ? t.workbench.payloadLimited : t.workbench.notCaptured}</InspectionEmpty>;
  return <>{payload.Status === 'redacted' && <p className="workbench-capture-note">{t.workbench.redacted}</p>}<InspectionSection collapsible title={t.workbench.tabs.raw}><JSONBlock value={payload.Data} /></InspectionSection></>;
}
function WireView({ payload }: { payload: DebugPayload }) {
  const { t } = useCopy();
  const data = payload.Data;
  if (!data) return <PayloadView payload={payload} />;
  const request = data.Request !== undefined;
  if (!request && data.Body === undefined) return <PayloadView payload={payload} />;
  let body: unknown = request ? data.Request : data.Body;
  if (typeof body === 'string') { try { body = JSON.parse(body); } catch { /* Keep captured SSE and non-JSON bodies verbatim. */ } }
  const { Body: _body, Request: _request, ...metadata } = data;
  return <div className="workbench-wire-view">
    {payload.Status === 'redacted' && <p className="workbench-capture-note">{t.workbench.redacted}</p>}
    {data.Status != null && <dl className="workbench-inline-facts"><div><dt>{t.workbench.httpStatus}</dt><dd>{data.Status}</dd></div></dl>}
    {request && data.Model && <InspectionDisclosure title={t.workbench.modelConfiguration}><JSONBlock value={data.Model} /></InspectionDisclosure>}
    <InspectionSection collapsible title={request ? t.workbench.requestBody : t.workbench.responseBody}>{typeof body === 'string' ? <CodeBlock code={body} maxHeight="none" language="text" label={t.workbench.responseBody} /> : <JSONBlock value={body} />}</InspectionSection>
    {Object.keys(metadata).some(key => key !== (request ? 'Model' : 'Status')) && <InspectionDisclosure title={t.workbench.metadata}><JSONBlock value={metadata} /></InspectionDisclosure>}
  </div>;
}
function ResponseView({ payload, wire, loading }: { payload?: DebugPayload | null; wire?: DebugPayload | null; loading: boolean }) {
  const { t } = useCopy();
  const [tab, setTab] = useState('content');
  // Runtime projects the output message from the retained context on demand.
  const data = payload?.Data, message = data?.ProviderState?.Payload;
  const thinking = typeof message?.reasoning_content === 'string' ? message.reasoning_content : '';
  const content = typeof data?.Text === 'string' ? data.Text : '';
  const refusal = typeof message?.refusal === 'string' ? message.refusal : '';
  const calls = Array.isArray(data?.Calls) ? data.Calls : [];
  const captured = typeof data?.Text === 'string';
  return <Tabs variant="segmented" size="sm" label={t.workbench.tabs.response} value={tab} onValueChange={setTab} items={[
    { value: 'content', label: t.workbench.responseContent, content: tab !== 'content' ? null : loading && !payload ? <Loading /> : <div className="workbench-response-content">
      {payload?.Status === 'redacted' && <p className="workbench-capture-note">{t.workbench.redacted}</p>}
      {thinking && <InspectionDisclosure className="workbench-response-section" title="thinking"><div className="workbench-prose">{thinking}</div></InspectionDisclosure>}
      {content && <InspectionDisclosure className="workbench-response-section" title="content" open><div className="workbench-prose">{content}</div></InspectionDisclosure>}
      {refusal && <InspectionSection collapsible title={t.workbench.responseRefusal}><div className="workbench-prose">{refusal}</div></InspectionSection>}
      {calls.length > 0 && <InspectionSection collapsible title={t.workbench.toolCall} count={calls.length}><JSONBlock value={calls} /></InspectionSection>}
      {!thinking && !content && !refusal && !calls.length && <InspectionEmpty>{payload?.Status === 'capacity_reached' ? t.workbench.capacityReached : payload?.Status === 'too_large' ? t.workbench.payloadLimited : captured ? t.workbench.responseEmpty : t.workbench.responseUnavailable}</InspectionEmpty>}
    </div> },
    { value: 'raw', label: t.workbench.responseRaw, content: tab !== 'raw' ? null : <><p className="workbench-section-note">{t.workbench.responseLogHelp}</p>{wire ? <WireView payload={wire} /> : loading ? <Loading /> : <PayloadView />}</> },
  ]} />;
}
function contextInputs(payload?: DebugPayload | null): Array<Record<string, any>> {
  const data = payload?.Data;
  return data?.Inputs ?? data?.Context?.Inputs ?? data?.Checkpoint?.Context?.Inputs ?? [];
}
function hasContext(payload?: DebugPayload | null): boolean {
  const data = payload?.Data;
  return Array.isArray(data?.Inputs ?? data?.Context?.Inputs ?? data?.Checkpoint?.Context?.Inputs);
}
function ContextView({ payload }: { payload?: DebugPayload | null }) {
  const { t } = useCopy(); const inputs = contextInputs(payload);
  const snapshot = payload?.Data?.Checkpoint ?? (payload?.Data?.Context ? payload.Data : undefined);
  if (payload?.Data?.ContextOmitted) return <><dl className="workbench-inline-facts workbench-checkpoint-metadata"><div><dt>{t.workbench.checkpointVersion}</dt><dd>{payload.Data.Sequence}</dd></div><div><dt>{t.workbench.savedAt}</dt><dd>{new Date(payload.Data.SavedAt).toLocaleString()}</dd></div><div><dt>{t.workbench.sourceLabel}</dt><dd>{(t.workbench.checkpointCauses as Record<string, string>)[payload.Data.Cause] ?? payload.Data.Cause}</dd></div></dl><InspectionEmpty>{t.workbench.checkpointSummaryHelp}</InspectionEmpty></>;
  if (!hasContext(payload)) return <PayloadView payload={{ Status: payload?.Status ?? 'unavailable', Data: null }} />;
  return <>
    {snapshot && <dl className="workbench-inline-facts workbench-checkpoint-metadata"><div><dt>{t.workbench.checkpointVersion}</dt><dd>{snapshot.Sequence}</dd></div>{snapshot.SavedAt && snapshot.Sequence > 0 && <div><dt>{t.workbench.savedAt}</dt><dd>{new Date(snapshot.SavedAt).toLocaleString()}</dd></div>}{payload?.Data?.Cause && <div><dt>{t.workbench.sourceLabel}</dt><dd>{(t.workbench.checkpointCauses as Record<string, string>)[payload.Data.Cause] ?? payload.Data.Cause}</dd></div>}</dl>}
    <ContextRoleShares inputs={inputs} />
    <InspectionSection title={t.workbench.contextMessages} count={inputs.length}>
      <p className="workbench-section-note">{snapshot ? t.workbench.checkpointHelp : t.workbench.contextHelp}</p>
      {inputs.length ? <div className="workbench-message-list">{inputs.map((input, index) => <details className="workbench-message workbench-disclosure" key={index} open={index === inputs.length - 1}>
        <summary><ChevronRight size={14} aria-hidden="true" /><span className="workbench-message-index">{index + 1}</span><Badge size="sm" dot={false}>{input.Role}</Badge><span className="workbench-message-source">{input.Role === 'system' ? input.Text?.startsWith('Runtime event:') ? t.workbench.sourceEvent : t.workbench.sourceSystem : input.Role === 'tool' ? t.workbench.sourceTool : input.Role === 'user' ? t.workbench.userInput : t.workbench.sourceModel}</span></summary>
        <div className="workbench-disclosure-body"><ContextMessageText role={input.Role} text={input.Text} />
          {input.ProviderState != null && <InspectionDisclosure title={t.workbench.providerState}><JSONBlock value={input.ProviderState} /></InspectionDisclosure>}
          {input.Calls?.length > 0 && <InspectionSection collapsible title={t.workbench.toolCall} count={input.Calls.length}><JSONBlock value={input.Calls} /></InspectionSection>}
          {input.FileIDs?.length > 0 && <InspectionSection collapsible title={t.workbench.attachments} count={input.FileIDs.length}><JSONBlock value={input.FileIDs} /></InspectionSection>}
          {input.ReplyTo && <dl className="workbench-inline-facts"><div><dt>{t.workbench.replyTo}</dt><dd>{input.ReplyTo}</dd></div></dl>}
        </div>
      </details>)}</div> : <InspectionEmpty>{t.workbench.emptyContext}</InspectionEmpty>}
    </InspectionSection>
    {payload?.Data?.Tools && <InspectionDisclosure title={t.workbench.tools}><JSONBlock value={payload.Data.Tools} /></InspectionDisclosure>}
  </>;
}
function ContextRoleShares({ inputs }: { inputs: Array<Record<string, unknown>> }) {
  const { t } = useCopy();
  const shares = useMemo(() => contextRoleShares(inputs), [inputs]);
  return <section className="workbench-role-shares" aria-label={t.workbench.roleShares}>
    <div className="workbench-section-heading"><h3>{t.workbench.roleShares}</h3></div>
    <p className="workbench-section-note">{t.workbench.roleSharesHelp}</p>
    <div className="workbench-role-grid">{shares.map(({ role, characters, percentage }) => <Progress key={role} label={role} value={percentage} valueText={`${percentage.toFixed(1)}% · ${t.workbench.characters(characters)}`} />)}</div>
  </section>;
}
function ContextDiff({ current, previous, loading }: { current?: DebugPayload | null; previous?: DebugPayload | null; loading: boolean }) {
  const { t } = useCopy(); if (loading) return <Loading />;
  if (!hasContext(current) || !hasContext(previous)) return <InspectionEmpty>{t.workbench.noComparison}</InspectionEmpty>;
  const before = contextInputs(previous), after = contextInputs(current);
  let prefix = 0, suffix = 0;
  while (prefix < before.length && prefix < after.length && JSON.stringify(before[prefix]) === JSON.stringify(after[prefix])) prefix++;
  while (suffix < before.length - prefix && suffix < after.length - prefix && JSON.stringify(before[before.length - 1 - suffix]) === JSON.stringify(after[after.length - 1 - suffix])) suffix++;
  const removed = before.slice(prefix, before.length - suffix), added = after.slice(prefix, after.length - suffix);
  return <><dl className="workbench-diff-stats"><div><dt>{t.workbench.unchanged}</dt><dd>{prefix + suffix}</dd></div><div><dt>{t.workbench.removed}</dt><dd>{removed.length}</dd></div><div><dt>{t.workbench.added}</dt><dd>{added.length}</dd></div></dl>
    <InspectionSection collapsible={removed.length > 0} title={t.workbench.removed} count={removed.length}>{removed.length ? <JSONBlock value={removed} /> : <InspectionEmpty>{t.workbench.noRemovedMessages}</InspectionEmpty>}</InspectionSection>
    <InspectionSection collapsible={added.length > 0} title={t.workbench.added} count={added.length}>{added.length ? <JSONBlock value={added} /> : <InspectionEmpty>{t.workbench.noAddedMessages}</InspectionEmpty>}</InspectionSection></>;
}
function JSONBlock({ value }: { value: unknown }) {
  return <CodeBlock maxHeight="none" language="json" code={JSON.stringify(value, null, 2) ?? 'null'} />;
}
function ContextMessageText({ role, text = '' }: { role?: string; text?: string }) {
  if (role === 'system') {
    const prefix = ['Runtime event: ', 'Runtime context_changed: '].find(value => text.startsWith(value));
    if (prefix) {
      const body = text.slice(prefix.length), newline = body.indexOf('\n');
      if (prefix === 'Runtime context_changed: ' && newline >= 0) {
        try {
          const snapshot: unknown = JSON.parse(body.slice(0, newline));
          return <><p className="workbench-section-note">{prefix.trim()}</p><JSONBlock value={snapshot} /><div className="workbench-prose">{body.slice(newline + 1)}</div></>;
        } catch { /* Preserve legacy or unrecognized snapshots verbatim. */ }
      }
      return <><p className="workbench-section-note">{prefix.trim()}</p><InspectionValue value={body} /></>;
    }
  }
  return <InspectionValue value={text} />;
}
function InspectionValue({ value }: { value: unknown }) {
  let content = value;
  if (typeof value === 'string') { try { content = JSON.parse(value); } catch { /* Render ordinary text verbatim. */ } }
  if (typeof content === 'string') return <div className="workbench-prose">{content}</div>;
  return <JSONBlock value={content} />;
}
function ResultContent({ value }: { value: any }) {
  if (typeof value?.Content?.Text === 'string') return <InspectionValue value={value.Content.Text} />;
  if (Array.isArray(value?.Items) && value.Items.length && value.Items.every((item: any) => typeof item?.Content?.Text === 'string')) return <div className="workbench-return-messages">{value.Items.map((item: any, index: number) => <div className="workbench-return-message" key={index}><small>{item.Sender}</small><InspectionValue value={item.Content.Text} /></div>)}</div>;
  return <InspectionValue value={value} />;
}
function Overview({ payload, run, connection, operationID, toolInput }: { payload: DebugPayload; run?: DebugRun; connection: string; operationID?: string; toolInput?: DebugPayload | null }) {
  const { t } = useCopy(), data = payload.Data;
  const operation = useQuery<{ State: string; ReceiptID: string }>(operationID ? `/api/operations/${operationID}` : null);
  const usage = data?.Receipt?.Usage;
  const hasText = typeof data?.Text === 'string', hasResult = data?.Result != null, hasInput = data?.Arguments != null;
  return <>
    {run?.Trigger === 'dream_due' && run.Source?.ID && <DreamInspection id={run.Source.ID} />}
    {payload.Status === 'redacted' && <p className="workbench-capture-note">{t.workbench.redacted}</p>}
    {data?.Attempt && <dl className="workbench-inline-facts" data-testid="model-retry-details"><div><dt>{t.workbench.retryAttempt}</dt><dd>{data.Attempt}/{data.MaxAttempts}</dd></div>{(data.Status || data.Failure?.Status) && <div><dt>{t.workbench.httpStatus}</dt><dd>{data.Status || data.Failure.Status}</dd></div>}{data.NextAt && <div><dt>{t.workbench.retryNext}</dt><dd>{new Date(data.NextAt).toLocaleString()}</dd></div>}</dl>}
    {data?.Name && <code className="workbench-tool-name">{data.Name}</code>}
    {toolInput !== undefined && <InspectionSection collapsible title={t.workbench.input}>{toolInput?.Data ? <InspectionValue value={toolInput.Data.Arguments} /> : <PayloadView payload={toolInput} />}</InspectionSection>}
    {data?.ContextOmitted && <p className="muted">{t.workbench.checkpointSummaryHelp}</p>}
    {data?.Source ? <InspectionDisclosure title={t.workbench.triggerText} open><TriggerContent source={data.Source} /></InspectionDisclosure> : hasText || hasResult || hasInput ? <InspectionSection collapsible title={hasInput ? t.workbench.input : t.workbench.output}>{hasText ? <InspectionValue value={data!.Text} /> : hasResult ? <ResultContent value={data!.Result.Content ?? data!.Result} /> : <InspectionValue value={data!.Arguments} />}</InspectionSection> : <PayloadView payload={payload} />}
    {!!data?.Calls?.length && <InspectionSection collapsible title={t.workbench.toolCall} count={data.Calls.length}><InspectionValue value={data.Calls} /></InspectionSection>}
    {usage && <InspectionSection title={t.workbench.usage}><dl className="workbench-usage"><div><dt>{t.workbench.inputTokens}</dt><dd>{usage.InputTokens}</dd></div><div><dt>{t.workbench.outputTokens}</dt><dd>{usage.OutputTokens}</dd></div><div><dt>{t.workbench.cached}</dt><dd>{usage.CachedTokens}</dd></div></dl></InspectionSection>}
    {data?.Receipt?.Charged && <div className="workbench-cost"><span>{t.settings.spent}</span><MoneyText value={data.Receipt.Charged} /></div>}
    {(data?.Reason || run?.Reason) && <p className="workbench-reason">{(t.waiting.reasons as Record<string, string>)[data?.Reason || run?.Reason || ''] ?? data?.Reason ?? run?.Reason}</p>}
    <InspectionDisclosure className="workbench-metadata" title={t.workbench.metadata}><dl className="connection-details">{data?.LogicalID && <div><dt>{t.workbench.logicalCall}</dt><dd>{data.LogicalID}</dd></div>}{run && <><div><dt>{t.workbench.event}</dt><dd>{run.EventID}</dd></div><div><dt>{t.workbench.attempt}</dt><dd>{run.Attempt}</dd></div></>}<div><dt>{t.settings.connection}</dt><dd>{connection}</dd></div>{operation.data && <div><dt>{t.workbench.operationState}</dt><dd>{(t.workbench.states as Record<string, string>)[operation.data.State] ?? operation.data.State}</dd></div>}{data?.Estimate && <div><dt>{t.workbench.estimate}</dt><dd><MoneyText value={data.Estimate} /></dd></div>}{data?.Reservation?.Reserved?.Currency && <div><dt>{t.settings.reserved}</dt><dd><MoneyText value={data.Reservation.Reserved} /></dd></div>}</dl></InspectionDisclosure>
  </>;
}
