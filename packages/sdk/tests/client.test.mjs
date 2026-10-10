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
      globalThis.fetch = async (_path, { signal }) => new Response(new ReadableStream({
        start(stream) {
          signal.addEventListener('abort', () => stream.error(signal.reason), { once: true });
        },
        pull() { bodyStarted(); },
      }), { status, headers: { 'Content-Type': 'application/json' } });
      const { api, onMutation, onSessionExpired } = createClient();
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
