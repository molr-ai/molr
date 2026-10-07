import { test, expect } from '@playwright/test';
import { TurnMetricsCache } from '../src/turn-metrics-cache';
import type { TurnMetrics } from '../src/api/types';

test('turn cache deduplicates requests, keeps only final snapshots without refetch and clears with session', async () => {
  const original = globalThis.fetch;
  const cache = new TurnMetricsCache();
  let calls = 0, resolve!: (response: Response) => void;
  globalThis.fetch = (() => { calls++; return new Promise<Response>(done => { resolve = done; }); }) as typeof fetch;
  const path = '/api/conversations/c/messages/m/metrics';
  const snapshot = { State: 'running', Finalized: false } as TurnMetrics;
  try {
    const first = cache.subscribe('turn', path, () => {}), second = cache.subscribe('turn', path, () => {});
    expect(calls).toBe(1);
    resolve(Response.json(snapshot)); await expect.poll(() => cache.snapshot('turn', path).data).toEqual(snapshot);
    first(); second();
    const third = cache.subscribe('turn', path, () => {});
    expect(calls).toBe(2);
    resolve(Response.json({ ...snapshot, State: 'done', Finalized: true }));
    await expect.poll(() => cache.snapshot('turn', path).data?.Finalized).toBe(true);
    third();
    const fourth = cache.subscribe('turn', path, () => {});
    await cache.refresh('turn', path); expect(calls).toBe(2); fourth();
    cache.clear(); cache.activate();
    expect(cache.snapshot('turn', path).data).toBeUndefined();
    const fifth = cache.subscribe('turn', path, () => {}); expect(calls).toBe(3); fifth();
  } finally { cache.clear(); globalThis.fetch = original; }
});

test('turn cache preserves old data on refresh failure and never applies aborted responses', async () => {
  const original = globalThis.fetch, cache = new TurnMetricsCache();
  let resolve!: (response: Response) => void;
  globalThis.fetch = (() => new Promise<Response>(done => { resolve = done; })) as typeof fetch;
  const path = '/api/conversations/c/messages/m/metrics';
  try {
    let release = cache.subscribe('turn', path, () => {});
    resolve(Response.json({ State: 'running', Finalized: false }));
    await expect.poll(() => cache.snapshot('turn', path).data?.State).toBe('running');
    release(); release = cache.subscribe('turn', path, () => {});
    resolve(Response.json({ code: 'unavailable' }, { status: 503 }));
    await expect.poll(() => !!cache.snapshot('turn', path).error).toBe(true);
    expect(cache.snapshot('turn', path).data?.State).toBe('running');
    release(); release = cache.subscribe('turn', path, () => {});
    const stale = resolve; release();
    release = cache.subscribe('turn', path, () => {});
    resolve(Response.json({ State: 'done', Finalized: true }));
    await expect.poll(() => cache.snapshot('turn', path).data?.Finalized).toBe(true);
    stale(Response.json({ State: 'running', Finalized: false }));
    await Promise.resolve(); await Promise.resolve();
    expect(cache.snapshot('turn', path).data?.Finalized).toBe(true);
    release();
  } finally { cache.clear(); globalThis.fetch = original; }
});
