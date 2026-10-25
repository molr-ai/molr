import { test, expect } from '@playwright/test';
import type { DebugRun, DebugStep, TrajectoryEntry } from '@molr/sdk';
import { trajectoryAxisItems, axisPositions, axisRangeIndices, trajectoryAxisFilter } from '../src/interfaces/official/features/trajectory-axis';
import { mergeRuns, mergeEntries, projectTrajectory, trajectoryTurns, recordPreview } from '../src/interfaces/official/features/trajectory';

const run = (ID: string, Attempt = 1): DebugRun => ({ ID, AssistantID: 'assistant', EventID: 'event', ExecutionID: 'execution', Trigger: 'user_message', Attempt, State: 'succeeded', Reason: '', StartedAt: '2026-09-27T00:00:00Z', FinishedAt: '2026-09-27T00:00:05Z', Incomplete: false, Steps: null });
const step = (Sequence: number, Kind: string, OperationID: string, At: string, extra: Partial<DebugStep> = {}): DebugStep => ({ Sequence, Kind, OperationID, At, State: Kind.endsWith('start') ? 'started' : 'succeeded', DataStatus: 'captured', ...extra });
const entry = (RunID: string, Step: DebugStep): TrajectoryEntry => ({ RunID, Step });

test('trajectory pairs calls within attempts and attaches tool records to their model', () => {
  const runs = [run('first'), run('retry', 2)];
  const first = [
    step(1, 'model.start', 'same-model', '2026-09-27T00:00:00Z'),
    step(2, 'model.request', 'same-model', '2026-09-27T00:00:01Z'),
    step(3, 'model.complete', 'same-model', '2026-09-27T00:00:02Z', { Preview: 'model output' }),
    step(4, 'tool.start', 'same-tool', '2026-09-27T00:00:03Z', { Name: 'search', Preview: '{"query":"hello"}' }),
    step(5, 'tool.complete', 'same-tool', '2026-09-27T00:00:04Z', { Name: 'search', Preview: 'found' }),
  ];
  const rows = projectTrajectory(runs, runs.flatMap(run => first.map(step => entry(run.ID, step))));
  expect(rows).toHaveLength(4);
  expect(new Set(rows.map(row => row.id)).size).toBe(4);
  expect(rows[0].duration).toBe(2000);
  expect(rows[1].duration).toBe(1000);
  expect(rows[1].parent).toBe(rows[0].id);
  expect(rows[3].parent).toBe(rows[2].id);
  expect(rows[1].preview).toBe('{"query":"hello"} → found');
  expect(rows[0].step.Sequence).toBe(3);
});

test('trajectory does not invent timing for missing starts or interrupted calls', () => {
  const interrupted = { ...run('interrupted'), State: 'interrupted', Incomplete: true };
  const entries = [entry('partial', step(8, 'model.complete', 'model', '2026-09-27T00:00:02Z')), entry('interrupted', step(2, 'tool.start', 'tool', '2026-09-27T00:00:03Z'))];
  const rows = projectTrajectory([run('partial'), interrupted], entries);
  expect(rows[0].partial).toBe(true); expect(rows[0].duration).toBeUndefined();
  expect(rows[1].state).toBe('interrupted'); expect(rows[1].end).toBeUndefined(); expect(rows[1].duration).toBeUndefined();
  const complete = projectTrajectory([run('partial')], [...entries, entry('partial', step(1, 'model.start', 'model', '2026-09-27T00:00:00Z'))]);
  expect(complete[0].id).toBe(rows[0].id); expect(complete[0].duration).toBe(2000);
});

test('trajectory merges reconnect pages by stable identity and removes expired runs', () => {
  const old = entry('expired', step(1, 'run.start', '', '2026-09-27T00:00:00Z'));
  const a = entry('retained', step(2, 'model.start', 'model', '2026-09-27T00:00:00Z', { Revision: 8 }));
  const b = entry('retained', step(3, 'model.complete', 'model', '2026-09-27T00:00:00Z', { Revision: 9 }));
  const merged = mergeEntries([old, a], [b, a], [run('retained')]);
  expect(merged).toEqual([a, b]);
  expect(projectTrajectory([run('retained')], merged, true)).toHaveLength(2);
});


