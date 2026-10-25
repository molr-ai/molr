import { useLayoutEffect, useMemo, useRef, useState } from 'react';
import { Badge, Button, Tooltip, useBreakpoint } from '@molr/ui';
import { Bot, ChevronDown, ChevronRight, Wrench, Activity, RotateCcw, Check, AlertTriangle, LoaderCircle, MessageSquare, ArrowDownToLine, ArrowUp, Info } from 'lucide-react';
import type { AxisFilter } from './trajectory-axis';
import type { DebugCleanupEvent, DebugRun, TrajectoryEntry } from '@molr/sdk';
import { useCopy } from '../../../state';
import { durationLabel, recordPreview, trajectoryTurns, type TrajectoryRecord } from './trajectory';

type Row = { id: string; run?: DebugRun; state?: string; incomplete?: boolean; record?: TrajectoryRecord; reset?: string; partial?: boolean; boundary?: boolean };
type PositionedRow = Row & { top: number; height: number };
function rowIndexAt(rows: PositionedRow[], top: number) {
  let lo = 0, hi = rows.length;
  while (lo < hi) { const mid = (lo + hi) >>> 1; if (rows[mid].top + rows[mid].height <= top) lo = mid + 1; else hi = mid; }
  return Math.min(lo, Math.max(0, rows.length - 1));
}
type Props = { runs: DebugRun[]; entries: TrajectoryEntry[]; records: TrajectoryRecord[]; query: string; filter: string; axisFilter: AxisFilter | null; follow: boolean; onFollow: (value: boolean) => void; onSelect: (record: TrajectoryRecord) => void; onSelectRun: (runID: string) => void; selected?: string; focusID?: { id: string }; hasOlder: boolean; loadingOlder: boolean; loadOlder: () => void; resetAt: string | null; cleanup: DebugCleanupEvent | null; timezone?: string };
export function TrajectoryLedger({ runs, entries, records, query, filter, axisFilter, follow, onFollow, onSelect, onSelectRun, selected, focusID, hasOlder, loadingOlder, loadOlder, resetAt, cleanup, timezone }: Props) {
  const { t, locale } = useCopy(), mobile = useBreakpoint('mobile');

  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set()), [scrollTop, setScrollTop] = useState(0), [height, setHeight] = useState(600);
  const panel = useRef<HTMLDivElement>(null), lastTop = useRef(0), previousRows = useRef<PositionedRow[]>([]), handledFocus = useRef<typeof focusID>(undefined), unfoldedFocus = useRef<typeof focusID>(undefined);
  const labels = t.workbench.kinds as Record<string, string>, states = t.workbench.states as Record<string, string>, triggers = t.workbench.triggers as Record<string, string>;
  const modelsWithTools = useMemo(() => new Set(records.filter(row => row.kind === 'tool' && row.parent).map(row => row.parent!)), [records]);
  const turns = useMemo(() => trajectoryTurns(runs), [runs]);
  const runRoots = useMemo(() => new Map(turns.flatMap(turn => turn.map(run => [run.ID, turn[0].ID] as const))), [turns]);
  const boundaryTitle = cleanup ? t.workbench.earlierEventsCleared : t.workbench.noEarlierEvents;
  const boundarySummary = cleanup ? t.workbench.cleanupPolicy(cleanup.Policy.MaxRuns) : '';
  const searching = !!query.trim() || filter !== 'all' || !!axisFilter;
  const matched = useMemo(() => records.filter(row => {
    if (filter !== 'all' && row.kind !== filter) return false;
    const text = [row.name, row.preview, recordPreview(row, t.workbench.toolSummary), row.run.Trigger, row.run.EventID, row.run.Source?.Title, row.run.Source?.Text, labels[row.name], states[row.state], row.kind === 'model' ? `${t.workbench.modelCall} ${row.name === 'context.compaction' ? t.workbench.checkpointCauses.context_compaction : ''}` : t.workbench.toolCall].join(' ').toLocaleLowerCase();
    return text.includes(query.trim().toLocaleLowerCase()) && (!axisFilter || axisFilter.matches(row));
  }), [records, query, filter, axisFilter, labels, states, t]);
  const rows = useMemo(() => {
    const byRun = new Map<string, TrajectoryRecord[]>(), first = new Map<string, number>();
    for (const record of matched) { const group = byRun.get(record.run.ID) ?? []; group.push(record); byRun.set(record.run.ID, group); }
    for (const entry of entries) first.set(entry.RunID, Math.min(first.get(entry.RunID) ?? Infinity, entry.Step.Sequence));
    const result: Row[] = !hasOlder && filter === 'all' && !axisFilter && `${boundaryTitle} ${boundarySummary}`.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()) ? [{ id: 'boundary', boundary: true }] : []; let resetAdded = false;
    const addReset = () => { if (resetAt && !searching && !resetAdded) { result.push({ id: `reset:${resetAt}`, reset: resetAt }); resetAdded = true; } };
    for (const turn of turns) {
      const run = turn[0], latest = turn[turn.length - 1];
      const loaded = turn.filter(attempt => first.has(attempt.ID));
      const matches = loaded.some(attempt => {
        const sourceMatches = !query.trim() || [attempt.Source?.Title, attempt.Source?.Text, triggers[attempt.Trigger]].join(' ').toLocaleLowerCase().includes(query.trim().toLocaleLowerCase());
        return byRun.has(attempt.ID) || (filter === 'all' && sourceMatches && (!axisFilter || axisFilter.triggerRuns.has(attempt.ID)));
      });
      if (!loaded.length || (searching && !matches)) continue;
      if (resetAt && Date.parse(run.StartedAt) > Date.parse(resetAt)) addReset();
      result.push({ id: `run:${run.ID}`, run, state: latest.State, incomplete: turn.some(attempt => attempt.Incomplete), partial: (first.get(run.ID) ?? Infinity) > 1 });
      if (collapsed.has(run.ID) && !searching) continue;
      for (const attempt of loaded) for (const record of byRun.get(attempt.ID) ?? []) if (!record.parent || !collapsed.has(record.parent) || searching) {
        result.push({ id: record.id, record });
      }
    }
    addReset(); return result;
  }, [turns, matched, entries, searching, collapsed, resetAt, axisFilter, filter, query, triggers, boundaryTitle, boundarySummary, hasOlder]);
  const layout = useMemo(() => {
    let top = 0;
    return rows.map(row => {
      const height = row.boundary ? 36 : row.record && mobile ? 64 : 44;
      const positioned = { ...row, top, height }; top += height; return positioned;
    });
  }, [rows, mobile]);
  const totalHeight = layout.length ? layout.at(-1)!.top + layout.at(-1)!.height : 0;
  useLayoutEffect(() => {
    if (!panel.current) return;
    const observer = new ResizeObserver(([entry]) => setHeight(entry.contentRect.height)); observer.observe(panel.current); return () => observer.disconnect();
  }, []);
  useLayoutEffect(() => {
    const el = panel.current; if (!el) return;
    const previous = previousRows.current;
    if (follow) el.scrollTop = Math.max(0, el.scrollHeight - el.clientHeight);
    else if (previous.length && previous !== layout) {
      let oldIndex = rowIndexAt(previous, lastTop.current);
      if (previous[oldIndex].top < lastTop.current && oldIndex < previous.length - 1) oldIndex++;
      const anchor = previous[oldIndex], offset = anchor.top - lastTop.current;
      const next = layout.find(row => row.id === anchor.id);
      if (next) el.scrollTop = next.top - offset;
      else el.scrollTop = Math.min(el.scrollTop, Math.max(0, el.scrollHeight - el.clientHeight));
    }
    previousRows.current = layout; lastTop.current = el.scrollTop; setScrollTop(el.scrollTop);
  }, [layout, totalHeight, follow, height]);
  useLayoutEffect(() => {
    if (!focusID || unfoldedFocus.current === focusID) return;
    unfoldedFocus.current = focusID;
    const record = records.find(row => row.id === focusID.id);
    if (record) setCollapsed(old => { const next = new Set(old); next.delete(runRoots.get(record.run.ID) ?? record.run.ID); if (record.parent) next.delete(record.parent); return next; });
  }, [focusID, records, runRoots]);
  useLayoutEffect(() => {
    if (!focusID || !panel.current || handledFocus.current === focusID) return;
    const row = layout.find(row => row.id === focusID.id);
    if (row) { handledFocus.current = focusID; panel.current.scrollTop = Math.max(0, row.top - panel.current.clientHeight / 2); lastTop.current = panel.current.scrollTop; setScrollTop(panel.current.scrollTop); }
  }, [focusID, layout]);
  const toggle = (id: string) => { onFollow(false); setCollapsed(old => { const next = new Set(old); if (next.has(id)) next.delete(id); else next.add(id); return next; }); };
  const inspectButton = (onClick: () => void) => <button type="button" className="trajectory-inspect" aria-label={t.workbench.inspectFull} title={t.workbench.inspectFull} onClick={onClick}><ChevronRight size={14} /></button>;
  const timeFormat = useMemo(() => new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false, timeZone: timezone }), [locale, timezone]);
  const showTime = (at: string) => timeFormat.format(new Date(at));
  const turnNumbers = new Map<string, number>();
  const runNumbers = new Map(runs.map(run => { const key = run.ExecutionID || run.ID; if (!turnNumbers.has(key)) turnNumbers.set(key, turnNumbers.size + 1); return [run.ID, turnNumbers.get(key)!]; }));
  const toolLabels = t.assistantInfo.toolLabels as Record<string, { name: string }>;
  const stateIcon = (state: string) => state === 'succeeded' ? <Check size={12} /> : ['failed', 'interrupted', 'canceled', 'unknown'].includes(state) ? <AlertTriangle size={12} /> : <LoaderCircle size={12} />;
  const start = Math.max(0, rowIndexAt(layout, scrollTop) - 4), end = Math.min(layout.length, rowIndexAt(layout, scrollTop + height) + 5);
  const visibleRows = layout.slice(start, end);
  const select = (record: TrajectoryRecord) => { onFollow(false); onSelect(record); };
  const footer = <footer className="trajectory-ledger-footer"><span title={t.workbench.loadedOnly}>{t.workbench.loaded} {records.length} {records[0]?.kind === 'event' ? t.workbench.eventsShort : t.workbench.calls}</span><span className="trajectory-footer-follow" data-follow={follow}>{follow ? <><i />{t.workbench.tailMode}</> : <button type="button" onClick={() => onFollow(true)}><ArrowDownToLine size={12} />{t.workbench.followShort}</button>}</span></footer>;
  return <section className="trajectory-ledger" aria-label={t.workbench.timeline}>
    {hasOlder && <div className="trajectory-history"><Button size="sm" variant="ghost" disabled={loadingOlder} onClick={() => { onFollow(false); loadOlder(); }}><ArrowUp size={12} />{loadingOlder ? t.loading : t.workbench.olderEvents}</Button></div>}
    <div ref={panel} className="trajectory-scroll" role="list" aria-label={t.workbench.trajectory} tabIndex={0}
      onWheel={event => { if (event.deltaY < 0) onFollow(false); }} onKeyDown={event => { if (['ArrowUp', 'PageUp', 'Home'].includes(event.key)) onFollow(false); }}
      onScroll={event => { const top = event.currentTarget.scrollTop; if (top < lastTop.current - 2) onFollow(false); lastTop.current = top; setScrollTop(top); }}>
      {!rows.length && <p className="trajectory-empty muted">{searching ? t.noResults : t.workbench.empty}</p>}
      <div style={{ height: totalHeight, position: 'relative' }}>
        {visibleRows.map((row, index) => <div role="listitem" aria-setsize={rows.length} aria-posinset={start + index + 1} key={row.id} data-record-id={row.id} className={`trajectory-virtual-row${row.boundary ? ' trajectory-virtual-row--boundary' : ''}`} style={{ position: 'absolute', top: row.top, height: row.height, left: 0, right: 0 }}>
          {row.boundary ? <div className="trajectory-boundary" data-boundary={cleanup ? 'cleanup' : 'start'}>
            {cleanup ? <Tooltip content={boundarySummary} width={320} side="bottom" openOnClick><button type="button" className="trajectory-boundary-help"><span>{boundaryTitle}</span><Info size={12} aria-hidden="true" /></button></Tooltip> : <span>{boundaryTitle}</span>}
          </div> : row.reset ? <div className="trajectory-reset"><RotateCcw size={14} /><span>{t.workbench.resetBoundary}</span><time>{showTime(row.reset)}</time></div> : row.run ? <div className="trajectory-run-shell" data-state={row.state!} data-selected={selected === row.id || undefined}><div className="trajectory-run-bar">
            <button type="button" className="trajectory-fold" aria-label={`${collapsed.has(row.run.ID) ? t.workbench.expand : t.workbench.collapse} ${t.workbench.execution} ${runNumbers.get(row.run.ID)}`} aria-expanded={!collapsed.has(row.run.ID)} onClick={() => toggle(row.run!.ID)}>{collapsed.has(row.run.ID) ? <ChevronRight size={13} /> : <ChevronDown size={13} />}</button>
            <button type="button" className={`trajectory-run${row.partial && hasOlder ? ' trajectory-run--partial' : ''}`} aria-pressed={selected === row.id} onClick={() => { onFollow(false); onSelectRun(row.run!.ID); }} title={`${row.run.EventID}${row.partial && hasOlder ? ` · ${t.workbench.earlierSteps}` : ''}`}>
              <span className="trajectory-trigger-kind"><MessageSquare size={15} /><strong title={triggers[row.run.Trigger] ?? row.run.Trigger}>{triggers[row.run.Trigger] ?? row.run.Trigger}</strong><Badge size="sm" dot={false}>{t.workbench.execution} {runNumbers.get(row.run.ID)}</Badge></span>
              <span className="trajectory-trigger-content"><span className="trajectory-trigger-copy" title={[row.run.Source?.Title, row.run.Source?.Text].filter(Boolean).join(' · ')}>{row.run.Source?.Title && <strong>{row.run.Source.Title} · </strong>}{row.run.Source?.Text || (row.run.Source?.AttachmentIDs?.length ? `${t.workbench.attachments} · ${row.run.Source.AttachmentIDs.length}` : '')}</span></span>
              <span className="trajectory-run-status">{row.partial && hasOlder && <small className="trajectory-partial">{t.workbench.earlierSteps}<ArrowUp size={12} aria-hidden="true" /></small>}{row.state! !== 'succeeded' && <span className="trajectory-state" data-state={row.state!}>{stateIcon(row.state!)}{states[row.state!] ?? row.state!}</span>}{row.incomplete && <AlertTriangle size={12} aria-label={t.workbench.incomplete} />}</span>
              <span className="trajectory-run-end"><time dateTime={row.run.StartedAt}>{showTime(row.run.StartedAt)}</time></span>
            </button>{inspectButton(() => { onFollow(false); onSelectRun(row.run!.ID); })}
          </div></div> : row.record && <div className={`trajectory-record trajectory-record--${row.record.kind}`} data-state={row.record.state} data-selected={selected === row.id || undefined}>
            {row.record.kind === 'model' && modelsWithTools.has(row.id) && !searching ? <button type="button" className="trajectory-fold" aria-label={`${collapsed.has(row.id) ? t.workbench.expand : t.workbench.collapse} ${t.workbench.toolCall}`} aria-expanded={!collapsed.has(row.id)} onClick={() => toggle(row.id)}>{collapsed.has(row.id) ? <ChevronRight size={12} /> : <ChevronDown size={12} />}</button> : <span className="trajectory-fold-space" />}
            <button type="button" className="trajectory-record-body" aria-pressed={selected === row.id} onClick={() => select(row.record!)}>
              <span className="trajectory-kind" title={row.record.name}>{row.record.kind === 'model' ? <Bot size={15} /> : row.record.kind === 'tool' ? <Wrench size={13} /> : <Activity size={14} />}<span>{row.record.kind === 'model' ? row.record.name === 'context.compaction' ? t.workbench.checkpointCauses.context_compaction : t.workbench.modelCall : row.record.kind === 'tool' ? toolLabels[row.record.name]?.name || row.record.name || t.workbench.toolCall : labels[row.record.name] ?? row.record.name}</span></span>
              <span className="trajectory-preview" title={recordPreview(row.record, t.workbench.toolSummary)}>{row.record.kind === 'tool' ? <>
                <span className="trajectory-preview-text">{recordPreview(row.record, t.workbench.toolSummary) || t.workbench.noPreview}</span>
              </> : <span className="trajectory-preview-text">{!!row.record.attempt && row.record.attempt > 1 && <strong>{t.workbench.retryLabel} {row.record.attempt - 1}/{(row.record.maxAttempts ?? row.record.attempt) - 1} · </strong>}{(row.record.errorReason && (t.waiting.reasons as Record<string,string>)[row.record.errorReason]) || row.record.preview || t.workbench.noPreview}{row.record.nextRetryAt && <> · {t.workbench.retryNext} {showTime(row.record.nextRetryAt)}</>}</span>}</span>
              <span className="trajectory-state" data-state={row.record.state} title={row.record.step.DataStatus !== 'captured' ? (t.workbench.capture as Record<string, string>)[row.record.step.DataStatus] : undefined}>{stateIcon(row.record.state)}<span>{states[row.record.state] ?? row.record.state}</span>{!['captured', 'redacted'].includes(row.record.step.DataStatus) && <AlertTriangle size={12} />}</span>
              <span className="trajectory-duration" title={row.record.end === undefined ? t.workbench.noEnd : t.workbench.duration}>{durationLabel(row.record.duration)}</span>
            </button>{inspectButton(() => select(row.record!))}
          </div>}
        </div>)}
      </div>
      {mobile && footer}
    </div>
    {!mobile && footer}
  </section>;
}
