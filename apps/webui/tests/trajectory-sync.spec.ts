import { test, expect } from '@playwright/test';
import type { DebugRun, TrajectoryEntry } from '../src/api/types';

const run = (id: string, revision: number, state = 'running'): DebugRun => ({ ID: id, AssistantID: 'assistant', EventID: id, ExecutionID: id, Trigger: 'user_message', Attempt: 1, State: state, Reason: '', StartedAt: '2026-01-01T00:00:00Z', FinishedAt: null, Incomplete: false, Steps: null, FirstRevision: revision });
const entry = (id: string, sequence: number, revision: number): TrajectoryEntry => ({ RunID: id, Step: { Sequence: sequence, Revision: revision, Kind: 'run.start', OperationID: '', At: '2026-01-01T00:00:00Z', State: 'started', DataStatus: 'captured' } });

test('trajectory sync retains deltas through reconnect, older paging, cleanup and authorization changes', async ({ page }) => {
  await page.addInitScript(() => { window.EventSource = class extends EventTarget { onopen = null; onerror = null; readyState = 1; close() {} } as unknown as typeof EventSource; });
  let stage = 'initial'; const requests: URLSearchParams[] = [];
  await page.route('**/api/debug/trajectory?*', async route => {
    const query = new URL(route.request().url()).searchParams; requests.push(query);
    const base = { Items: [] as TrajectoryEntry[], Runs: [] as DebugRun[], RunsReset: false, RunsCursor: 'runs-1', HeadCursor: 'head-2', NextCursor: '', ContextResetAt: null, Cleanup: null };
    if (stage === 'denied') { await route.fulfill({ status: 403, json: { code: 'forbidden', message: 'revoked' } }); return; }
    if (query.get('assistant_id') === 'other') {
      expect(query.get('runs_cursor')).toBeNull(); expect(query.get('after')).toBeNull();
      await route.fulfill({ json: { ...base, RunsReset: true, Runs: [run('other', 1)], Items: [entry('other', 1, 1)] } }); return;
    }
    if (!query.has('runs_cursor')) {
      await route.fulfill({ json: { ...base, RunsReset: true, Runs: [run('a', 1), run('b', 2)], Items: [entry('b', 1, 2)], NextCursor: 'older-1' } }); return;
    }
    if (query.has('cursor')) {
      expect(query.get('runs_cursor')).toBe('runs-2');
      await route.fulfill({ json: { ...base, RunsCursor: 'runs-2', Items: [entry('a', 1, 1)], HeadCursor: 'head-1' } }); return;
    }
    if (stage === 'backfill') {
      const first = query.get('after') === 'head-2';
      expect(query.get('runs_cursor')).toBe(first ? 'runs-1' : 'runs-2');
      await route.fulfill({ json: { ...base, RunsCursor: 'runs-2', Runs: first ? [run('b', 2, 'succeeded'), run('c', 3)] : [], Items: [entry('c', first ? 1 : 2, first ? 3 : 4)], NextCursor: first ? 'head-3' : '', HeadCursor: first ? 'head-3' : 'head-4' } }); return;
    }
    if (stage === 'cleanup') {
      expect(query.get('after')).toBe('head-4');
      await route.fulfill({ json: { ...base, RunsCursor: 'runs-3', HeadCursor: 'head-4', RunsReset: true, Runs: [run('c', 3, 'succeeded')] } }); return;
    }
    await route.fulfill({ json: base });
  });
  await page.goto('/tests/fixtures/trajectory-sync.html');
  const snapshot = async () => JSON.parse((await page.getByTestId('snapshot').textContent())!);
  await expect.poll(async () => (await snapshot()).loading).toBe(false);
  expect((await snapshot()).runs).toEqual([['a', 'running'], ['b', 'running']]);
  const projections = (await snapshot()).projection, before = requests.length;
  stage = 'idle'; await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect.poll(() => requests.length).toBeGreaterThan(before);
  expect((await snapshot()).projection).toBe(projections);
  stage = 'backfill'; await page.getByRole('button', { name: 'Reconnect', exact: true }).click();
  await expect.poll(async () => (await snapshot()).entries).toEqual(['b:1', 'c:1', 'c:2']);
  expect((await snapshot()).runs).toEqual([['a', 'running'], ['b', 'succeeded'], ['c', 'running']]);
  await page.getByRole('button', { name: 'Older', exact: true }).click();
  await expect.poll(async () => (await snapshot()).entries).toEqual(['a:1', 'b:1', 'c:1', 'c:2']);
  stage = 'cleanup'; await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect.poll(async () => (await snapshot()).entries).toEqual(['c:1', 'c:2']);
  expect((await snapshot()).runs).toEqual([['c', 'succeeded']]);
  stage = 'denied'; await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect.poll(async () => (await snapshot()).error).toBe(true);
  expect((await snapshot()).runs).toEqual([]); expect((await snapshot()).entries).toEqual([]);
  stage = 'other'; await page.getByRole('button', { name: 'Other assistant', exact: true }).click();
  await expect.poll(async () => (await snapshot()).entries).toEqual(['other:1']);
});
