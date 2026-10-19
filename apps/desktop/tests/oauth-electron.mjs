import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, rmSync } from 'node:fs';
import { tmpdir, networkInterfaces } from 'node:os';
import { join, resolve } from 'node:path';
import { _electron as electron, expect } from '@playwright/test';
import { oauthFixture } from './oauth-fixture.mjs';

const root = resolve(import.meta.dirname, '../../..'), serve = process.argv.includes('--serve');
const lan = Object.values(networkInterfaces()).flat().find(a => a.family === 'IPv4' && !a.internal)?.address;
const fixture = await oauthFixture({ host: '0.0.0.0', port: serve ? Number(process.env.MOLR_OAUTH_DEMO_PORT || 5496) : 0 });
const data = mkdtempSync(join(tmpdir(), 'molr-oauth-electron-'));
let app, pendingURL;
const env = { ...process.env, MOLR_DESKTOP_DATA: data };
delete env.MOLR_DESKTOP_RUNTIME; delete env.MOLR_DESKTOP_DEV_URL; delete env.ELECTRON_RUN_AS_NODE;
const screenshots = join(root, '.cache/acceptance/desktop-oauth'); mkdirSync(screenshots, { recursive: true });
try {
  app = await electron.launch({ args: [join(root, 'apps/desktop'), ...(process.argv.includes('--headless') ? ['--ozone-platform=headless'] : [])], env, chromiumSandbox: true });
  const manager = await app.firstWindow();
  // Test-only OS boundary mocks. No production env switch bypasses confirmation
  // or provider HTTPS; the real bridge, cookies and callback socket still run.
  await app.evaluate(({ dialog, shell }) => {
    globalThis.oauthOpened = [];
    dialog.showMessageBox = async () => ({ response: 1, checkboxChecked: false });
    shell.openExternal = async url => { globalThis.oauthOpened.push(url); };
  });
  async function connect(host) {
    const url = `http://${host}:${fixture.port}`;
    await manager.evaluate(async url => {
      await window.molrDesktop.manager();
      await window.molrDesktop.save({ name: 'OAuth fixture', url, allowHTTP: true });
      const connections = (await window.molrDesktop.state()).connections;
      await window.molrDesktop.connect(connections.at(-1).id);
    }, url);
    await expect.poll(() => manager.evaluate(async () => (await window.molrDesktop.state()).view)).toBe('runtime');
    return app.context().pages().find(p => p.url() === url + '/');
  }
  async function opened() {
    let raw;
    await expect.poll(async () => { raw = await app.evaluate(() => globalThis.oauthOpened.shift()); return !!raw; }).toBe(true);
    return raw;
  }
  let page;
  for (const host of ['127.0.0.1', ...(lan ? [lan] : [])]) {
    page = await connect(host);
    await page.locator('#authorize').click();
    const raw = await opened();
    assert.equal(await fixture.approve(raw), 200);
    await expect(page.locator('#result')).toContainText('connected');
    const result = await page.evaluate(() => window.result);
    assert.deepEqual(Object.keys(result).sort(), ['attemptId', 'connectionId', 'status']);
    assert(fixture.hasCredentials);
    assert(!(await page.content()).includes('synthetic-access-token'));
  }
  const exchanged = fixture.exchanges;
  await page.locator('#authorize').click();
  let raw = await opened();
  await fixture.approve(raw, true);
  await expect(page.locator('#result')).toContainText('cancelled');
  assert.equal(fixture.exchanges, exchanged);
  await page.locator('#authorize').click(); raw = await opened();
  await page.locator('#cancel').click();
  await expect(page.locator('#result')).toContainText('cancelled');
  await assert.rejects(fixture.approve(raw));
  await page.locator('#authorize').click(); raw = await opened(); fixture.rotateSession();
  await fixture.approve(raw);
  await expect(page.locator('#result')).toContainText('unknown');
  assert.equal(fixture.exchanges, exchanged);
  await page.locator('#authorize').click(); raw = await opened();
  await manager.evaluate(() => window.molrDesktop.manager());
  await assert.rejects(fixture.approve(raw));
  assert.equal(fixture.exchanges, exchanged);
  page = await connect(lan ?? '127.0.0.1');
  await page.locator('#authorize').click(); await fixture.approve(await opened());
  await expect(page.locator('#result')).toContainText('connected');
  await page.screenshot({ path: join(screenshots, 'connected.png') });
  console.log('OAuth native acceptance passed: loopback and LAN HTTP, PKCE exchange, cancellation, denial, account switching, disconnect, token isolation.');
  if (serve) {
    fixture.setControl(async (req, res, url) => {
      const json = value => res.end(JSON.stringify(value));
      if (req.method === 'GET' && url.pathname === '/control') {
        res.setHeader('Content-Type', 'text/html; charset=utf-8');
        return res.end('<!doctype html><meta charset="utf-8"><h1>桌面 OAuth 模拟验收</h1><p>连接独立 Electron 实例；仅使用人工凭据。原生确认和浏览器打开由测试夹具模拟。</p><button onclick="run(\'start\')">1. 开始授权</button> <button onclick="run(\'approve\')">2. 同意授权</button> <button onclick="run(\'deny\')">拒绝</button> <button onclick="run(\'cancel\')">取消</button><pre id="status"></pre><script>async function run(action){await fetch("/control/"+action,{method:"POST"})}setInterval(async()=>{document.querySelector("#status").textContent=JSON.stringify(await(await fetch("/control/status")).json(),null,2)},1000)</script>');
      }
      if (url.pathname === '/control/status') return json({ result: await page.locator('#result').textContent(), exchanges: fixture.exchanges, credentialsSavedInFakeRuntime: fixture.hasCredentials, awaitingConsent: !!pendingURL });
      if (req.method !== 'POST' || req.headers.origin !== url.origin) { res.writeHead(403); return json({}); }
      if (url.pathname === '/control/start' && !pendingURL) { await page.locator('#authorize').click(); pendingURL = await opened(); }
      if (url.pathname === '/control/cancel') { await page.locator('#cancel').click(); pendingURL = undefined; }
      if (['/control/approve', '/control/deny'].includes(url.pathname) && pendingURL) { await fixture.approve(pendingURL, url.pathname.endsWith('/deny')); pendingURL = undefined; }
      json({ ok: true });
    });
    console.log(`Acceptance: http://${lan ?? '127.0.0.1'}:${fixture.port}/control`);
    await new Promise(resolve => { process.once('SIGTERM', resolve); process.once('SIGINT', resolve); });
  }
} finally {
  await app?.close(); await fixture.close(); rmSync(data, { recursive: true, force: true });
}
