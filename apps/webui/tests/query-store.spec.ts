import { test, expect } from '@playwright/test';
import { QueryStore } from '../src/query-store';
import { APIError } from '../src/api/client';

function harness() {
  const queries = new QueryStore();
  const requests: { signal: AbortSignal; resolve: (data: unknown) => void; reject: (error: unknown) => void }[] = [];
  const load = (signal: AbortSignal) => new Promise((resolve, reject) => requests.push({ signal, resolve, reject }));
  return { queries, requests, load };
}

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
  expect(requests).toHaveLength(2);
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
  requests[1].reject(new APIError('unavailable', '', 503)); await Promise.resolve();
  expect(queries.snapshot('item').data).toBe('private');
  queries.refresh('item', 2, load);
  requests[2].reject(new APIError('forbidden', '', 403)); await Promise.resolve();
  expect(queries.snapshot('item').data).toBeUndefined();
  release(); queries.clear();
});
