import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { describe, test } from 'node:test';
import ts from 'typescript';

async function moduleURL(file, replacements = []) {
  let source = await readFile(new URL(file, import.meta.url), 'utf8');
  for (const [from, to] of replacements) source = source.replace(from, to);
  const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
  return `data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`;
}
const clientURL = await moduleURL('../src/client.ts');
const defaultClient = await import(clientURL);
const { createClient } = defaultClient;
const { subscribeRuntime } = await import(await moduleURL('../src/events.ts', [["'./client'", JSON.stringify(clientURL)]]));
const flush = () => new Promise(resolve => setImmediate(resolve));
const unauthorized = () => Response.json({ code: 'unauthenticated' }, { status: 401 });

for (const scope of ['default client', 'custom client']) describe(scope, () => {
const client = scope === 'default client' ? defaultClient : createClient();
const other = scope === 'default client' ? createClient() : defaultClient;
const { api, onSessionExpired, clearClientState } = client;

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
  const document = Object.assign(new EventTarget(), { cookie: 'molr_dev_csrf_p80=synthetic-current-session', visibilityState: 'visible' });
  const values = { window, document, navigator: { onLine: true }, fetch: (...args) => { calls.push(args); return fetch(...args); }, EventSource: FakeEventSource };
  for (const [key, value] of Object.entries(values)) Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
  clearClientState();
  const realFetch = globalThis.fetch;
  globalThis.fetch = async () => Response.json({}, { headers: { 'X-Molr-CSRF-Cookie': 'molr_dev_csrf_p80' } });
  await api.get('/api/auth/me');
  await other.api.get('/api/auth/me');
  globalThis.fetch = realFetch;
  const unsubscribe = onSessionExpired(() => expired++);
  try {
    subscription = subscribeRuntime({ ...(scope === 'custom client' ? { session: client } : {}), onUpdate() {}, onRestore() { restores.push(true); }, onConnection(state) { states.push(state); } });
    current.onopen?.();
    await check({
      subscription, calls, states, sources, restores, ready(cursor) { current.dispatchEvent(new MessageEvent('ready', { data: '{}', lastEventId: cursor })); }, expired: () => expired,
      fail(terminal = true) { current.readyState = terminal ? FakeEventSource.CLOSED : FakeEventSource.CONNECTING; current.onerror(); },
      open() { current.readyState = FakeEventSource.OPEN; current.dispatchEvent(new MessageEvent('ready', { data: '{}' })); },
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
  f.ready('initial-position');
  assert.equal(f.restores.length, 2);
  f.sources.at(-1).dispatchEvent(new MessageEvent('update', { data: '{}', lastEventId: 'delivered-position' }));
  f.subscription.retry();
  assert.equal(f.sources.at(-1).url, '/api/events/stream?after=delivered-position');
}));


test('an open transport stays reconnecting until ready, and a stalled handshake retries automatically', async t => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  await fixture(async () => Response.json({}), async f => {
    assert.equal(f.states.at(-1), 'reconnecting');
    t.mock.timers.tick(20_000);
    assert.equal(f.states.at(-1), 'failed');
    const stale = f.sources[0];
    t.mock.timers.tick(5500);
    assert.equal(f.sources.length, 2);
    assert.equal(stale.readyState, EventSource.CLOSED);
    stale.dispatchEvent(new MessageEvent('ready', { lastEventId: 'obsolete' }));
    assert.equal(f.states.at(-1), 'reconnecting');
    f.ready('resumed');
    assert.equal(f.states.at(-1), 'recovered');
    assert.equal(f.restores.length, 1);
    t.mock.timers.tick(2400);
    assert.equal(f.states.at(-1), 'connected');
  });
});

test('terminal failures keep retrying with capped backoff and reset after ready', async t => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  t.mock.method(Math, 'random', () => 0.5);
  await fixture(async () => Response.json({}), async f => {
    for (const delay of [5000, 10_000, 20_000, 30_000, 30_000]) {
      const count = f.sources.length;
      f.fail(); await flush(); // Successful auth checks must not accelerate backoff.
      t.mock.timers.tick(delay - 1);
      assert.equal(f.sources.length, count);
      t.mock.timers.tick(1);
      assert.equal(f.sources.length, count + 1);
    }
    f.ready('healthy'); f.fail(); await flush();
    const count = f.sources.length;
    t.mock.timers.tick(5000);
    assert.equal(f.sources.length, count + 1);
  });
});

