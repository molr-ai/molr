// Real main-process startup and IPC with a deliberately blocked installer read.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { randomUUID } from 'node:crypto';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { _electron as electron, expect } from '@playwright/test';
import { waitForVisibleWindow } from './window-controls.mjs';

if (process.platform === 'win32') throw new Error('The installer fixture requires a Unix runner.');
const root = resolve(import.meta.dirname, '../../..');
const directory = mkdtempSync(join(tmpdir(), 'molr-lifecycle-'));
const payload = join(directory, 'payload'), data = join(directory, 'data');
mkdirSync(join(payload, 'bin'), { recursive: true }); mkdirSync(data);
const server = createServer((_req, res) => {
  res.setHeader('Content-Type', 'text/html');
  res.end('<!doctype html><script>window.molrApp.getInfo()</script><h1>Remote fixture</h1>');
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const url = `http://127.0.0.1:${server.address().port}`;
const remoteID = randomUUID(), localID = randomUUID();
const connections = [
  { id: remoteID, kind: 'remote', name: 'Remote fixture', url, allowHTTP: true },
  { id: localID, kind: 'managed-local', name: '这台电脑', url: 'http://127.0.0.1:8787', allowHTTP: true },
];
writeFileSync(join(payload, 'release.json'), JSON.stringify({ version: '0.2.0', os: process.platform, arch: process.arch === 'arm64' ? 'arm64' : 'amd64' }));
writeFileSync(join(payload, 'bin/molr-setup'), `#!${process.execPath}
const fs = require('node:fs'), path = require('node:path');
const directory = path.join(__dirname, '..');
fs.appendFileSync(path.join(directory, 'calls'), process.argv[3] + '\\n');
(async () => {
  while (!fs.existsSync(path.join(directory, 'release'))) await new Promise(resolve => setTimeout(resolve, 25));
  console.log(JSON.stringify({type:'desktop', installed:true, state:'running', version:'0.1.0', url:'http://127.0.0.1:8787'}));
})();
`, { mode: 0o755 });

// Inject OS events while discovery is pending and while a window is loading.
const bootstrap = join(directory, 'bootstrap.mjs');
writeFileSync(bootstrap, `
import { app } from 'electron';
import { existsSync } from 'node:fs';
globalThis.createdWindows = 0;
app.on('browser-window-created', () => {
  globalThis.createdWindows++;
  setImmediate(() => { app.emit('activate'); app.emit('second-instance'); });
});
await import(${JSON.stringify(pathToFileURL(join(root, 'apps/desktop/dist/main/main/index.js')).href)});
const timer = setInterval(() => {
  if (!existsSync(${JSON.stringify(join(payload, 'calls'))})) return;
  clearInterval(timer); app.emit('activate'); app.emit('second-instance');
}, 5);
`);
let app;
let phase;
const checkpoint = value => { phase = value; console.log('Lifecycle acceptance:', value); };
async function launch(selected, saved = connections) {
  checkpoint('launch Electron');
  writeFileSync(join(data, 'connections.json'), JSON.stringify({ connections: saved, lastConnection: selected }));
  writeFileSync(join(payload, 'calls'), ''); rmSync(join(payload, 'release'), { force: true });
  const env = { ...process.env, MOLR_DESKTOP_DATA: data, MOLR_DESKTOP_RUNTIME: payload };
  delete env.ELECTRON_RUN_AS_NODE; delete env.MOLR_DESKTOP_DEV_URL;
  app = await electron.launch({ args: [bootstrap, ...(process.env.MOLR_DESKTOP_TEST_HEADLESS === '1' ? ['--ozone-platform=headless'] : [])], env, chromiumSandbox: true, timeout: 30000 });
  app.context().setDefaultTimeout(15000);
  return waitForVisibleWindow(app, await app.firstWindow());
}
const snapshot = page => page.evaluate(() => window.molrDesktop.state());
try {
  checkpoint('remote restoration during slow discovery');
  let page = await launch(remoteID);
  await expect.poll(async () => (await snapshot(page)).view).toBe('runtime');
  assert.equal((await snapshot(page)).local.state, 'checking', 'Remote restoration does not await the installer');
  assert.equal(await app.evaluate(() => globalThis.createdWindows), 1);
  assert.equal(await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows().length), 1);
  // Many IPC readers share the still-blocked startup query.
  checkpoint('coalesced installer reads');
  await page.evaluate(() => { window.pendingReads = Promise.all(Array.from({ length: 8 }, () => window.molrDesktop.refreshLocal())); });
  assert.equal(readFileSync(join(payload, 'calls'), 'utf8'), 'status\n');
  writeFileSync(join(payload, 'release'), '');
  await page.evaluate(() => window.pendingReads);
  assert.equal((await snapshot(page)).local.state, 'running');
  assert.equal((await snapshot(page)).active, remoteID);
  assert.equal(readFileSync(join(payload, 'calls'), 'utf8'), 'status\n');
  checkpoint('close Electron before local restoration');
  await app.close(); app = undefined;

  checkpoint('local restoration and user selection');
  page = await launch(localID);
  await expect(page.getByText('正在检测本机服务…')).toBeVisible();
  await expect(page.getByRole('article', { name: '这台电脑' }).getByRole('button', { name: '检测中' })).toBeDisabled();
  await page.evaluate(id => window.molrDesktop.connect(id), remoteID);
  await expect.poll(async () => (await snapshot(page)).active).toBe(remoteID);
  writeFileSync(join(payload, 'release'), '');
  await expect.poll(async () => (await snapshot(page)).local.state).toBe('running');
  assert.equal((await snapshot(page)).active, remoteID, 'Delayed local restoration cannot replace the user-selected remote connection');
  // Reopen bursts share a single new window, after the previous window closes.
  checkpoint('window event bursts');
  await expect.poll(async () => (await snapshot(page)).view).toBe('runtime');
  await app.evaluate(({ app }) => app.removeAllListeners('window-all-closed'));
  for (let index = 0; index < 2; index++) {
    checkpoint(`close window ${index + 1}`);
    await Promise.all([
      page.waitForEvent('close'),
      app.evaluate(({ BrowserWindow }) => new Promise(resolve => {
        const window = BrowserWindow.getAllWindows()[0];
        window.once('closed', resolve);
        window.destroy();
      })),
    ]);
    checkpoint(`reopen window ${index + 1}`);
    const reopened = app.waitForEvent('window');
    await app.evaluate(({ app }) => {
      for (let i = 0; i < 8; i++) { app.emit('activate'); app.emit('second-instance'); }
    });
    page = await waitForVisibleWindow(app, await reopened);
    await expect.poll(async () => (await snapshot(page)).view).toBe('runtime');
    assert.equal(await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows().length), 1);
    assert.equal(await app.evaluate(() => globalThis.createdWindows), index + 2);
  }
  checkpoint('close Electron before connection form');
  await app.close(); app = undefined;
  checkpoint('connection form survives discovery');
  page = await launch(undefined, []);
  const address = page.getByRole('textbox', { name: '服务地址', exact: true });
  await address.fill('https://remote.example.com');
  writeFileSync(join(payload, 'release'), '');
  await expect.poll(async () => (await snapshot(page)).local.state).toBe('running');
  await expect(address).toHaveValue('https://remote.example.com');
  console.log('Lifecycle passed: slow discovery, remote restoration, coalesced IPC, stale restoration, window event bursts and reopening.');
} catch (error) {
  // Print before teardown: an Electron shutdown failure must not hide the cause.
  console.error('Lifecycle acceptance failed during:', phase, error);
  throw error;
} finally {
  checkpoint('cleanup: release installer fixture');
  writeFileSync(join(payload, 'release'), '');
  checkpoint('cleanup: close Electron');
  if (app) await app.close();
  checkpoint('cleanup: close HTTP fixture');
  server.closeAllConnections(); await new Promise(resolve => server.close(resolve));
  checkpoint('cleanup: remove profile');
  rmSync(directory, { recursive: true, force: true });
  checkpoint('cleanup complete');
}
