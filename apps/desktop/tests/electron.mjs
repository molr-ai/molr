// Real Electron processes and storage partitions; the HTTP fixture tests transport
// isolation. Native Runtime acceptance is a separate test, never inferred here.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { setTimeout as delay } from 'node:timers/promises';
import { tmpdir } from 'node:os';
import { resolve, join } from 'node:path';
import { _electron as electron, expect } from '@playwright/test';
import { expectMacTitlebar, expectFullscreenTitlebar, expectStableMacTitlebar, waitForVisibleWindow, waitForWindowZoom } from './window-controls.mjs';
const root = resolve(import.meta.dirname, '../../..');
const data = mkdtempSync(join(tmpdir(), 'molr-desktop-test-'));
const screenshots = join(root, '.cache/acceptance/desktop'); mkdirSync(screenshots, { recursive: true });
const fixture = createServer((req, res) => {
  if (req.url === '/cookie') { res.setHeader('Set-Cookie', 'fixture_session=first; HttpOnly; SameSite=Strict; Path=/; Max-Age=86400'); return res.end('ok'); }
  if (req.url === '/who') return res.end(req.headers.cookie ?? '');
  if (req.url === '/stream') { res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.write('data: fixture-ready\n\n'); req.on('close', () => res.end()); return; }
  res.setHeader('Content-Type', 'text/html'); res.end('<!doctype html><script>window.molrApp.getInfo()</script><h1>Runtime fixture</h1><button onclick="void window.molrApp.disconnect()">Disconnect</button><a href="file:///tmp/private">unsafe</a>');
});
await new Promise(resolve => fixture.listen(0, '127.0.0.1', resolve));
const url = `http://127.0.0.1:${fixture.address().port}/`;
let app;
let phase = 'launch';
const checkpoint = value => { phase = value; console.log('Electron acceptance:', value); };
const watchdog = setTimeout(() => {
  console.error('Electron acceptance timed out during:', phase);
  process.exitCode = 1;
  app?.process().kill('SIGKILL');
  fixture.closeAllConnections();
}, 120000).unref();
async function launch() {
  const env = { ...process.env, MOLR_DESKTOP_DATA: data };
  delete env.MOLR_DESKTOP_RUNTIME; delete env.MOLR_DESKTOP_DEV_URL; delete env.ELECTRON_RUN_AS_NODE;
  app = await electron.launch({ args: [join(root, 'apps/desktop'), ...(process.env.MOLR_DESKTOP_TEST_HEADLESS === '1' ? ['--ozone-platform=headless'] : [])], env, chromiumSandbox: true, timeout: 30000 });
  app.context().setDefaultTimeout(15000);
  assert.equal(await app.evaluate(() => process.argv.includes('--no-sandbox')), false);
  return waitForVisibleWindow(app, await app.firstWindow());
}
async function remote(code) {
  return app.evaluate(async ({ webContents }, { url, code }) => {
    const wc = webContents.getAllWebContents().find(c => c.getURL() === url);
    if (!wc) throw new Error('Runtime view not found: ' + JSON.stringify(webContents.getAllWebContents().map(c => ({ url: c.getURL(), type: c.getType() }))));
    return wc.executeJavaScript(code);
  }, { url, code });
}
async function waitRuntime(page) { await expect.poll(() => page.evaluate(async () => (await window.molrDesktop.state()).view), { timeout: 35000 }).toBe('runtime'); }
async function closeWindow(page) {
  // The page can close before BrowserWindow emits 'closed' and the main
  // process clears its window reference. Wait for both before activating.
  await Promise.all([
    page.waitForEvent('close'),
    app.evaluate(({ BrowserWindow }) => new Promise((resolve, reject) => {
      const window = BrowserWindow.getAllWindows()[0];
      const timer = setTimeout(() => reject(new Error('Native window did not close')), 15000);
      window.once('closed', () => { clearTimeout(timer); resolve(); });
      window.close();
    })),
  ]);
}
try {
  checkpoint('first launch');
  let page = await launch();
  await page.getByRole('heading', { name: '连接远程服务' }).waitFor();
  checkpoint('hidden window readiness');
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].hide());
  const visible = waitForVisibleWindow(app, page);
  assert.equal(await Promise.race([visible.then(() => true), delay(100, false)]), false, 'A hidden native window is not ready for interaction');
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].show());
  await visible;
  await expect(page.locator('.desktop-app')).toHaveAttribute('data-app-platform', process.platform);
  await expect(page.locator('.desktop-identity > span')).toHaveCount(0);
  assert.deepEqual((await page.evaluate(() => window.molrDesktop.state())).appearance, { theme: 'system', palette: 'forest' });
  await expect(page.locator('.desktop-window-controls')).toHaveCount(process.platform === 'darwin' ? 0 : 1);
  if (process.platform === 'darwin') {
    for (const zoom of [0.8, 1.25, 1.5, 1]) {
      checkpoint(`macOS titlebar resize at zoom ${zoom}`);
      await app.evaluate(({ BrowserWindow }, zoom) => BrowserWindow.getAllWindows()[0].webContents.setZoomFactor(zoom), zoom);
      await waitForWindowZoom(app, page, zoom);
      await expectMacTitlebar(page, '.desktop-toolbar', '.desktop-identity .logo');
      await expectStableMacTitlebar(app, page, '.desktop-toolbar', '.desktop-identity .logo');
    }
    const margin = await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].getWindowButtonPosition());
    assert(margin.x > 0 && margin.x === margin.y, 'Native top and left margins match');
    // Exercise the renderer's fullscreen state independently of macOS Spaces.
    const state = await page.evaluate(() => window.molrDesktop.state());
    await app.evaluate(({ BrowserWindow }, state) => BrowserWindow.getAllWindows()[0].webContents.send('desktop:changed', { ...state, window: { ...state.window, fullscreen: true } }), state);
    await expectFullscreenTitlebar(page, '.desktop-toolbar', '.desktop-identity .logo');
    await app.evaluate(({ BrowserWindow }, state) => BrowserWindow.getAllWindows()[0].webContents.send('desktop:changed', state), state);
    await expectMacTitlebar(page, '.desktop-toolbar', '.desktop-identity .logo');
    const originalInset = (await page.locator('.desktop-identity .logo').boundingBox()).x;
    for (const zoom of [0.8, 1.5]) {
      checkpoint(`reopen macOS window at zoom ${zoom}`);
      await app.evaluate(({ BrowserWindow }, zoom) => BrowserWindow.getAllWindows()[0].webContents.setZoomFactor(zoom), zoom);
      await waitForWindowZoom(app, page, zoom);
      await expectMacTitlebar(page, '.desktop-toolbar', '.desktop-identity .logo');
      await closeWindow(page);
      [page] = await Promise.all([app.waitForEvent('window'), app.evaluate(({ app }) => app.emit('activate'))]);
      await waitForVisibleWindow(app, page);
      assert.equal(await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].webContents.getZoomFactor()), zoom);
      await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].webContents.setZoomFactor(1));
      await waitForWindowZoom(app, page, 1);
      await expectMacTitlebar(page, '.desktop-toolbar', '.desktop-identity .logo');
      assert.equal((await page.locator('.desktop-identity .logo').boundingBox()).x, originalInset, 'Reopening at fractional zoom preserves native measurements');
    }
  }
  const logo = page.locator('.desktop-identity .logo img');
  await expect(logo).toHaveAttribute('src', /molr-wordmark-(alive|playful)\.svg/);
  await expect(page.locator('.desktop-identity picture source')).toHaveAttribute('media', '(prefers-reduced-motion: reduce)');
  checkpoint('connection form');
  await page.getByLabel('连接名称（选填）', { exact: true }).fill('Fixture one');
  await page.getByLabel('服务地址', { exact: true }).fill('http://example.com');
  const allowHTTP = page.getByRole('switch', { name: '允许使用未加密的 HTTP 连接' });
  await allowHTTP.check();
  await expect(allowHTTP).toBeChecked();
  // Clicking a below-the-fold submit button can scroll a small native window.
  // Compare document coordinates to distinguish scrolling from a layout shift.
  const introBox = () => page.locator('.desktop-intro').evaluate(element => {
    const { left, top, width, height } = element.getBoundingClientRect();
    return { x: left + window.scrollX, y: top + window.scrollY, width, height };
  });
  const beforeError = await introBox();
  await page.getByRole('button', { name: '保存连接' }).click();
  await expect(page.locator('.ui-toast--danger [role="alert"]')).toBeVisible();
  assert.equal(await page.locator('.desktop-error').count(), 0);
  assert.deepEqual(await introBox(), beforeError, 'Error Toast does not shift page layout');
  await page.getByRole('button', { name: '保存连接' }).click();
  await expect(page.locator('.ui-toast--danger')).toHaveCount(1);
  await page.screenshot({path:join(screenshots,'error-toast.png'),animations:'disabled'});
  await page.locator('.ui-toast--danger').getByRole('button', {name:'关闭',exact:true}).click();
  await expect(page.locator('.ui-toast--danger')).toHaveCount(0);
  await page.getByLabel('服务地址', { exact: true }).fill(url);
  await page.getByRole('button', { name: '保存连接' }).click();
  await page.getByRole('heading', { name: 'Fixture one' }).waitFor();
  await page.evaluate(async url => window.molrDesktop.save({ name: 'Fixture two', url, allowHTTP: true }), url);
  const [one, two] = (await page.evaluate(() => window.molrDesktop.state())).connections;
  await page.evaluate(id => window.molrDesktop.connect(id), one.id); await waitRuntime(page);
  assert.deepEqual(await remote('({ bridge: typeof window.molrDesktop, require: typeof require, process: typeof process })'), { bridge: 'undefined', require: 'undefined', process: 'undefined' });
  const appInfo = await remote('window.molrApp.getInfo()');
  assert.equal(appInfo.apiVersion, 1); assert.equal(appInfo.connection.kind, 'remote'); assert.equal(appInfo.build.version, 'development');
  assert.equal((await remote('window.molrApp.updates.status()')).comparison, 'uncomparable');
  assert.match(await remote("window.molrApp.updates.upgradeLocal().then(() => 'unexpected', e => e.message)"), /不是桌面管理/);
  assert.equal(await remote('window.molrApp.consumeInitialLogin()'), false, 'Remote links cannot opt into installation login');
  assert.deepEqual(await remote('Object.keys(window.molrApp).sort()'), ['consumeInitialLogin', 'disconnect', 'getInfo', 'notifications', 'oauth', 'onWindowStateChanged', 'setAppearance', 'updates', 'window']);
  assert.equal(await page.locator('.desktop-toolbar').count(), 0);
  await remote('window.molrApp.setAppearance({ theme: "dark", palette: "ocean" })');
  assert.deepEqual((await page.evaluate(() => window.molrDesktop.state())).appearance, { theme: 'dark', palette: 'ocean' });
  assert.equal(await remote('window.molrApp.setAppearance({ theme: "dark", palette: "url(https://example.com)" }).then(() => false, () => true)'), true);

  if (process.platform === 'darwin') {
    const margin = await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].getWindowButtonPosition());
    assert(margin.x > 0 && margin.x === margin.y, 'Native margins remain equal with the Runtime view attached');
  }
  assert.equal(await app.evaluate(({ BrowserWindow }) => {
    const w = BrowserWindow.getAllWindows()[0], [width, height] = w.getContentSize();
    return { actual: w.contentView.children.find(view => view.webContents !== w.webContents).getBounds(), expected: { x: 0, y: 0, width, height } };
  }).then(({ actual, expected }) => JSON.stringify(actual) === JSON.stringify(expected)), true);
  if (process.platform === 'win32' || process.platform === 'linux') {
    assert.equal(await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].isMenuBarVisible()), false);
  }
  checkpoint('untrusted WebContents');
  // A different WebContents using the same preload still cannot invoke controls.
  const untrusted = await app.evaluate(async ({ BrowserWindow }, { url, preload }) => {
    const w = new BrowserWindow({ show: false, webPreferences: { preload, sandbox: true, nodeIntegration: false, contextIsolation: true } });
    try {
      await w.loadURL(new URL('untrusted', url).href);
      return { id: w.id, error: await w.webContents.executeJavaScript('window.molrApp.getInfo().then(() => "unexpected", error => error.message)') };
    } catch (error) { w.destroy(); throw error; }
  }, { url, preload: join(root, 'apps/desktop/dist/main/preload/runtime.cjs') });
  try {
    assert.match(untrusted.error, /unavailable to this page/);
    // Keep the same-prefix page alive so selection cannot depend on its cleanup timing.
    assert.equal((await remote('window.molrApp.getInfo()')).apiVersion, 1, 'Runtime selection excludes the untrusted page');
  } finally {
    await app.evaluate(({ BrowserWindow }, id) => BrowserWindow.fromId(id)?.destroy(), untrusted.id);
  }
  checkpoint('notification bridge');
  // No native notification permission or display is requested by these checks.
  await remote("window.molrApp.notifications.begin('fixture-login')");
  assert.match(await remote("window.molrApp.notifications.show('stale-login', {tag:'test',title:'Fixture',body:'Message',route:''}).then(() => 'unexpected', e => e.message)"), /expired/);
  assert.match(await remote("window.molrApp.notifications.show('fixture-login', {tag:'test',title:'Fixture',body:'Message',route:'https://example.invalid'}).then(() => 'unexpected', e => e.message)"), /Invalid/);
  await remote("window.molrApp.notifications.close('fixture-login')");
  assert.match(await remote("window.molrApp.notifications.setBadge('stale-login', 2).then(() => 'unexpected', e => e.message)"), /expired/);
  assert.match(await remote("window.molrApp.notifications.setBadge('fixture-login', -1).then(() => 'unexpected', e => e.message)"), /Invalid/);
  assert.equal(typeof await remote("window.molrApp.notifications.setBadge('fixture-login', 0)"), 'boolean');
  await remote("window.molrApp.notifications.end('fixture-login')");
  assert.match(await remote("window.molrApp.notifications.setBadge('fixture-login', 2).then(() => 'unexpected', e => e.message)"), /expired/);
  checkpoint('clipboard');
  await app.evaluate(({ app, BrowserWindow, webContents }, url) => {
    // CI can launch Electron behind another macOS app. Clipboard writes wait
    // for an active document, so activate the window before focusing its view.
    app.focus({ steal: true });
    const window = BrowserWindow.getAllWindows()[0]; window.show(); window.focus();
    const wc = webContents.getAllWebContents().find(c => c.getURL() === url); wc.focus();
  }, url);
  // CDP focus emulation gives the renderer an active document even when the
  // CI window server cannot foreground the app. Clipboard permission checks
  // and the actual operating-system clipboard still run normally.
  const runtimePage = app.context().pages().find(p => p.url() === url);
  assert(runtimePage, 'Runtime page is available for focus emulation');
  const focus = await app.context().newCDPSession(runtimePage);
  try {
    await focus.send('Emulation.setFocusEmulationEnabled', { enabled: true });
    await expect.poll(() => remote('document.hasFocus()')).toBe(true);
    await app.evaluate(async ({ webContents }, url) => {
      const wc = webContents.getAllWebContents().find(c => c.getURL() === url);
      await wc.executeJavaScript("navigator.clipboard.writeText('molr-desktop-copy-fixture')", true);
    }, url);
    assert.equal(await app.evaluate(({ clipboard }) => clipboard.readText()), 'molr-desktop-copy-fixture');
  } finally {
    await focus.send('Emulation.setFocusEmulationEnabled', { enabled: false });
    await focus.detach();
  }
  checkpoint('cookies and SSE');
  assert.equal(await remote("fetch('/cookie').then(() => fetch('/who')).then(r => r.text())"), 'fixture_session=first');
  assert.equal(await remote("new Promise(resolve => { const e = new EventSource('/stream'); e.onmessage = m => { e.close(); resolve(m.data); }; })"), 'fixture-ready');
  assert.equal(await remote(`new Promise(resolve => {
    const frame = document.createElement('iframe'); frame.sandbox = 'allow-scripts';
    const url = URL.createObjectURL(new Blob(['<script>parent.postMessage(typeof window.molrApp === "undefined" ? "plugin-ready" : "leaked-bridge", "*")<\\/script>'], {type:'text/html'}));
    const timer = setTimeout(() => resolve('timeout'), 5000);
    window.addEventListener('message', function listener(event) { if (event.source === frame.contentWindow && event.data === 'plugin-ready') { clearTimeout(timer); window.removeEventListener('message', listener); frame.remove(); URL.revokeObjectURL(url); resolve(event.data); } });
    frame.src = url; document.body.append(frame);
  })`), 'plugin-ready');
  const preferences = await app.evaluate(({ webContents }, url) => webContents.getAllWebContents().find(c => c.getURL() === url).getLastWebPreferences(), url);
  assert.equal(preferences.sandbox, true); assert.equal(preferences.contextIsolation, true); assert.equal(preferences.nodeIntegration, false); assert.equal(preferences.webSecurity, true);
  checkpoint('disconnect through Runtime UI');
  // Disconnect destroys the caller. Waiting for executeJavaScript's IPC result
  // can hang after the view has already closed; assert the visible outcome.
  await Promise.all([
    runtimePage.waitForEvent('close'),
    runtimePage.getByRole('button', { name: 'Disconnect', exact: true }).click(),
  ]);
  await page.getByRole('heading', { name: '选择连接' }).waitFor();
  await expect(page.locator('html')).toHaveAttribute('data-palette','ocean');
  await expect(page.locator('html')).toHaveAttribute('data-theme','dark');
  checkpoint('connect isolated session');
  await page.evaluate(id => window.molrDesktop.connect(id), two.id); await waitRuntime(page);
  await remote('window.molrApp.setAppearance({ theme: "light", palette: "rose" })');
  checkpoint('second session cookie check');
  assert.equal(await remote("fetch('/who').then(r => r.text())"), '');
  checkpoint('restore most recent connection');
  await app.close();
  page = await launch(); await waitRuntime(page);
  assert.equal((await page.evaluate(() => window.molrDesktop.state())).active, two.id);
  assert.equal(await remote("fetch('/who').then(r => r.text())"), '');
  checkpoint('return to manager');
  await page.evaluate(() => window.molrDesktop.manager());
  await expect(page.locator('html')).toHaveAttribute('data-palette','rose');
  await expect(page.locator('html')).toHaveAttribute('data-theme','light');
  // The main-process preference survives clearing the manager's rendering cache.
  await page.evaluate(() => localStorage.clear());
  checkpoint('restart');
  await app.close();
  page = await launch();
  await page.getByRole('heading', { name: '选择连接' }).waitFor();
  await expect(page.locator('html')).toHaveAttribute('data-palette','rose');
  await expect(page.locator('html')).toHaveAttribute('data-theme','light');
  await page.evaluate(id => window.molrDesktop.connect(id), one.id); await waitRuntime(page);
  assert.equal(await remote("fetch('/who').then(r => r.text())"), 'fixture_session=first', 'Persistent login survives desktop restart');
  await remote("localStorage.setItem('fixture', 'persisted')");
  checkpoint('restore connection and login automatically');
  await app.close();
  page = await launch(); await waitRuntime(page);
  assert.equal((await page.evaluate(() => window.molrDesktop.state())).active, one.id);
  assert.equal(await remote("fetch('/who').then(r => r.text())"), 'fixture_session=first');
  assert.equal(await remote("localStorage.getItem('fixture')"), 'persisted');
  if (process.platform === 'darwin') {
    checkpoint('restore after reopening macOS window');
    await closeWindow(page);
    [page] = await Promise.all([
      app.waitForEvent('window'),
      app.evaluate(({ app }) => app.emit('activate')),
    ]);
    await waitForVisibleWindow(app, page);
    await waitRuntime(page);
    assert.equal((await page.evaluate(() => window.molrDesktop.state())).active, one.id);
  }
  checkpoint('explicit disconnect persists across restart');
  const restoredPage = app.context().pages().find(p => p.url() === url);
  await Promise.all([
    restoredPage.waitForEvent('close'),
    restoredPage.getByRole('button', { name: 'Disconnect', exact: true }).click(),
  ]);
  await page.getByRole('heading', { name: '选择连接' }).waitFor();
  await app.close();
  page = await launch();
  await page.getByRole('heading', { name: '选择连接' }).waitFor();
  assert.equal((await page.evaluate(() => window.molrDesktop.state())).lastConnection, undefined);
  await page.evaluate(() => window.molrDesktop.manager());
  await page.evaluate(id => window.molrDesktop.connect(id), one.id); await waitRuntime(page);
  assert.equal(await remote("localStorage.getItem('fixture')"), 'persisted');
  await page.evaluate(() => window.molrDesktop.manager());
  await page.evaluate(() => window.molrDesktop.save({ name: 'Offline', url: 'http://127.0.0.1:1', allowHTTP: true }));
  const offline = (await page.evaluate(() => window.molrDesktop.state())).connections.find(c => c.name === 'Offline');
  await page.evaluate(id => window.molrDesktop.connect(id), offline.id);
  await page.getByRole('heading', { name: '暂时无法连接' }).waitFor();
  checkpoint('offline startup recovers without losing selection');
  await app.close();
  page = await launch();
  await page.getByRole('heading', { name: '暂时无法连接' }).waitFor();
  assert.equal((await page.evaluate(() => window.molrDesktop.state())).active, offline.id);
  await expect(page.getByRole('button', { name: '重新连接', exact: true })).toBeVisible();
  await page.getByRole('button', { name: '返回连接列表' }).click();
  checkpoint('theme screenshots');
  for (const theme of ['light', 'dark']) for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) {
    await page.evaluate(id => window.molrDesktop.connect(id), one.id); await waitRuntime(page);
    await remote(`window.molrApp.setAppearance(${JSON.stringify({theme,palette})})`);
    await page.evaluate(() => window.molrDesktop.manager());
    await page.reload(); await page.getByRole('heading', { name: '选择连接' }).waitFor();
    await expect(page.locator('html')).toHaveAttribute('data-palette',palette);
    await expect(page.locator('html')).toHaveAttribute('data-theme',theme);
    await page.screenshot({ path: join(screenshots, `${palette}-${theme}.png`) });
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
  }
  await page.evaluate(id => window.molrDesktop.connect(id), one.id); await waitRuntime(page);
  await remote('window.molrApp.setAppearance({ theme: "system", palette: "forest" })');
  await page.evaluate(() => window.molrDesktop.manager());
  await page.emulateMedia({colorScheme:'dark'});
  await expect(page.locator('html')).toHaveAttribute('data-theme','dark');
  await page.emulateMedia({colorScheme:'light'});
  await expect(page.locator('html')).toHaveAttribute('data-theme','light');
  assert.equal((await page.evaluate(() => window.molrDesktop.state())).appearance.theme,'system');
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setSize(640, 720));
  await page.screenshot({ path: join(screenshots, 'narrow.png') });
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
  console.log('Electron acceptance passed: isolated sessions, restart, SSE, remote privilege boundary, recovery, six palettes/light-dark and narrow layout.');
} catch (error) {
  console.error('Electron acceptance failed during:', phase, error);
  const windows = await app?.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows().map(win => ({
    visible: win.isVisible(), minimized: win.isMinimized(), focused: win.isFocused(), loading: win.webContents.isLoading(),
  }))).catch(() => undefined);
  writeFileSync(join(screenshots, 'failure.json'), JSON.stringify({ phase, error: String(error), windows }, null, 2));
  const page = app?.context().pages().find(page => !page.isClosed());
  if (page) await page.screenshot({path:join(screenshots,'failure.png'),timeout:5000}).catch(()=>{});
  throw error;
} finally {
  checkpoint('cleanup');
  await app?.close(); fixture.closeAllConnections(); await new Promise(resolve => fixture.close(resolve));
  rmSync(data, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
  clearTimeout(watchdog);
}

if (process.platform !== 'win32') await import('./updates-electron.mjs');