for (const cause of ['get', 'post', 'focus', 'visible', 'online']) {
  test(`${cause} wakes failed SSE without replacing an ongoing attempt or claiming recovery`, async t => {
    t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
    await fixture(async () => Response.json({}), async f => {
      f.ready('before-outage');
      t.mock.timers.tick(6000);
      f.fail(); await flush();
      const wake = async () => {
        if (cause === 'get') await api.get('/api/assistants');
        else if (cause === 'post') await api.post('/api/conversations/messages', {});
        else if (cause === 'visible') document.dispatchEvent(new Event('visibilitychange'));
        else window.dispatchEvent(new Event(cause));
      };
      await Promise.all([wake(), wake(), wake()]);
      assert.equal(f.sources.length, 2);
      assert.equal(f.sources.at(-1).url, '/api/events/stream?after=before-outage');
      assert.equal(f.states.at(-1), 'reconnecting');
      f.fail(); await flush();
      await Promise.all([wake(), wake(), wake()]);
      t.mock.timers.tick(4999);
      assert.equal(f.sources.length, 2, 'Repeated signals share a throttled retry');
      t.mock.timers.tick(1);
      assert.equal(f.sources.length, 3);
      f.ready('after-outage');
      assert.equal(f.states.at(-1), 'recovered');
      await wake();
      assert.equal(f.sources.length, 3, 'Healthy streams are preserved');
    });
  });
}

test('offline pauses retries and returning online reconnects without losing the cursor', async t => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  await fixture(async () => Response.json({}), async f => {
    f.ready('saved'); f.fail(); await flush();
    navigator.onLine = false;
    window.dispatchEvent(new Event('offline'));
    t.mock.timers.tick(60_000);
    window.dispatchEvent(new Event('focus'));
    await api.get('/api/assistants');
    assert.equal(f.sources.length, 1);
    assert.equal(f.states.at(-1), 'disconnected');
    navigator.onLine = true;
    window.dispatchEvent(new Event('online'));
    assert.equal(f.sources.length, 2);
    assert.equal(f.sources.at(-1).url, '/api/events/stream?after=saved');
  });
});

for (const cause of ['dispose', 'expired']) {
  test(`${cause} stops scheduled retries and removes all wake listeners`, async t => {
    t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
    await fixture(async () => cause === 'expired' ? unauthorized() : Response.json({}), async f => {
      f.fail(); await flush();
      if (cause === 'dispose') f.subscription.dispose();
      t.mock.timers.tick(60_000);
      window.dispatchEvent(new Event('online')); window.dispatchEvent(new Event('focus'));
      document.dispatchEvent(new Event('visibilitychange'));
      globalThis.fetch = async () => Response.json({});
      await api.get('/api/assistants');
      f.subscription.retry();
      assert.equal(f.sources.length, 1);
      assert.equal(f.sources[0].readyState, EventSource.CLOSED);
    });
  });
}

test('native transient retries retain one stream and ready cancels the attempt deadline', async t => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  await fixture(async () => Response.json({}), async f => {
    f.ready('initial');
    assert.equal(f.states.at(-1), 'connected');
    f.fail(false); await flush();
    await api.get('/api/assistants');
    window.dispatchEvent(new Event('online'));
    window.dispatchEvent(new Event('focus'));
    t.mock.timers.tick(19_000);
    assert.equal(f.sources.length, 1);
    assert.equal(f.states.at(-1), 'reconnecting');
    f.ready('resumed');
    assert.equal(f.states.at(-1), 'recovered');
    t.mock.timers.tick(60_000);
    assert.equal(f.states.at(-1), 'connected');
    assert.equal(f.sources.length, 1);
    assert.equal(f.restores.length, 2);
  });
});


test('only the owning client can wake or expire its subscription', async t => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  await fixture(async () => Response.json({}), async f => {
    f.ready('owned-cursor');
    t.mock.timers.tick(6000);
    f.fail(); await flush();
    await other.api.get('/api/assistants');
    assert.equal(f.sources.length, 1, 'Other clients cannot wake this subscription');
    await api.get('/api/assistants');
    assert.equal(f.sources.length, 2);
    assert.equal(f.sources[1].url, '/api/events/stream?after=owned-cursor');
    f.ready('owned-cursor');
    globalThis.fetch = async () => unauthorized();
    await assert.rejects(other.api.get('/api/assistants'), error => error.status === 401);
    assert.equal(f.sources[1].readyState, EventSource.OPEN, 'Other clients cannot dispose this subscription');
    assert.equal(f.expired(), 0);
    await assert.rejects(api.get('/api/assistants'), error => error.status === 401);
    assert.equal(f.sources[1].readyState, EventSource.CLOSED);
    assert.equal(f.expired(), 1);
    t.mock.timers.tick(60_000);
    assert.equal(f.sources.length, 2);
  });
});
});
