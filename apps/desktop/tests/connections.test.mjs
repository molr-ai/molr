import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync, writeFileSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ConnectionStore, normalizeURL, partition, sameOrigin, externalURL, allowedFrameURL } from '../dist/main/main/connections.js';

test('connection URLs cannot carry credentials, paths or executable protocols', () => {
  for (const url of ['javascript:alert(1)', 'file:///etc/passwd', 'https://user:pass@example.com', 'https://example.com/#/login?token=fixture', 'https://example.com/api', 'https://example.com/?x=1', 'http://example.com', 'http://0.0.0.0', 'http://169.254.169.254', 'http://localhost.evil.example', 'https://example.com\\@evil.example']) assert.throws(() => normalizeURL(url, true), url);
  assert.throws(() => normalizeURL('http://127.0.0.1:8787', false));
  assert.equal(normalizeURL(' http://127.0.0.1:8787/ ', true), 'http://127.0.0.1:8787');
  assert.equal(normalizeURL('http://[::1]:8787', true), 'http://[::1]:8787');
  assert.equal(normalizeURL('https://EXAMPLE.com:443/', false), 'https://example.com');
});
test('sessions are isolated, endpoint changes rotate identity, WSL remains remote', t => {
  const root = mkdtempSync(join(tmpdir(), 'molr-connections-')); t.after(() => rmSync(root, { recursive: true, force: true }));
  const file = join(root, 'connections.json'), store = new ConnectionStore(file);
  const first = store.save({ name: 'WSL', url: 'http://localhost:8787', allowHTTP: true }).connection;
  const second = store.save({ name: 'Other account', url: first.url, allowHTTP: true }).connection;
  assert.equal(first.kind, 'remote'); assert.notEqual(partition(first.id), partition(second.id));
  store.select(first.id);
  const renamed = store.save({ id: first.id, name: 'Renamed', url: first.url, allowHTTP: true });
  assert.equal(renamed.connection.id, first.id); assert.equal(renamed.retired, undefined);
  const changed = store.save({ id: first.id, name: 'Server', url: 'https://molr.example.com', allowHTTP: false });
  assert.equal(changed.retired, first.id); assert.notEqual(changed.connection.id, first.id);
  assert.equal(store.data.lastConnection, changed.connection.id);
  assert.deepEqual(new ConnectionStore(file).data, store.data);
  store.remove(changed.connection.id); assert.equal(store.data.lastConnection, undefined);
  assert.equal(JSON.parse(readFileSync(file)).connections.length, 1);
});
test('managed connection cannot be edited as a remote endpoint', t => {
  const root = mkdtempSync(join(tmpdir(), 'molr-connections-')); t.after(() => rmSync(root, { recursive: true, force: true }));
  const store = new ConnectionStore(join(root, 'connections.json'));
  const local = store.managed('http://127.0.0.1:8787');
  assert.throws(() => store.save({ id: local.id, name: 'Hijack', url: 'https://example.com', allowHTTP: false }));
  assert.equal(store.managed(local.url).id, local.id);
  assert.notEqual(store.managed('http://127.0.0.1:8788').id, local.id);
});
test('invalid persisted data fails closed without overwriting it', t => {
  const root = mkdtempSync(join(tmpdir(), 'molr-connections-')); t.after(() => rmSync(root, { recursive: true, force: true }));
  const file = join(root, 'connections.json');
  writeFileSync(file, '{bad'); assert.throws(() => new ConnectionStore(file)); assert.equal(readFileSync(file, 'utf8'), '{bad');
  assert.throws(() => partition('../../victim'));
});
test('navigation compares origins, never string prefixes', () => {
  assert(sameOrigin('https://molr.example.com/#/assistants', 'https://molr.example.com'));
  assert(!sameOrigin('https://molr.example.com.evil.example', 'https://molr.example.com'));
  assert(!sameOrigin('https://user@molr.example.com', 'https://molr.example.com'));
  for (const url of ['file:///tmp/test', 'ms-settings:', 'javascript:alert(1)', 'https://user:pass@example.com']) assert(!externalURL(url));
  assert(externalURL('https://example.com/docs'));
  assert(allowedFrameURL('blob:https://molr.example.com/fixture', 'https://molr.example.com', false));
  assert(!allowedFrameURL('blob:https://evil.example/fixture', 'https://molr.example.com', false));
  assert(!allowedFrameURL('blob:https://molr.example.com/fixture', 'https://molr.example.com', true));
  assert(!allowedFrameURL('molr-desktop://app/', 'https://molr.example.com', false));
  assert(!allowedFrameURL('file:///tmp/private', 'https://molr.example.com', false));
});
