import { test, expect } from '@playwright/test';
import { QueryStore } from '../src/query-store';
import { APIError } from '@molr/sdk';
import { shareJSON } from '../src/share-json';

function harness() {
  const queries = new QueryStore();
  const requests: { signal: AbortSignal; resolve: (data: unknown) => void; reject: (error: unknown) => void }[] = [];
  const load = (signal: AbortSignal) => new Promise((resolve, reject) => requests.push({ signal, resolve, reject }));
  return { queries, requests, load };
}

test('unchanged query data retains identity while loading and errors remain observable', async () => {
  const { queries, requests, load } = harness();
  const release = queries.subscribe('resources', () => {});
  const original = [{ ID: 'one', Version: 1, State: 'ready' }, { ID: 'two', Version: 1, State: 'ready' }];
  try {
    queries.refresh('resources', 0, load);
    requests[0].resolve(original);
    await expect.poll(() => queries.snapshot('resources').loading).toBe(false);
    const before = queries.snapshot('resources').data;
    queries.refresh('resources', 1, load);
    expect(queries.snapshot('resources').loading).toBe(true);
    await expect.poll(() => requests.length).toBe(2);
    requests[1].resolve(structuredClone(original));
    await expect.poll(() => queries.snapshot('resources').loading).toBe(false);
    expect(queries.snapshot('resources').data).toBe(before);
    queries.refresh('resources', 2, load);
    await expect.poll(() => requests.length).toBe(3);
    requests[2].resolve([{ ...original[0], State: 'offline' }, original[1]]);
    await expect.poll(() => queries.snapshot('resources').loading).toBe(false);
    const changed = queries.snapshot<typeof original>('resources').data!;
    expect(changed).not.toBe(before);
    expect(changed[0].State).toBe('offline'); // Version alone does not describe live state.
    expect(changed[1]).toBe(original[1]);
  } finally { release(); queries.clear(); }
});

test('JSON sharing preserves removal, null, arrays and own property names', () => {
  const previous = { value: { stable: 1 }, removed: true };
  const next = shareJSON(previous, { value: { stable: 1 } }) as typeof previous;
  expect(next).not.toBe(previous);
  expect(next.value).toBe(previous.value);
  expect(Object.hasOwn(next, 'removed')).toBe(false);
  expect(shareJSON([1, 2], [1])).toEqual([1]);
  expect(shareJSON({ value: 1 }, null)).toBeNull();
  const special = JSON.parse('{"__proto__":{"safe":true},"constructor":2}');
  expect(shareJSON({}, special)).toEqual(special);
  expect(Object.getPrototypeOf(shareJSON({}, special))).toBe(Object.prototype);
});

test('queries share in-flight and completed revisions across subscribers', async () => {
  const { queries, requests, load } = harness();
  const first = queries.subscribe('settings', () => {});
  queries.refresh('settings', 0, load);
  const second = queries.subscribe('settings', () => {});
  queries.refresh('settings', 0, load);
  expect(requests).toHaveLength(1);
  first(); await Promise.resolve();
  expect(requests[0].signal.aborted).toBe(false);
  requests[0].resolve({ Theme: 'dark' }); await Promise.resolve();
  const third = queries.subscribe('settings', () => {});
  queries.refresh('settings', 0, load);
  expect(requests).toHaveLength(1);
  expect(queries.snapshot('settings').data).toEqual({ Theme: 'dark' });
  second(); third(); await Promise.resolve();
  const fourth = queries.subscribe('settings', () => {});
  queries.refresh('settings', 0, load);
  expect(requests).toHaveLength(2);
  fourth(); queries.clear();
});

