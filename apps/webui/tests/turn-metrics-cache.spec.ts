import { test, expect } from '@playwright/test';
import { TurnMetricsCache } from '../src/turn-metrics-cache';
import type { TurnMetrics } from '@molr/sdk';
import { clearClientState } from '@molr/sdk';

const globals = ['window', 'document'].map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)] as const);
test.beforeEach(() => {
  // The SDK reads browser CSRF identity even for GETs; exercise that boundary.
  Object.defineProperty(globalThis, 'window', { configurable: true, value: { location: { port: '5193', protocol: 'http:' } } });
  Object.defineProperty(globalThis, 'document', { configurable: true, value: { cookie: 'molr_dev_csrf_p5193=cache-test' } });
  clearClientState();
});
test.afterEach(() => {
  clearClientState();
  for (const [key, descriptor] of globals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else Reflect.deleteProperty(globalThis, key);
  }
});


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

test('browsing messages shares an empty snapshot without requests or retained placeholders', () => {
  const original = globalThis.fetch, cache = new TurnMetricsCache();
  let requests = 0;
  globalThis.fetch = (() => { requests++; throw new Error('snapshot read must not fetch'); }) as typeof fetch;
  try {
    const snapshots = new Set();
    for (let i = 0; i < 10000; i++) snapshots.add(cache.snapshot(`turn-${i}`, `/api/metrics/${i}`));
    expect(snapshots.size).toBe(1);
    expect(cache.snapshot('missing', '/api/metrics/missing')).toEqual({ loading: false });
    expect(requests).toBe(0);
    cache.clear();
    expect(cache.snapshot('after-logout', '/api/metrics/new')).toBe([...snapshots][0]);
  } finally { cache.clear(); globalThis.fetch = original; }
});

test('idle history is bounded and an evicted final result is fetched again on demand', async () => {
  const original = globalThis.fetch, cache = new TurnMetricsCache();
  let calls = 0;
  globalThis.fetch = (async () => { calls++; return Response.json({ State: 'done', Finalized: true }); }) as typeof fetch;
  try {
    for (let i = 0; i < 101; i++) await cache.refresh(`turn-${i}`, `/api/metrics/${i}`);
    expect(cache.snapshot('turn-0', '/api/metrics/0').data).toBeUndefined();
    expect(cache.snapshot('turn-100', '/api/metrics/100').data?.Finalized).toBe(true);
    const retained = cache.subscribe('turn-100', '/api/metrics/100', () => {}); retained();
    expect(calls).toBe(101);
    const reopened = cache.subscribe('turn-0', '/api/metrics/0', () => {});
    await expect.poll(() => cache.snapshot('turn-0', '/api/metrics/0').data?.Finalized).toBe(true);
    expect(calls).toBe(102); reopened();
    expect(cache.snapshot('turn-1', '/api/metrics/1').data).toBeUndefined();
  } finally { cache.clear(); globalThis.fetch = original; }
});

test('active viewers and pending requests survive idle cache pressure', async () => {
  const original = globalThis.fetch, cache = new TurnMetricsCache();
  let pendingCalls = 0, resolve!: (response: Response) => void;
  globalThis.fetch = ((input: RequestInfo | URL) => {
    if (String(input).endsWith('/pending')) { pendingCalls++; return new Promise<Response>(done => { resolve = done; }); }
    return Promise.resolve(Response.json({ State: 'done', Finalized: true }));
  }) as typeof fetch;
  const release = cache.subscribe('active', '/api/metrics/pending', () => {});
  try {
    for (let i = 0; i < 110; i++) await cache.refresh(`idle-${i}`, `/api/metrics/${i}`);
    expect(cache.snapshot('active', '/api/metrics/pending').loading).toBe(true);
    const second = cache.subscribe('active', '/api/metrics/pending', () => {});
    expect(pendingCalls).toBe(1);
    resolve(Response.json({ State: 'done', Finalized: true }));
    await expect.poll(() => cache.snapshot('active', '/api/metrics/pending').data?.Finalized).toBe(true);
    release(); second();
    await cache.refresh('newest', '/api/metrics/newest');
    expect(cache.snapshot('active', '/api/metrics/pending').data?.Finalized).toBe(true);
    expect(pendingCalls).toBe(1);
  } finally { release(); cache.clear(); globalThis.fetch = original; }
});

test('active entries may exceed the idle budget and are reclaimed after release', async () => {
  const original = globalThis.fetch, cache = new TurnMetricsCache(), releases: (() => void)[] = [];
  globalThis.fetch = (async () => Response.json({ State: 'done', Finalized: true })) as typeof fetch;
  try {
    for (let i = 0; i < 102; i++) releases.push(cache.subscribe(`active-${i}`, `/api/metrics/${i}`, () => {}));
    await expect.poll(() => cache.snapshot('active-101', '/api/metrics/101').data?.Finalized).toBe(true);
    for (let i = 0; i < 102; i++) expect(cache.snapshot(`active-${i}`, `/api/metrics/${i}`).data?.Finalized).toBe(true);
    releases.forEach(release => release());
    expect(cache.snapshot('active-0', '/api/metrics/0').data).toBeUndefined();
    expect(cache.snapshot('active-101', '/api/metrics/101').data?.Finalized).toBe(true);
  } finally { releases.forEach(release => release()); cache.clear(); globalThis.fetch = original; }
});

test('activation is finite and old cleanup cannot remove a new session request', async () => {
  const original = globalThis.fetch, cache = new TurnMetricsCache(), responses: ((response: Response) => void)[] = [];
  globalThis.fetch = (() => new Promise<Response>(resolve => { responses.push(resolve); })) as typeof fetch;
  const old = cache.subscribe('turn', '/api/metrics/old', () => {});
  try {
    cache.clear();
    const current = cache.subscribe('turn', '/api/metrics/current', () => {});
    expect(responses).toHaveLength(1);
    cache.activate(); expect(responses).toHaveLength(2);
    old();
    expect(cache.snapshot('turn', '/api/metrics/current').loading).toBe(true);
    responses[1](Response.json({ State: 'done', Finalized: true }));
    await expect.poll(() => cache.snapshot('turn', '/api/metrics/current').data?.Finalized).toBe(true);
    responses[0](Response.json({ State: 'running', Finalized: false }));
    await Promise.resolve(); await Promise.resolve();
    expect(cache.snapshot('turn', '/api/metrics/current').data?.Finalized).toBe(true);
    current(); cache.clear();
    await cache.refresh('logged-out', '/api/metrics/ignored');
    expect(responses).toHaveLength(2);
  } finally { old(); cache.clear(); globalThis.fetch = original; }
});
