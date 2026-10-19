import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

async function moduleURL(file, replacements = []) {
  let source = await readFile(new URL(file, import.meta.url), 'utf8');
  for (const [from, to] of replacements) source = source.replace(from, to);
  const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
  return `data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`;
}
const clientURL = await moduleURL('../src/client.ts');
const { api, onSessionExpired, clearClientState } = await import(clientURL);
const { subscribeRuntime } = await import(await moduleURL('../src/events.ts', [["'./client'", JSON.stringify(clientURL)]]));
const flush = () => new Promise(resolve => setImmediate(resolve));
const unauthorized = () => Response.json({ code: 'unauthenticated' }, { status: 401 });

async function fixture(fetch, check) {
  const names = ['window', 'document', 'navigator', 'fetch', 'EventSource'];
  const saved = Object.fromEntries(names.map(name => [name, Object.getOwnPropertyDescriptor(globalThis, name)]));
  let current, subscription, expired = 0;
  const states = [], calls = [], sources = [], restores = [];
  class FakeEventSource extends EventTarget {
    static CONNECTING = 0; static OPEN = 1; static CLOSED = 2;
    readyState = FakeEventSource.OPEN;
    constructor(url) { super(); current = this; this.url = url; sources.push(this); }
    close() { this.readyState = FakeEventSource.CLOSED; }
  }
  const window = Object.assign(new EventTarget(), { location: { protocol: 'http:', port: '80' } });
  const values = { window, document: { cookie: 'molr_dev_csrf_p80=synthetic-current-session' }, navigator: { onLine: true }, fetch: (...args) => { calls.push(args); return fetch(...args); }, EventSource: FakeEventSource };
  for (const [key, value] of Object.entries(values)) Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
  clearClientState();
  const realFetch = globalThis.fetch;
  globalThis.fetch = async () => Response.json({}, { headers: { 'X-Molr-CSRF-Cookie': 'molr_dev_csrf_p80' } });
  await api.get('/api/auth/me');
  globalThis.fetch = realFetch;
  const unsubscribe = onSessionExpired(() => expired++);
  try {
    subscription = subscribeRuntime({ onUpdate() {}, onRestore() { restores.push(true); }, onConnection(state) { states.push(state); } });
    current.onopen();
    await check({
      subscription, calls, states, sources, restores, ready(cursor) { current.dispatchEvent(new MessageEvent('ready', { data: '{}', lastEventId: cursor })); }, expired: () => expired,
      fail(terminal = true) { current.readyState = terminal ? FakeEventSource.CLOSED : FakeEventSource.CONNECTING; current.onerror(); },
      open() { current.readyState = FakeEventSource.OPEN; current.onopen(); },
    });
  } finally {
    subscription.dispose(); unsubscribe(); clearClientState();
    for (const [name, descriptor] of Object.entries(saved)) {
      if (descriptor) Object.defineProperty(globalThis, name, descriptor);
      else delete globalThis[name];
    }
  }
}

test('SSE session verification immediately expires a revoked current session', () => fixture(async () => unauthorized(), async f => {
  f.fail(); await flush();
  assert.equal(f.expired(), 1);
  assert.equal(f.calls[0][0], '/api/auth/me');
}));

for (const status of [200, 403, 429, 503]) {
  test(`SSE HTTP ${status} session check does not expire the session`, () => fixture(async () => Response.json({}, { status }), async f => {
    f.fail(); await flush();
    assert.equal(f.expired(), 0);
    assert.equal(f.states.at(-1), 'failed');
  }));
}
test('network failure retains connection recovery without expiring the session', () => fixture(async () => { throw new TypeError('synthetic network failure'); }, async f => {
  f.fail(); await flush(); assert.equal(f.expired(), 0);
}));

test('ordinary session restoration still handles its own 401', () => fixture(async () => unauthorized(), async f => {
  await assert.rejects(api.get('/api/auth/me'), error => error.status === 401);
  assert.equal(f.expired(), 0);
}));

test('repeated SSE errors share the pending session check', async () => {
  let resolve;
  await fixture(() => new Promise(done => { resolve = done; }), async f => {
    f.fail(false); f.fail(false); f.fail(false);
    assert.equal(f.calls.length, 1);
    resolve(unauthorized()); await flush();
    assert.equal(f.expired(), 1);
  });
});

for (const cause of ['dispose', 'recovery', 'retry', 'offline', 'identity-change']) {
  test(`${cause} prevents a late verification from expiring the session`, async () => {
    let resolve;
    // Deliberately ignore AbortSignal to verify the client's response guard too.
    await fixture(() => new Promise(done => { resolve = done; }), async f => {
      f.fail(false);
      if (cause === 'dispose') f.subscription.dispose();
      if (cause === 'recovery') f.open();
      if (cause === 'retry') f.subscription.retry();
      if (cause === 'offline') window.dispatchEvent(new Event('offline'));
      if (cause === 'identity-change') document.cookie = 'molr_dev_csrf_p80=synthetic-new-session';
      else assert.equal(f.calls[0][1].signal.aborted, true);
      resolve(unauthorized()); await flush();
      assert.equal(f.expired(), 0);
    });
  });
}

test('an obsolete check cannot clear the newer pending verification', async () => {
  const replies = [];
  await fixture(() => new Promise(resolve => replies.push(resolve)), async f => {
    f.fail(false);
    f.subscription.retry();
    f.fail(false);
    assert.equal(f.calls.length, 2);
    replies[0](unauthorized()); await flush();
    assert.equal(f.expired(), 0);
    f.fail(false);
    assert.equal(f.calls.length, 2);
    replies[1](unauthorized()); await flush();
    assert.equal(f.expired(), 1);
  });
});


test('ready refreshes the initial snapshot and preserves its cursor before any live update', () => fixture(async () => Response.json({}), async f => {
  assert.equal(f.restores.length, 0);
  f.ready('initial-position');
  assert.equal(f.restores.length, 1);
  const stale = f.sources.at(-1);
  f.subscription.retry();
  assert.equal(f.sources.at(-1).url, '/api/events/stream?after=initial-position');
  stale.dispatchEvent(new MessageEvent('ready', { lastEventId: 'stale' }));
  assert.equal(f.restores.length, 1);
  f.open(); f.ready('initial-position');
  assert.equal(f.restores.length, 2);
  f.sources.at(-1).dispatchEvent(new MessageEvent('update', { data: '{}', lastEventId: 'delivered-position' }));
  f.subscription.retry();
  assert.equal(f.sources.at(-1).url, '/api/events/stream?after=delivered-position');
}));
