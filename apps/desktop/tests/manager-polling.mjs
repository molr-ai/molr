import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';
const base = process.env.MOLR_MANAGER_PREVIEW ?? 'http://127.0.0.1:5420/tests/fixtures/manager-preview.html';
const browser = await chromium.launch({ chromiumSandbox: true, executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || chromium.executablePath() });
try {
  const page = await browser.newPage({ viewport: { width: 1180, height: 820 } });
  await page.goto(`${base}?scenario=list`);
  await expect(page.getByRole('heading', { name: '选择连接' })).toBeVisible();
  await page.clock.install({ time: new Date('2030-01-01T00:00:00Z') });
  await page.clock.pauseAt(new Date('2030-01-01T00:00:01Z'));
  await page.evaluate(() => {
    const fixture = window.pollingFixture = { calls: 0, inFlight: 0, maximum: 0, hold: false, fail: false, release: undefined, errors: 0 };
    const bridge = window.molrDesktop, refresh = bridge.refreshLocal, action = bridge.localAction;
    bridge.refreshLocal = async () => {
      fixture.calls++; fixture.inFlight++; fixture.maximum = Math.max(fixture.maximum, fixture.inFlight);
      try {
        if (fixture.hold) await new Promise(resolve => { fixture.release = resolve; });
        if (fixture.fail) throw new Error('Fixture status unavailable');
        await refresh();
      } finally { fixture.inFlight--; }
    };
    bridge.localAction = async (...args) => {
      const operation = action(...args);
      await new Promise(resolve => setTimeout(resolve, 20000));
      await operation;
    };
    new MutationObserver(records => {
      for (const record of records) for (const node of record.addedNodes) if (node instanceof Element) {
        if (node.matches('.ui-toast--danger')) fixture.errors++;
        fixture.errors += node.querySelectorAll('.ui-toast--danger').length;
      }
    }).observe(document.body, { childList: true, subtree: true });
  });
  const calls = () => page.evaluate(() => window.pollingFixture.calls);
  async function enter() {
    await page.getByRole('button', { name: '这台电脑的操作', exact: true }).click();
    await page.getByRole('menuitem', { name: '管理本机服务' }).click();
  }
  await page.clock.runFor(10000); assert.equal(await calls(), 0, 'No polling in the connection list');
  await enter(); await expect.poll(calls).toBe(1);
  await expect(page.getByRole('button', { name: '刷新状态', exact: true })).toHaveCount(0);
  await page.clock.runFor(4999); assert.equal(await calls(), 1);
  await page.clock.runFor(1); await expect.poll(calls).toBe(2);
  await page.evaluate(() => { window.pollingFixture.hold = true; });
  await page.clock.runFor(5000); await expect.poll(calls).toBe(3);
  await page.clock.runFor(20000); assert.equal(await calls(), 3, 'Slow checks must not overlap');
  await page.getByRole('button', { name: '返回连接列表' }).click();
  await page.clock.runFor(10000); assert.equal(await calls(), 3);
  await enter(); await page.clock.runFor(10000); assert.equal(await calls(), 3, 'Re-entry must not overlap the unfinished check');
  await page.evaluate(() => { window.pollingFixture.hold = false; window.pollingFixture.release(); });
  await expect.poll(() => page.evaluate(() => window.pollingFixture.inFlight)).toBe(0);
  await page.clock.runFor(5000); await expect.poll(calls).toBe(4);
  await page.getByRole('button', { name: '停止服务', exact: true }).click();
  await page.clock.runFor(15000); assert.equal(await calls(), 4, 'Pause throughout maintenance');
  await page.clock.runFor(5000); await expect.poll(calls).toBe(5);
  await expect(page.getByRole('button', { name: '启动并连接', exact: true })).toBeVisible();
  await page.evaluate(() => { window.pollingFixture.fail = true; });
  await page.clock.runFor(5000); await expect.poll(() => page.evaluate(() => window.pollingFixture.errors)).toBe(1);
  await page.clock.runFor(15000); assert.equal(await page.evaluate(() => window.pollingFixture.errors), 1, 'Repeated failures do not repeat notifications');
  assert.equal(await page.evaluate(() => window.pollingFixture.maximum), 1);
  await page.evaluate(() => window.molrDesktop.connect('local'));
  await expect(page.getByRole('heading', { name: '暂时无法连接' })).toBeVisible();
  const before = await calls(); await page.clock.runFor(15000); assert.equal(await calls(), before, 'Stop polling outside the manager view');
  console.log('Manager polling passed: entry, 5s cadence, slow requests, navigation, maintenance, failure deduplication and connection transitions.');
} finally { await browser.close(); }
