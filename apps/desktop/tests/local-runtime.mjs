// Native systemd/launchd acceptance. Uses a unique desktop-owned instance and
// private XDG directories; never reuses an existing Runtime's data or credentials.
import assert from 'node:assert/strict';
import { mkdtempSync, realpathSync, readFileSync, existsSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { LocalRuntime } from '../dist/main/runtime/local.js';
const root = resolve(import.meta.dirname, '../../..');
const arch = process.arch === 'arm64' ? 'arm64' : 'amd64';
const packageRoot = resolve(root, process.env.MOLR_DESKTOP_RUNTIME_PACKAGE || `.cache/release/${process.platform}-${arch}/molr`);
const directory = realpathSync(mkdtempSync(join(tmpdir(), 'molr-desktop-native-')));
process.env.XDG_CONFIG_HOME = join(directory, 'config');
process.env.XDG_DATA_HOME = join(directory, 'data');
process.env.XDG_CACHE_HOME = join(directory, 'cache');
const runtime = new LocalRuntime(packageRoot, join(directory, 'manager'));
let record, session;
try {
  assert.equal((await runtime.status()).installed, false);
  const installed = await runtime.action('install');
  assert.equal(installed.installed, true); assert.equal(installed.health, 'verified');
  record = JSON.parse(readFileSync(installed.record));
  assert.equal(record.manager, 'desktop'); assert.equal(record.system, false);
  assert(installed.url.startsWith('http://127.0.0.1:'));
  assert(installed.loginURL?.startsWith(installed.url + '/#/login?token='));
  const log = readFileSync(runtime.logFile, 'utf8'); assert(!log.includes('/#/login?token='));
  const { request } = await import('@playwright/test');
  session = await request.newContext({ baseURL: installed.url, extraHTTPHeaders: { Origin: installed.url } });
  const csrf = (await (await session.get('/api/auth/csrf')).json()).csrf_token;
  const password = readFileSync(join(record.data_dir, 'initial-password'), 'utf8').trim();
  const loginResponse = await session.post('/api/auth/login', { headers: { 'X-CSRF-Token': csrf }, data: { username: 'owner', password, device_name: 'Desktop acceptance', client_type: 'cli' } });
  assert.equal(loginResponse.status(), 200);
  const auth = await loginResponse.json();
  const health = await (await session.get('/api/sandbox')).json();
  if (process.platform === 'linux') {
    assert.equal(health.Enabled, true); assert.equal(health.Capabilities.Reason, 'available');
    for (const key of ['Shell', 'ProcessIsolation', 'FileIsolation', 'NetworkIsolation', 'ResourceLimits']) assert.equal(health.Capabilities[key], true, key);
  } else { assert.equal(health.Enabled, false); assert.equal(health.Storage, true); }
  const status = await runtime.status(); assert(['active', 'running'].includes(status.state));
  await runtime.action('upgrade');
  assert.equal((await session.get('/api/auth/me')).status(), 200, 'Bundled update preserves login session and account data');
  // Dropping the manager object does not stop its service.
  const reopened = new LocalRuntime(packageRoot, join(directory, 'manager'));
  assert.equal((await reopened.status()).url, installed.url);
  await runtime.action('stop'); assert.equal((await runtime.status()).state, 'inactive');
  await runtime.action('start');
  assert.equal((await session.get('/api/auth/me')).status(), 200, 'Runtime restart preserves login session');
  assert.equal((await session.get('/api/sandbox')).status(), 200);
  await session.post('/api/auth/logout', { headers: { 'X-CSRF-Token': auth.csrf_token }, data: {} });
  await session.dispose(); session = undefined;
  await runtime.action('uninstall');
  assert.equal((await runtime.status()).installed, false);
  assert(existsSync(join(record.data_dir, 'runtime.db')), 'Uninstall must preserve data');
  assert(!existsSync(record.paths.unit), 'Uninstall must unregister the service');
  console.log('Native desktop Runtime passed: owned install, authenticated capabilities, background service, stop/start, persistent session, uninstall preserves data.');
} finally {
  await session?.dispose();
  const status = await runtime.status().catch(() => null);
  if (status?.installed) await runtime.action('uninstall');
  rmSync(directory, { recursive: true, force: true });
}