test('trajectory axes include triggers, preserve real idle time and assign equal event slots', () => {
  const first = { ...run('first'), FirstRevision: 10 };
  const scheduled = { ...run('scheduled'), ExecutionID: 'scheduled-execution', FirstRevision: 20, Trigger: 'schedule_due', StartedAt: '2026-09-27T00:10:00Z' };
  const entries = [
    entry('first', step(1, 'run.start', '', '2026-09-27T00:00:00Z', { Revision: 10 })),
    entry('first', step(2, 'model.start', 'model', '2026-09-27T00:00:01Z', { Revision: 11 })),
    entry('first', step(3, 'model.complete', 'model', '2026-09-27T00:00:11Z', { Revision: 12 })),
    entry('scheduled', step(1, 'run.start', '', '2026-09-27T00:10:00Z', { Revision: 20 })),
    entry('scheduled', step(2, 'model.start', 'model', '2026-09-27T00:10:01Z', { Revision: 21 })),
    entry('scheduled', step(3, 'model.complete', 'model', '2026-09-27T00:10:02Z', { Revision: 22 })),
  ];
  const records = projectTrajectory([first, scheduled], entries);
  const items = trajectoryAxisItems([first, scheduled], entries, records);
  expect(items.map(item => item.kind)).toEqual(['trigger', 'model', 'trigger', 'model']);
  expect(items[2].run.Trigger).toBe('schedule_due');
  const time = axisPositions(items, 'time');
  expect(time[1].end - time[1].start).toBe(10000);
  expect(time[3].end - time[3].start).toBe(1000);
  expect(time[2].start - time[1].end).toBe(589000);
  expect(axisPositions(items, 'event').map(item => [item.start, item.end])).toEqual([[0, 1], [1, 2], [2, 3], [3, 4]]);
  const selection = { mode: 'event' as const, first: items[1].id, last: items[1].id };
  const prefixed = [{ ...items[0], id: 'older' }, ...items, { ...items[0], id: 'later' }];
  expect(axisRangeIndices(prefixed, selection)).toEqual([2, 2]);
  const filter = trajectoryAxisFilter(prefixed, selection)!;
  expect(records.filter(filter.matches).map(row => row.run.ID)).toEqual(['first']);
  const triggerOnly = trajectoryAxisFilter(items, { mode: 'event', first: items[2].id, last: items[2].id })!;
  expect([...triggerOnly.triggerRuns]).toEqual(['scheduled']);
  expect(records.filter(triggerOnly.matches)).toEqual([]);
  expect(trajectoryAxisItems([first, scheduled], entries.slice(1), records)[0].id).toBe('run:first');
});

test('retry metadata keeps each attempt and does not merge its response with the successor', () => {
 const runs = [{ ...run('one'), State: 'retrying' }, run('two', 2)];
 const entries = [
  entry('one', step(1, 'model.start', 'a', '2026-09-27T00:00:00Z', { LogicalID: 'logical', Attempt: 1, MaxAttempts: 4 })),
  entry('one', step(2, 'model.complete', 'a', '2026-09-27T00:00:01Z', { State: 'failed', HTTPStatus: 429 })),
  entry('one', step(3, 'model.retry', 'a', '2026-09-27T00:00:01Z', { State: 'retrying', NextRetryAt: '2026-09-27T00:00:03Z' })),
  entry('two', step(1, 'model.start', 'b', '2026-09-27T00:00:03Z', { LogicalID: 'logical', Attempt: 2, MaxAttempts: 4 })),
  entry('two', step(2, 'model.complete', 'b', '2026-09-27T00:00:04Z')),
 ];
 const rows = projectTrajectory(runs, entries);
 expect(rows).toHaveLength(2);
 expect(rows.map(row => row.attempt)).toEqual([1, 2]);
 expect(rows.map(row => row.state)).toEqual(['failed', 'succeeded']);
 expect(rows.map(row => row.duration)).toEqual([1000, 1000]);
 expect(rows[0].nextRetryAt).toBe('2026-09-27T00:00:03Z');
 expect(rows[1].httpStatus).toBeUndefined();
});


