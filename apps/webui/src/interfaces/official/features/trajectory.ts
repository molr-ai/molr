import type { DebugRun, DebugStep, TrajectoryEntry } from '../../../api/types';

export type TrajectoryRecord = {
  id: string; run: DebugRun; step: DebugStep; kind: 'model' | 'tool' | 'event';
  attempt?: number; maxAttempts?: number; nextRetryAt?: string; logicalID?: string; httpStatus?: number; errorReason?: string;
  steps: DebugStep[]; parent?: string; name: string; preview: string;
  start: number; end?: number; duration?: number; state: string; partial: boolean;
};
export const entryID = (entry: TrajectoryEntry) => `${entry.RunID}:${entry.Step.Sequence}`;
export function mergeEntries(before: TrajectoryEntry[], incoming: TrajectoryEntry[], runs: DebugRun[]) {
  const retained = new Set(runs.map(run => run.ID));
  const entries = new Map(before.filter(entry => retained.has(entry.RunID)).map(entry => [entryID(entry), entry]));
  incoming.forEach(entry => { if (retained.has(entry.RunID)) entries.set(entryID(entry), entry); });
  return [...entries.values()].sort((a, b) => (a.Step.Revision ?? 0) - (b.Step.Revision ?? 0) || a.Step.At.localeCompare(b.Step.At) || a.RunID.localeCompare(b.RunID) || a.Step.Sequence - b.Step.Sequence);
}
// Runtime retries retain separate Run IDs, but share one logical turn.
export function trajectoryTurns(runs: DebugRun[]): DebugRun[][] {
  const turns = new Map<string, DebugRun[]>();
  for (const run of runs) {
    const key = `${run.AssistantID}:${run.ExecutionID || run.ID}`;
    const turn = turns.get(key) ?? [];
    turn.push(run); turns.set(key, turn);
  }
  return [...turns.values()];
}

export function projectTrajectory(runs: DebugRun[], entries: TrajectoryEntry[], raw = false): TrajectoryRecord[] {
  const stepsByRun = new Map<string, DebugStep[]>();
  for (const entry of entries) {
    const steps = stepsByRun.get(entry.RunID) ?? []; steps.push(entry.Step); stepsByRun.set(entry.RunID, steps);
  }
  const result: TrajectoryRecord[] = [];
  for (const run of runs) {
    const steps = (stepsByRun.get(run.ID) ?? []).sort((a, b) => a.Sequence - b.Sequence);
    if (raw) {
      result.push(...steps.map(step => ({ id: `${run.ID}:step:${step.Sequence}`, run, step, steps: [step], kind: 'event' as const, name: step.Kind, preview: step.Preview ?? '', start: Date.parse(step.At), state: step.State, partial: false }))); continue;
    }
    const operations = new Map<string, TrajectoryRecord>(); let parent: string | undefined;
    let compacting = false;
    for (const step of steps) {
      if (step.Kind === 'context.compaction') compacting = step.State === 'started';
      const kind = step.Kind.startsWith('model.') ? 'model' : step.Kind.startsWith('tool.') ? 'tool' : null;
      if (!kind || !step.OperationID) continue;
      const id = `${run.ID}:${kind}:${step.OperationID}`;
      let row = operations.get(id);
      if (!row) {
        row = { id, run, step, steps: [], kind, parent: kind === 'tool' ? parent : undefined, name: step.Name ?? '', preview: '', start: Date.parse(step.At), state: 'running', partial: true };
        operations.set(id, row); result.push(row);
      }
      row.attempt = step.Attempt || row.attempt; row.maxAttempts = step.MaxAttempts || row.maxAttempts; row.logicalID = step.LogicalID || row.logicalID; row.nextRetryAt = step.NextRetryAt || row.nextRetryAt; row.httpStatus = step.HTTPStatus || row.httpStatus; row.errorReason = step.ErrorReason || row.errorReason;
      row.steps.push(step); row.step = step; if (kind === 'model') parent = id;
      if (step.Kind === `${kind}.start`) { row.start = Date.parse(step.At); row.partial = false; if (kind === 'model') parent = id; }
      if (step.Name) row.name = step.Name;
      if (kind === 'model' && compacting) row.name = 'context.compaction';
      if (step.Preview) row.preview = step.Kind === 'tool.complete' && row.preview ? `${row.preview} → ${step.Preview}` : step.Preview;
      if (step.Kind === `${kind}.complete`) { row.end = Date.parse(step.At); row.state = step.State; if (!row.partial) row.duration = Math.max(0, row.end - row.start); }
      else if (step.State === 'failed') row.state = 'failed';
    }
    for (const row of operations.values()) {
      if (row.end === undefined && row.state !== 'failed' && run.State !== 'running') row.state = ['failed', 'canceled', 'interrupted'].includes(run.State) ? run.State : 'unknown';
    }
  }
  return result;
}
export function intersects(record: TrajectoryRecord, range: [number, number] | null) {
  return !range || (record.start <= range[1] && (record.end ?? record.start) >= range[0]);
}
export function durationLabel(value?: number) {
  if (value === undefined) return '—';
  return value < 1000 ? `${value} ms` : `${(value / 1000).toFixed(2)} s`;
}

// Prefer the server summary: it was extracted before JSON preview truncation.
// Never interpret a requested limit as the number of successfully read items.
type ToolSummaryCopy = {
  messages: (count: number) => string; memories: (count: number) => string;
  plans: (count: number) => string; attachments: (count: number) => string;
  readMessages: string; readMemories: string; listPlans: string; listPermissions: string;
};
export function recordPreview(record: TrajectoryRecord, copy: ToolSummaryCopy) {
  if (record.kind !== 'tool') return record.preview;
  const start = record.steps.find(step => step.Kind === 'tool.start');
  const complete = record.steps.find(step => step.Kind === 'tool.complete');
  const summary = (complete?.State === 'succeeded' ? complete.Summary : undefined) ?? start?.Summary;
  if (summary?.Kind === 'text' && summary.Text) return summary.Text;
  if (summary?.Count !== undefined && Number.isInteger(summary.Count) && summary.Count >= 0) {
    const kind = summary.Kind;
    if (kind === 'attachments' || (complete?.State === 'succeeded' && ['messages', 'memories', 'plans'].includes(kind))) {
      return copy[kind as 'messages' | 'memories' | 'plans' | 'attachments'](summary.Count);
    }
  }
  const defaults: Record<string, string> = { conversation_read: copy.readMessages, plan_list: copy.listPlans, permission_list: copy.listPermissions };
  if (defaults[record.name]) return defaults[record.name];
  // Unknown tools keep their captured arguments as a bounded fallback. Full
  // input and output remain available in the inspector, loaded only on demand.
  return start?.Preview ?? '';
}
