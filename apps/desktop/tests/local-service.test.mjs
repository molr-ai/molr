import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ConnectionStore } from '../dist/main/main/connections.js';
import { LocalService } from '../dist/main/main/local-service.js';

const deferred = () => Promise.withResolvers();
const installed = { available: true, installed: true, state: 'running', url: 'http://127.0.0.1:8787', version: '0.1.0', updatePending: true };
const options = { confirm: async () => true, before() {} };
function fixture(t, overrides = {}, hooks = {}) {
  const directory = mkdtempSync(join(tmpdir(), 'molr-local-service-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const store = new ConnectionStore(join(directory, 'connections.json'));
  const calls = { reads: 0, actions: 0, changed: 0, retired: [] };
  let status = { ...installed };
  const runtime = {
    async status() { calls.reads++; return { ...status }; },
    async action() { calls.actions++; return { installed: status.installed, url: status.url }; },
    ...overrides,
  };
  const service = new LocalService(runtime, store, {
    changed() { calls.changed++; },
    async retire(id) { calls.retired.push(id); },
    ...hooks,
  });
  return { service, store, calls, setStatus(value) { status = value; } };
}

test('concurrent refreshes share both the read and retirement; later refreshes read again', async t => {
  const read = deferred(), cleaning = deferred();
  let reads = 0, retired = 0;
  const { service, store } = fixture(t, { status() { reads++; return read.promise; } }, { async retire() { retired++; await cleaning.promise; } });
  store.managed('http://127.0.0.1:9999');
  const first = service.refresh();
  assert.equal(service.refresh(), first);
  read.resolve(installed);
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(retired, 1);
  assert.equal(service.refresh(), first, 'Reconciliation is part of the shared operation');
  cleaning.resolve();
  await first;
  await service.refresh();
  assert.equal(reads, 2);
  assert.equal(retired, 1);
});

test('maintenance drains an earlier read and holds pollers until final reconciliation', async t => {
  const oldRead = deferred(), mutation = deferred();
  let reads = 0, actions = 0;
  const final = { ...installed, version: '0.2.0', updatePending: false };
  const { service } = fixture(t, {
    status() { return ++reads === 1 ? oldRead.promise : Promise.resolve(final); },
    async action() { actions++; await mutation.promise; return { installed: true }; },
  });
  const first = service.refresh();
  const action = service.action('upgrade', options);
  const poll = service.refresh();
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(actions, 0);
  oldRead.resolve(installed);
  await first;
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(actions, 1);
  assert.equal(reads, 1);
  assert.equal(service.refresh(), poll);
  mutation.resolve();
  await Promise.all([action, poll]);
  assert.equal(reads, 2);
  assert.equal(service.status.version, '0.2.0');
});

test('native confirmation is locked, cancellation neither mutates nor refreshes', async t => {
  const { service, calls } = fixture(t);
  await service.refresh();
  const confirm = deferred();
  const action = service.action('upgrade', { ...options, confirm: () => confirm.promise });
  await assert.rejects(service.action('stop', options), /请等待/);
  const poll = service.refresh();
  confirm.resolve(false);
  assert.equal(await action, undefined);
  await poll;
  assert.equal(calls.actions, 0);
  assert.equal(calls.reads, 1);
  await service.action('start', options);
  assert.equal(calls.actions, 1, 'Cancellation releases the lock');
});

test('failure keeps the original error, reconciles once, and allows retry', async t => {
  const failure = new Error('upgrade failed');
  const { service, store, calls, setStatus } = fixture(t, { async action() { throw failure; } });
  await service.refresh();
  const previous = store.data.connections[0].id;
  setStatus({ available: true, installed: false, state: 'uninstalled' });
  await assert.rejects(service.action('upgrade', options), e => e === failure);
  assert.equal(calls.reads, 2);
  assert.deepEqual(calls.retired, [previous]);
  assert.equal(store.data.connections.length, 0);
  await service.refresh();
  assert.deepEqual(calls.retired, [previous]);
  await assert.rejects(service.action('start', options), e => e === failure);
});

test('failed observations preserve connections and recover on the next read', async t => {
  let fail = false;
  const { service, store } = fixture(t, { async status() { if (fail) throw new Error('offline'); return installed; } });
  await service.refresh();
  const id = store.data.connections[0].id;
  fail = true;
  await service.refresh();
  assert.equal(service.status.error, 'offline');
  assert.equal(store.data.connections[0].id, id);
  fail = false;
  await service.refresh();
  assert.equal(service.status.error, undefined);
});


test('failed session cleanup is retained for retry after connection reconciliation', async t => {
  let attempts = 0;
  const { service, store } = fixture(t, {}, { async retire() { if (++attempts === 1) throw new Error('cleanup failed'); } });
  store.managed('http://127.0.0.1:9999');
  await service.refresh();
  assert.equal(service.status.error, 'cleanup failed');
  await service.refresh();
  assert.equal(attempts, 2);
  assert.equal(service.status.error, undefined);
  await service.refresh();
  assert.equal(attempts, 2);
});