test('compaction calls retain model type, timing and payload while receiving a distinct name', () => {
  const steps = [
    step(1, 'context.compaction', 'archive', '2026-09-27T00:00:00Z', { State: 'started' }),
    step(2, 'model.start', 'summary', '2026-09-27T00:00:01Z'),
    step(3, 'model.complete', 'summary', '2026-09-27T00:00:03Z', { Preview: 'summary result' }),
    step(4, 'context.compaction', 'archive', '2026-09-27T00:00:04Z'),
    step(5, 'model.start', 'normal', '2026-09-27T00:00:05Z'),
    step(6, 'model.complete', 'normal', '2026-09-27T00:00:06Z'),
  ];
  const entries = steps.map(item => entry('run', item));
  const rows = projectTrajectory([run('run')], entries);
  expect(rows[0]).toMatchObject({ kind: 'model', name: 'context.compaction', duration: 2000, preview: 'summary result', state: 'succeeded' });
  expect(rows[0].steps).toEqual(steps.slice(1, 3));
  expect(rows[1]).toMatchObject({ kind: 'model', name: '', duration: 1000 });
  expect(trajectoryAxisItems([run('run')], entries, rows).filter(item => item.kind === 'model')).toHaveLength(2);
  expect(projectTrajectory([run('run')], entries, true).map(row => row.name)).toEqual(steps.map(item => item.Kind));
  // Paging in the missing start boundary updates the existing call identity.
  expect(projectTrajectory([run('run')], entries.slice(1))[0].id).toBe(rows[0].id);
});


test('retry turns share one stable trigger across paging while attempts retain separate identities', () => {
  const first = { ...run('first'), State: 'retrying', FirstRevision: 1 };
  const retry = { ...run('retry', 2), StartedAt: '2026-09-27T00:00:03Z', FirstRevision: 4 };
  const next = { ...run('next'), ExecutionID: 'next-execution', EventID: 'next-event', FirstRevision: 7 };
  const entries = [
    entry('first', step(1, 'run.start', '', first.StartedAt, { Revision: 1 })),
    entry('first', step(2, 'model.start', 'same-operation', first.StartedAt, { Revision: 2 })),
    entry('first', step(3, 'model.complete', 'same-operation', '2026-09-27T00:00:01Z', { Revision: 3, State: 'failed' })),
    entry('retry', step(1, 'run.start', '', retry.StartedAt, { Revision: 4 })),
    entry('retry', step(2, 'model.start', 'same-operation', retry.StartedAt, { Revision: 5 })),
    entry('retry', step(3, 'model.complete', 'same-operation', '2026-09-27T00:00:04Z', { Revision: 6 })),
  ];
  expect(trajectoryTurns([first, retry, next]).map(turn => turn.map(run => run.ID))).toEqual([['first', 'retry'], ['next']]);
  const records = projectTrajectory([first, retry], entries);
  const items = trajectoryAxisItems([first, retry], entries, records);
  expect(items.map(item => item.kind)).toEqual(['trigger', 'model', 'model']);
  expect(items[0]).toMatchObject({ id: 'run:first', state: 'succeeded', start: Date.parse(first.StartedAt), triggerRunIDs: ['first', 'retry'] });
  expect(records.map(record => record.state)).toEqual(['failed', 'succeeded']);
  expect(new Set(records.map(record => record.id)).size).toBe(2);
  const later = entries.filter(entry => entry.RunID === 'retry');
  const paged = trajectoryAxisItems([first, retry], later, projectTrajectory([first, retry], later));
  expect(paged[0]).toEqual(items[0]);
  const selected = { mode: 'event' as const, first: records[1].id, last: records[1].id };
  expect(records.filter(trajectoryAxisFilter(items, selected)!.matches)).toEqual([records[1]]);
  const trigger = trajectoryAxisFilter(items, { mode: 'event', first: items[0].id, last: items[0].id })!;
  expect([...trigger.triggerRuns]).toEqual(['first', 'retry']);
  expect(projectTrajectory([first, retry], entries, true).filter(trigger.matches).map(record => record.run.ID)).toEqual(['first', 'retry']);
  expect(trajectoryTurns([{ ...first, ExecutionID: '' }, { ...retry, ExecutionID: '' }, { ...first, ID: 'foreign', AssistantID: 'other' }])).toHaveLength(3);
});

