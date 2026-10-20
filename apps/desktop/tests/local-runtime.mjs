// Native systemd/launchd acceptance. Uses a unique desktop-owned instance and
// private XDG directories; never reuses an existing Runtime's data or credentials.
import assert from 'node:assert/strict';
import { mkdtempSync, realpathSync, readFileSync, existsSync, rmSync, writeFileSync, mkdirSync } from 'node:fs';
import { cp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { LocalRuntime } from '../dist/main/runtime/local.js';
const root = resolve(import.meta.dirname, '../../..');
const arch = process.arch === 'arm64' ? 'arm64' : 'amd64';
const packageRoot = resolve(root, process.env.MOLR_DESKTOP_RUNTIME_PACKAGE || `.cache/release/${process.platform}-${arch}/molr`);
// The JS realpath implementation keeps Windows 8.3 aliases used by CI's TEMP.
const canonicalPath = process.platform === 'win32' ? realpathSync.native : realpathSync;
const directory = canonicalPath(mkdtempSync(join(tmpdir(), process.platform === 'win32' ? 'molr-中文路径-' : 'molr-desktop-native-')));
process.env.XDG_CONFIG_HOME = join(directory, 'config');
process.env.XDG_DATA_HOME = join(directory, 'data');
process.env.XDG_CACHE_HOME = join(directory, 'cache');
const runtime = new LocalRuntime(packageRoot, join(directory, 'manager'));
const administrator = { username: 'desktop-owner', password: 'native-fixture-password-2026' };
let record, session;
try {
  assert.equal((await runtime.status()).installed, false);

  if (process.platform === 'win32') {
    // Reproduce a failure after bootstrap and before task registration: the
    // XML destination is temporarily a directory. Retry must preserve the
    // existing owner/password and still return the initial authenticated link.
    const blockedXML = join(directory, 'manager', 'program', 'task.xml');
    mkdirSync(blockedXML, { recursive: true });
    await assert.rejects(runtime.action('install', 'permissive', administrator));
    assert.equal((await runtime.status()).installed, false);
    assert(!existsSync(join(directory, 'manager', 'program', 'data', 'initial-password')));
    rmSync(blockedXML, { recursive: true });
  }
  const installed = await runtime.action('install', 'permissive', administrator);

  assert.equal(installed.installed, true); assert.equal(installed.health, 'verified');
  record = JSON.parse(readFileSync(installed.record));
  assert.equal(record.manager, 'desktop'); assert.equal(record.system, false);
  assert(installed.url.startsWith('http://127.0.0.1:'));
  assert(installed.loginURL?.startsWith(installed.url + '/#/login?token='));
  const log = existsSync(runtime.logFile) ? readFileSync(runtime.logFile, 'utf8') : ''; assert(!log.includes('/#/login?token='));
  const { request } = await import('@playwright/test');
  session = await request.newContext({ baseURL: installed.url, extraHTTPHeaders: { Origin: installed.url } });
  const csrf = (await (await session.get('/api/auth/csrf')).json()).csrf_token;
  assert(!existsSync(join(record.data_dir, 'initial-password')));
  assert(!log.includes(administrator.password));
  const password = administrator.password;
  const loginResponse = await session.post('/api/auth/login', { headers: { 'X-CSRF-Token': csrf }, data: { username: administrator.username, password, device_name: 'Desktop acceptance', client_type: 'cli' } });
  assert.equal(loginResponse.status(), 200);
  const auth = await loginResponse.json();
  const health = await (await session.get('/api/sandbox')).json();
  if (process.platform === 'linux') {
    assert.equal(health.Enabled, true); assert.equal(health.Capabilities.Reason, 'available');
    for (const key of ['Shell', 'ProcessIsolation', 'FileIsolation', 'NetworkIsolation', 'ResourceLimits']) assert.equal(health.Capabilities[key], true, key);
  } else if (process.platform === 'win32') { assert.equal(health.Enabled, true); assert.equal(health.Storage, true); assert.equal(health.Capabilities.Reason, 'available'); assert.equal(health.Capabilities.NetworkBackend, 'appcontainer-toggle'); assert.equal(health.Capabilities.Shell, true); assert.equal(health.Capabilities.FullAccess, true); assert.equal(health.Capabilities.ProcessIsolation, false); assert.equal(health.Capabilities.ResourceLimits, false); } else { assert.equal(health.Enabled, true); assert.equal(health.Storage, true); assert.equal(health.Capabilities.NetworkBackend, 'seatbelt-toggle'); }
  const status = await runtime.status(); assert(['active', 'running'].includes(status.state));
  if (process.platform === 'win32') {
    // Node 22.21's cpSync mishandles non-ASCII Windows paths (nodejs/node#61950).
    const invalidPackage = join(directory, 'invalid-package'); await cp(packageRoot, invalidPackage, { recursive: true });
    const metadataPath = join(invalidPackage, 'release.json'); const metadata = JSON.parse(readFileSync(metadataPath));
    metadata.version = '999.0.0-local'; writeFileSync(metadataPath, JSON.stringify(metadata));
    const broken = new LocalRuntime(invalidPackage, join(directory, 'manager'));
    await assert.rejects(broken.action('upgrade'), /binary does not match release/);
    assert.equal((await runtime.status()).state, 'running', 'Failed upgrade restores the previous task');
    assert.equal((await session.get('/api/auth/me')).status(), 200, 'Failed upgrade preserves account and session');
  }
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
  {
    const reinstalled = await runtime.action('install', 'least-privilege');
    assert.equal(reinstalled.installed, true); assert.equal(reinstalled.loginURL, undefined);
    assert.equal(reinstalled.health, 'verification_pending');
    await runtime.action('uninstall');
  }

  console.log('Native desktop Runtime passed: owned install, authenticated capabilities, background service, stop/start, persistent session, uninstall preserves data.');
} finally {
  await session?.dispose();
  const status = await runtime.status().catch(() => null);
  if (status?.installed) await runtime.action('uninstall');
  rmSync(directory, { recursive: true, force: true });
}