test('continuous invalidation delivers responses and coalesces follow-up reads', async () => {
  const { queries, requests, load } = harness();
  const release = queries.subscribe('status', () => {});
  queries.refresh('status', 0, load);
  for (let revision = 1; revision <= 10; revision++) queries.refresh('status', revision, load);
  expect(requests).toHaveLength(1);
  expect(requests[0].signal.aborted).toBe(false);
  requests[0].resolve('first'); await Promise.resolve();
  expect(queries.snapshot('status').data).toBe('first');
  await expect.poll(() => requests.length).toBe(2);
  requests[1].resolve('current'); await Promise.resolve();
  queries.refresh('status', 10, load);
  expect(requests).toHaveLength(2);
  expect(queries.snapshot('status').data).toBe('current');
  release(); queries.clear();
});

test('last unsubscribe cancels, stale responses cannot replace data, and session clear discards snapshots', async () => {
  const { queries, requests, load } = harness();
  let release = queries.subscribe('chat', () => {});
  queries.refresh('chat', 0, load);
  release(); await Promise.resolve();
  expect(requests[0].signal.aborted).toBe(true);
  release = queries.subscribe('chat', () => {});
  queries.refresh('chat', 0, load);
  requests[1].resolve('new'); await Promise.resolve();
  requests[0].resolve('stale'); await Promise.resolve();
  expect(queries.snapshot('chat').data).toBe('new');
  queries.refresh('chat', 1, load);
  queries.clear(); requests[2].resolve('old-account'); await Promise.resolve();
  expect(queries.snapshot('chat').data).toBeUndefined();
  release(); queries.clear();
});

test('refresh failure preserves data, but access denial removes it for every subscriber', async () => {
  const { queries, requests, load } = harness();
  const release = queries.subscribe('item', () => {});
  queries.refresh('item', 0, load);
  requests[0].resolve('private'); await Promise.resolve();
  queries.refresh('item', 1, load);
  await expect.poll(() => requests.length).toBe(2);
  requests[1].reject(new APIError('unavailable', '', 503)); await Promise.resolve();
  expect(queries.snapshot('item').data).toBe('private');
  queries.refresh('item', 2, load);
  expect(requests).toHaveLength(2); // Invalidation must respect the transient-error cooldown.
  await expect.poll(() => requests.length).toBe(3);
  requests[2].reject(new APIError('forbidden', '', 403)); await Promise.resolve();
  expect(queries.snapshot('item').data).toBeUndefined();
  release(); queries.clear();
});

test('snapshot reads without subscriptions do not evict retained query data', async () => {
  const { queries, requests, load } = harness();
  try {
    const release = queries.subscribe('retained', () => {});
    queries.refresh('retained', 0, load);
    requests[0].resolve({ value: 'retained result' });
    await Promise.resolve();
    expect(queries.snapshot('retained').data).toEqual({ value: 'retained result' });
    release(); await Promise.resolve();
    // React can read snapshots during renders that never commit a subscription.
    const empty = queries.snapshot(null);
    for (let index = 0; index < 1000; index++) {
      expect(queries.snapshot(`uncommitted-${index}`)).toBe(empty);
    }
    const unrelated = queries.subscribe('another-page', () => {});
    unrelated(); await Promise.resolve();
    expect(queries.snapshot('retained').data).toEqual({ value: 'retained result' });
    expect(requests).toHaveLength(1);
  } finally { queries.clear(); }
});

test('manual server refresh applies once and does not turn later requests into bypasses', async () => {
  const queries = new QueryStore();
  const flags: boolean[] = [];
  const release = queries.subscribe('cached', () => {});
  const load = async (_signal: AbortSignal, refresh = false) => { flags.push(refresh); return flags.length; };
  try {
    queries.refresh('cached', 0, load);
    await expect.poll(() => queries.snapshot('cached').data).toBe(1);
    queries.refresh('cached', 0, load, true);
    await expect.poll(() => queries.snapshot('cached').data).toBe(2);
    queries.refresh('cached', 1, load);
    await expect.poll(() => queries.snapshot('cached').data).toBe(3);
    expect(flags).toEqual([false, true, false]);
  } finally { release(); queries.clear(); }
});
