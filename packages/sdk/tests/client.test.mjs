import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import ts from 'typescript';

// Compile the browser client without bringing a browser or framework into the SDK.
const source = await readFile(new URL('../src/client.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
const { createClient, APIError } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

async function withBrowser(callback) {
  const saved = Object.fromEntries(['window', 'document', 'fetch'].map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));
  Object.defineProperty(globalThis, 'window', { configurable: true, value: { location: { port: '80', protocol: 'http:' } } });
  Object.defineProperty(globalThis, 'document', { configurable: true, value: { cookie: '' } });
  try { await callback(); }
  finally {
    for (const [key, descriptor] of Object.entries(saved)) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else delete globalThis[key];
    }
  }
}

for (const method of ['get', 'read', 'download']) {
  for (const status of [200, 401, 429, 503]) {
    test(`${method} preserves cancellation while consuming an HTTP ${status} response body`, async () => withBrowser(async () => {
      const controller = new AbortController();
      let bodyStarted;
      const started = new Promise(resolve => { bodyStarted = resolve; });
      const { api, onMutation, onSessionExpired } = createClient();
      globalThis.fetch = async () => Response.json({}, { headers: { 'X-Molr-CSRF-Cookie': 'molr_dev_csrf' } });
      await api.get('/api/auth/me');
      globalThis.fetch = async (_path, { signal }) => new Response(new ReadableStream({
        start(stream) {
          signal.addEventListener('abort', () => stream.error(signal.reason), { once: true });
        },
        pull() { bodyStarted(); },
      }), { status, headers: { 'Content-Type': 'application/json' } });
      let mutations = 0, expirations = 0;
      onMutation(() => mutations++);
      onSessionExpired(() => expirations++);
      const pending = method === 'read' ? api.read('/api/example', {}, controller.signal) : api[method]('/api/example', controller.signal);
      const rejected = assert.rejects(pending, error => error === controller.signal.reason && error.name === 'AbortError');
      await started;
      controller.abort();
      await rejected;
      assert.equal(mutations, 0);
      assert.equal(expirations, 0);
      globalThis.fetch = async () => Response.json({ ok: true });
      assert.deepEqual(await api.get('/api/example'), { ok: true }, 'Cancellation must not install a rate-limit cooldown');
    }));
  }
}

test('malformed successful JSON remains invalid_response and HTTP errors retain their status', async () => withBrowser(async () => {
  const { api } = createClient();
  globalThis.fetch = async () => new Response('not json', { status: 200 });
  await assert.rejects(api.get('/api/example'), error => error instanceof APIError && error.code === 'invalid_response' && error.status === 200);
  globalThis.fetch = async () => new Response('not json', { status: 403 });
  await assert.rejects(api.get('/api/example'), error => error instanceof APIError && error.code === 'unavailable' && error.status === 403);
  globalThis.fetch = async () => new Response(JSON.stringify({ code: 'forbidden', request_id: 'example-request' }), { status: 403 });
  await assert.rejects(api.get('/api/example'), error => error.code === 'forbidden' && error.requestID === 'example-request' && error.status === 403);
}));

test('HTTP error reasons survive without exposing diagnostic messages', async () => withBrowser(async () => {
  const { api } = createClient();
  globalThis.fetch = async () => Response.json({}, { headers: { 'X-Molr-CSRF-Cookie': 'molr_dev_csrf' } });
  await api.get('/api/auth/me');
  for (const reason of ['network_private_address_denied', undefined, { secret: 'private' }]) {
    globalThis.fetch = async () => Response.json({ code: 'invalid_argument', reason, message: 'private diagnostic' }, { status: 400 });
    await assert.rejects(api.post('/api/resources/test-connection'), error => error instanceof APIError
      && error.code === 'invalid_argument' && error.status === 400
      && error.reason === (typeof reason === 'string' ? reason : '') && !error.message.includes('private diagnostic'));
  }
}));

for (const [header, expected] of [["3661", 3661000], [null, 0], ["invalid", 0]]) {
 test(`rate-limit wait uses only a valid Retry-After header (${header})`, async () => withBrowser(async () => {
  const { api } = createClient();
  globalThis.fetch = async () => Response.json({ code: 'rate_limited' }, { status: 429, headers: header ? { 'Retry-After': header } : {} });
  const before = Date.now();
  await assert.rejects(api.post('/api/auth/login', {}, 'synthetic-prelogin'), error => expected ? error.retryAt >= before + expected && error.retryAt <= Date.now() + expected : error.retryAt === 0);
 }));
}

