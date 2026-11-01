import test from 'node:test';
import assert from 'node:assert/strict';
import ts from 'typescript';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { repository } from './check-polling.mjs';
// Compile the real implementations; only module URLs change for Node's loader.
function moduleURL(path, imports = {}) {
  let code = ts.transpileModule(readFileSync(join(repository, path), 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText;
  code = code.replace(/from\s+(['"])(.*?)\1/g, (_, quote, name) => {
    if (!imports[name]) throw new Error(`Unmapped runtime import: ${name}`);
    return `from ${JSON.stringify(imports[name])}`;
  });
  return `data:text/javascript;base64,${Buffer.from(code).toString('base64')}`;
}
const client = moduleURL('packages/sdk/src/client.ts');
const polling = moduleURL('apps/webui/src/polling.ts', { '@molr/sdk': client });
const sharing = moduleURL('apps/webui/src/share-json.ts');
const { QueryStore } = await import(moduleURL('apps/webui/src/query-store.ts', { '@molr/sdk': client, './polling': polling, './share-json': sharing }));
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };

test('event-only queries perform zero extra requests across one idle hour, then wake on change', async t => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  const store = new QueryStore(); let calls = 0;
  const load = async () => { calls++; return { value: calls }; };
  const release = store.subscribe('watch', () => {});
  try {
    store.refresh('watch', 1, load); await flush(); assert.equal(calls, 1);
    t.mock.timers.tick(3600000); await flush(); assert.equal(calls, 1, 'idle state must not be polled');
    store.refresh('watch', 2, load); await flush(); assert.equal(calls, 2, 'committed change must wake the query');
    release(); await flush(); t.mock.timers.tick(3600000); await flush(); assert.equal(calls, 2);
  } finally { store.clear(); }
});
test('allowed active polling is serial and stops when its last subscriber leaves', async t => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  const store = new QueryStore(); let calls = 0, resolve;
  const load = () => { calls++; return new Promise(done => { resolve = done; }); };
  const release = store.subscribe('active', () => {}, 2000);
  try {
    store.refresh('active', 1, load); await flush();
    t.mock.timers.tick(60000); await flush(); assert.equal(calls, 1, 'slow requests must not overlap');
    resolve('ready'); await flush();
    t.mock.timers.tick(1999); await flush(); assert.equal(calls, 1);
    t.mock.timers.tick(1); await flush(); assert.equal(calls, 2);
    release(); await flush(); resolve('late'); await flush();
    t.mock.timers.tick(3600000); await flush(); assert.equal(calls, 2);
  } finally { store.clear(); }
});

test('visible-query polling pauses while hidden or offline and resumes once on reconnection', async t => {
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] });
  const document = Object.assign(new EventTarget(), { visibilityState: 'visible' });
  const window = new EventTarget(), navigator = { onLine: true };
  for (const [key, value] of Object.entries({ document, window, navigator })) {
    const previous = Object.getOwnPropertyDescriptor(globalThis, key);
    Object.defineProperty(globalThis, key, { configurable: true, value });
    t.after(() => { if (previous) Object.defineProperty(globalThis, key, previous); else delete globalThis[key]; });
  }
  const store = new QueryStore(); let calls = 0;
  const release = store.subscribe('visible', () => {}, 2000);
  try {
    store.refresh('visible', 1, async () => ++calls); await flush(); assert.equal(calls, 1);
    document.visibilityState = 'hidden'; document.dispatchEvent(new Event('visibilitychange'));
    t.mock.timers.tick(60000); await flush(); assert.equal(calls, 1);
    document.visibilityState = 'visible'; navigator.onLine = false; document.dispatchEvent(new Event('visibilitychange'));
    t.mock.timers.tick(60000); await flush(); assert.equal(calls, 1);
    navigator.onLine = true; window.dispatchEvent(new Event('online')); await flush(); assert.equal(calls, 2);
    release(); await flush(); t.mock.timers.tick(60000); await flush(); assert.equal(calls, 2);
  } finally { store.clear(); }
});
