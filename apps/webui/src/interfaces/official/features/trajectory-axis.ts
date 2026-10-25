import type { DebugRun, TrajectoryEntry } from '@molr/sdk';
import { trajectoryTurns, type TrajectoryRecord } from './trajectory';

export type AxisMode = 'time' | 'event';
export type AxisRange = { mode: 'time'; bounds: [number, number] } | { mode: 'event'; first: string; last: string };
export type AxisItem = { id: string; run: DebugRun; kind: 'trigger' | 'model' | 'tool'; start: number; end?: number; revision: number; sequence: number; record?: TrajectoryRecord; triggerRunIDs?: string[]; state?: string };
export type AxisFilter = { matches: (record: TrajectoryRecord) => boolean; triggerRuns: ReadonlySet<string> };

// Each logical turn has one trigger at its earliest retained attempt, even if
// that attempt’s payload is on an older page. They do not invent the user's message timestamp.
export function trajectoryAxisItems(runs: DebugRun[], entries: TrajectoryEntry[], records: TrajectoryRecord[]): AxisItem[] {
  const loaded = new Set(entries.map(entry => entry.RunID));
  const starts = new Map(entries.filter(entry => entry.Step.Kind === 'run.start').map(entry => [entry.RunID, entry.Step]));
  const items: AxisItem[] = trajectoryTurns(runs).filter(turn => turn.some(run => loaded.has(run.ID))).map(turn => {
    const run = turn[0];
    return { id: `run:${run.ID}`, run, kind: 'trigger', start: Date.parse(starts.get(run.ID)?.At ?? run.StartedAt), revision: run.FirstRevision ?? starts.get(run.ID)?.Revision ?? 0, sequence: 1, triggerRunIDs: turn.map(run => run.ID), state: turn[turn.length - 1].State };
  });
  for (const record of records) {
    if (record.kind === 'event') continue;
    const first = record.steps[0];
    items.push({ id: record.id, run: record.run, kind: record.kind, start: record.start, end: record.end, revision: first?.Revision ?? 0, sequence: first?.Sequence ?? record.step.Sequence, record });
  }
  return items.sort((a, b) => a.revision - b.revision || a.start - b.start || a.run.ID.localeCompare(b.run.ID) || a.sequence - b.sequence);
}

export function axisPositions(items: AxisItem[], mode: AxisMode) {
  return items.map((item, index) => ({ item, start: mode === 'event' ? index : item.start, end: mode === 'event' ? index + 1 : item.end ?? item.start }));
}

// Event selections persist identities, not page-relative numbers, so prepending
// history and appending live events cannot silently change the selected calls.
export function axisRangeIndices(items: AxisItem[], range: Extract<AxisRange, { mode: 'event' }>): [number, number] | null {
  const first = items.findIndex(item => item.id === range.first), last = items.findIndex(item => item.id === range.last);
  return first < 0 || last < 0 ? null : [Math.min(first, last), Math.max(first, last)];
}

export function trajectoryAxisFilter(items: AxisItem[], range: AxisRange | null): AxisFilter | null {
  if (!range) return null;
  if (range.mode === 'time') {
    const [start, end] = range.bounds;
    return { matches: record => record.start <= end && (record.end ?? record.start) >= start, triggerRuns: new Set(items.filter(item => item.kind === 'trigger' && item.start >= start && item.start <= end).flatMap(item => item.triggerRunIDs ?? [item.run.ID])) };
  }
  const bounds = axisRangeIndices(items, range);
  const selected = bounds ? items.slice(bounds[0], bounds[1] + 1) : [];
  const ids = new Set(selected.map(item => item.id));
  const operations = new Set(selected.filter(item => item.record).map(item => `${item.run.ID}:${item.record!.step.OperationID}`));
  const triggerRuns = new Set(selected.filter(item => item.kind === 'trigger').flatMap(item => item.triggerRunIDs ?? [item.run.ID]));
  return { triggerRuns, matches: record => record.kind !== 'event' ? ids.has(record.id) : record.step.Kind === 'run.start' ? triggerRuns.has(record.run.ID) : !!record.step.OperationID && operations.has(`${record.run.ID}:${record.step.OperationID}`) };
}