const summaryCopy = {
  messages: (n: number) => `读取了 ${n} 条聊天记录`, memories: (n: number) => `读取了 ${n} 条记忆`,
  plans: (n: number) => `列出了 ${n} 个计划`, attachments: (n: number) => `${n} 个附件`,
  readMessages: '读取聊天记录', readMemories: '读取记忆', listPlans: '列出计划', listPermissions: '查看权限与可申请资源',
};

test('tool summaries use actual results, keep unknown counts unknown, and handle missing starts', () => {
  const project = (steps: DebugStep[]) => projectTrajectory([run('summary')], steps.map(s => entry('summary', s)))[0];
  const started = step(1, 'tool.start', 'read', '2026-09-27T00:00:00Z', { Name: 'conversation_read', Preview: '{"limit":50,"conversation_id":"opaque"}' });
  const completed = step(2, 'tool.complete', 'read', '2026-09-27T00:00:01Z', { Name: 'conversation_read', Summary: { Kind: 'messages', Count: 2 } });
  expect(recordPreview(project([started, completed]), summaryCopy)).toBe('读取了 2 条聊天记录');
  expect(recordPreview(project([started]), summaryCopy)).toBe('读取聊天记录');
  expect(recordPreview(project([completed]), summaryCopy)).toBe('读取了 2 条聊天记录');
  expect(recordPreview(project([started, { ...completed, Summary: { Kind: 'messages', Count: 0 } }]), summaryCopy)).toBe('读取了 0 条聊天记录');
  expect(recordPreview(project([started, { ...completed, Summary: undefined }]), summaryCopy)).toBe('读取聊天记录');
  expect(recordPreview(project([started, { ...completed, State: 'failed' }]), summaryCopy)).toBe('读取聊天记录');
});

test('tool summaries prefer readable content and keep unknown tools inspectable', () => {
  const project = (extra: Partial<DebugStep>) => projectTrajectory([run('summary')], [entry('summary', step(1, 'tool.start', 'tool', '2026-09-27T00:00:00Z', extra))])[0];
  expect(recordPreview(project({ Name: 'conversation_publish', Preview: '{"conversation_id":"opaque","text":"truncated…', Summary: { Kind: 'text', Text: '完整消息摘要…' } }), summaryCopy)).toBe('完整消息摘要…');
  expect(recordPreview(project({ Name: 'conversation_publish', Summary: { Kind: 'attachments', Count: 2 } }), summaryCopy)).toBe('2 个附件');
  expect(recordPreview(project({ Name: 'plugin_tool', Preview: '{"custom":true}' }), summaryCopy)).toBe('{"custom":true}');
  expect(recordPreview(project({ Name: 'file_read', DataStatus: 'too_large' }), summaryCopy)).toBe('');
});


test('summary deltas preserve retained attempts, order and idle identity; resets remove expired runs', () => {
  const first = { ...run('first'), FirstRevision: 1 }, second = { ...run('second'), FirstRevision: 5 };
  const before = [first, second];
  expect(mergeRuns(before, [], false)).toBe(before);
  const changed = { ...first, State: 'interrupted' };
  const third = { ...run('third'), FirstRevision: 10 };
  const merged = mergeRuns(before, [third, changed], false);
  expect(merged).toEqual([changed, second, third]); expect(merged[1]).toBe(second);
  expect(mergeRuns(merged, [third], true)).toEqual([third]);
  expect(mergeRuns(merged, [], true)).toEqual([]);
  expect(before).toEqual([first, second]);
});