for (const name of ['molr_lan_csrf_p5193', 'molr_dev_csrf_p5193', '__Host-molr_csrf_p5193', '__Host-molr_csrf', 'molr_dev_csrf']) {
  test(`uses only the Runtime-selected ${name} with competing cookies present`, async () => withBrowser(async () => {
    const candidates = ['molr_lan_csrf_p5193', 'molr_dev_csrf_p5193', '__Host-molr_csrf_p5193', '__Host-molr_csrf', 'molr_dev_csrf', 'molr_lan_csrf_p5194'];
    document.cookie = candidates.map(candidate => `${candidate}=${candidate === name ? 'current-proof' : 'stale-proof'}`).join('; ');
    const client = createClient();
    assert.equal(client.csrfToken(), '', 'No cookie guessing before Runtime discovery');
    const calls = [];
    globalThis.fetch = async (path, init) => {
      calls.push(path);
      if (path === '/api/auth/me') return Response.json({}, { headers: { 'X-Molr-CSRF-Cookie': name } });
      assert.equal(init.headers.get('X-CSRF-Token'), 'current-proof');
      return Response.json({ verification: 'synthetic-proof' }, { headers: { 'X-Molr-CSRF-Cookie': name } });
    };
    await client.api.post('/api/auth/reauthenticate', { password: 'synthetic-password', purpose: 'password.change' });
    assert.deepEqual(calls, ['/api/auth/me', '/api/auth/reauthenticate']);
    // Reloaded clients rediscover the selector. Live clients read the cookie
    // again when another tab rotates or removes the current session.
    const reloaded = createClient();
    await reloaded.api.get('/api/auth/me');
    assert.equal(reloaded.csrfToken(), 'current-proof');
    document.cookie = document.cookie.replace(`${name}=current-proof`, `${name}=rotated-proof`);
    assert.equal(client.csrfToken(), 'rotated-proof');
    document.cookie = candidates.filter(candidate => candidate !== name).map(candidate => `${candidate}=stale-proof`).join('; ');
    assert.equal(client.csrfToken(), '', 'Missing selected cookie must not fall back to an old identity');
  }));
}

test('login uses the explicit pre-login proof and selects the returned session cookie', async () => withBrowser(async () => {
  const client = createClient();
  const calls = [];
  document.cookie = 'molr_dev_csrf_p5193=old-proof; molr_lan_csrf_p5193=current-proof';
  globalThis.fetch = async (path, init) => {
    calls.push(path);
    assert.equal(init.headers.get('X-CSRF-Token'), 'prelogin-proof');
    return Response.json({}, { headers: { 'X-Molr-CSRF-Cookie': 'molr_lan_csrf_p5193' } });
  };
  await client.api.post('/api/auth/login', {}, 'prelogin-proof');
  assert.deepEqual(calls, ['/api/auth/login']);
  assert.equal(client.csrfToken(), 'current-proof');
}));

test('missing or invalid cookie discovery never sends the pending mutation', async () => withBrowser(async () => {
  for (const name of ['', 'unrelated_cookie', 'molr_lan_csrf_p5193; unsafe']) {
    const client = createClient(), calls = [];
    document.cookie = 'molr_dev_csrf=old-proof';
    globalThis.fetch = async path => {
      calls.push(path);
      return Response.json({}, { headers: { 'X-Molr-CSRF-Cookie': name } });
    };
    await assert.rejects(client.api.post('/api/auth/password', {}), error => error.code === 'invalid_response');
    assert.deepEqual(calls, ['/api/auth/me']);
  }
}));


test('only successful non-auth requests notify recovery subscribers, including reads and writes', () => withBrowser(async () => {
  const client = createClient(); let successes = 0;
  const unsubscribe = client.onRequestSuccess(() => successes++);
  globalThis.fetch = async () => Response.json({}, { headers: { 'X-Molr-CSRF-Cookie': 'molr_dev_csrf' } });
  await client.api.get('/api/auth/me');
  await client.verifySession();
  assert.equal(successes, 0);
  await client.api.get('/api/assistants');
  await client.api.post('/api/conversations/messages');
  await client.api.read('/api/plugins/query', {});
  await client.api.download('/api/files/example');
  assert.equal(successes, 4);
  for (const response of [Response.json({}, { status: 503 }), new Response('invalid JSON')]) {
    globalThis.fetch = async () => response;
    await assert.rejects(client.api.get('/api/assistants'));
  }
  assert.equal(successes, 4);
  unsubscribe();
  globalThis.fetch = async () => Response.json({});
  await client.api.get('/api/assistants');
  assert.equal(successes, 4);
}));

for (const cause of ['abort', 'identity-change', 'clear']) {
  test(`${cause} prevents a stale successful request from waking the current subscription`, () => withBrowser(async () => {
    const client = createClient();
    document.cookie = 'molr_dev_csrf=synthetic-first';
    globalThis.fetch = async () => Response.json({}, { headers: { 'X-Molr-CSRF-Cookie': 'molr_dev_csrf' } });
    await client.api.get('/api/auth/me');
    let respond, successes = 0;
    client.onRequestSuccess(() => successes++);
    globalThis.fetch = () => new Promise(resolve => { respond = resolve; });
    const controller = new AbortController();
    const pending = client.api.get('/api/assistants', controller.signal);
    if (cause === 'abort') controller.abort();
    if (cause === 'identity-change') document.cookie = 'molr_dev_csrf=synthetic-second';
    if (cause === 'clear') client.clearClientState();
    respond(Response.json({})); await pending;
    assert.equal(successes, 0);
  }));
}
