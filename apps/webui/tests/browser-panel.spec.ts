import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const port = process.env.MOLR_TEST_PORT ?? '5193';
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: `http://127.0.0.1:${port}`, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(response.ok(), await response.text()).toBeTruthy(); return response.json();
}

// Authentication/chat use the real Runtime. Browser transport timing is fixed
// here; native actions, metadata and controller isolation have Go integration tests.
test('browser panel preserves control, commits IME once, closes the selected tab and fills the page', async ({ page }, info) => {
  if (info.project.name === 'desktop') await page.setViewportSize({ width: 1920, height: 1000 });
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Browser ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: { ResourceID: string }) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  let lastFailure: { TaskID: string; Reason: string } | undefined;
  let client = '', generation = 1, sequence = 0, takeovers = 0, running = false, rejectInput = false;
  let tabs = [{ tabId: 't1', title: 'First tab', url: 'https://example.com', active: true }, { tabId: 't2', title: 'Second tab', url: 'about:blank', active: false }];
  const text: string[] = [], closedTabs: string[] = [];
  await page.addInitScript(() => {
    const Native = window.EventSource;
    class BrowserStream {
      onopen?: () => void; onmessage?: (event: { data: string }) => void;
      listener = (event: Event) => this.onmessage?.({ data: JSON.stringify({ type: 'tabs', tabs: (event as CustomEvent).detail }) });
      constructor() { window.addEventListener('browser-test-tabs', this.listener); setTimeout(() => this.onopen?.(), 20); }
      close() { window.removeEventListener('browser-test-tabs', this.listener); }
    }
    window.EventSource = new Proxy(Native, { construct(target, args) { return String(args[0]).includes('/browser/stream') ? new BrowserStream() : Reflect.construct(target, args); } });
  });
  await page.goto('about:blank'); // Install the transport in a fresh document before routing requests.
  await page.route('**/api/resources?kind=compute&limit=200', route => route.fulfill({ json: { Items: [{ ID: 'local-one', Name: 'Local one' }, { ID: 'local-two', Name: 'Local two' }] } }));
  await page.route(`**/api/grants?assistant_id=${assistant.ID}&limit=200`, route => route.fulfill({ json: { Items: ['local-one', 'local-two'].map(ResourceID => ({ ResourceID, Scope: {}, Actions: ['execute'] })) } }));
  await page.route(`**/api/assistants/${assistant.ID}/browser`, async route => {
    if (route.request().method() === 'POST') {
      const q = route.request().postDataJSON();
      if (rejectInput) { rejectInput = false; client = ''; generation++; await route.fulfill({ status: 409, json: { code: 'conflict' } }); return; }
      if (q.action === 'start') { expect(q.compute_id).toBe('local-two'); running = true; }
      else if (q.action === 'takeover') { client = q.client_id; generation++; takeovers++; sequence = q.sequence; }
      else {
        expect(q.client_id).toBe(client); expect(q.generation).toBe(generation); expect(q.sequence).toBeGreaterThan(sequence); sequence = q.sequence;
        if (q.action === 'insert_text') text.push(q.text);
        if (q.action === 'close_tab') { closedTabs.push(q.tab); tabs = tabs.filter(t => t.tabId !== q.tab); }
      }
    }
    await route.fulfill({ json: { Available: true, LastFailure: lastFailure, TabCount: tabs.length, ControllerIsCurrentSession: !!client, ControllerClientID: client, Task: running ? { ID: 'browser-test', Kind: 'browser', State: 'running', Browser: { Control: client ? 'user' : 'assistant', Generation: generation } } : null } });
    await page.evaluate(tabs => window.dispatchEvent(new CustomEvent('browser-test-tabs', { detail: tabs })), tabs);
  });
  await page.goto(`/#/assistants/${assistant.ID}`);
  const trigger = page.getByRole('button', { name: /^Browser(?: \(\d+\))?$/ });
  await expect(trigger).toHaveAccessibleName('Browser');
  const iconPosition = () => trigger.evaluate(el => { const r = el.getBoundingClientRect(), icon = el.querySelector('svg')!.getBoundingClientRect(); return { width: r.width, x: icon.x-r.x, y: icon.y-r.y }; });
  const bareIcon = await iconPosition();
  await trigger.click();
  await expect(page.locator('.browser-drawer .ui-dialog__header h2')).not.toHaveClass(/ui-sr-only/);
  await expect(page.getByRole('tablist', { name: 'Browser tabs' })).toHaveCount(0);
  expect(await page.locator('.browser-panel').evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgba(0, 0, 0, 0)');
  const resourceSelect = page.getByRole('combobox', { name: 'Compute resource' });
  await expect(resourceSelect).toHaveText('Local one'); await resourceSelect.click();
  await page.getByRole('option', { name: 'Local two' }).click();
  await page.getByRole('button', { name: 'Start browser', exact: true }).click();
  await expect(trigger).toHaveAccessibleName('Browser (2)');
  expect(await iconPosition()).toEqual(bareIcon);
  const badge = trigger.locator('.badge');
  await expect(badge).toHaveCSS('position', 'absolute');
  await expect(badge).toHaveCSS('font-size', '9px');
  await page.getByRole('button', { name: 'Take control', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Return control', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: 'Close browser panel', exact: true }).click();
  await expect(page.locator('.browser-drawer')).toHaveCount(0); await trigger.click();
  await expect(page.getByRole('button', { name: 'Return control', exact: true })).toBeEnabled(); expect(takeovers).toBe(1);
  await expect(page.locator('.browser-drawer .browser-navigation .browser-control-button')).toHaveText('Return control');
  await expect(page.locator('.browser-drawer .ui-dialog__header h2')).toHaveClass(/ui-sr-only/);
  const input = page.getByRole('textbox', { name: 'Page keyboard input', exact: true });
  await input.focus(); await input.dispatchEvent('compositionstart', { data: '' });
  await input.evaluate((element: HTMLTextAreaElement) => { element.value = '中文组合'; element.dispatchEvent(new InputEvent('input', { bubbles: true, data: '中文组合', isComposing: true })); });
  expect(text).toEqual([]); await expect(page.locator('.browser-preedit')).toHaveCount(0); await input.dispatchEvent('compositionend', { data: '中文组合' });
  await expect.poll(() => text).toEqual(['中文组合']);
  await page.getByRole('button', { name: 'Close tab: New tab', exact: true }).click();
  await expect.poll(() => closedTabs).toEqual(['t2']); await expect(trigger).toHaveAccessibleName('Browser (1)');
  // Freeze only the browser's geometry animation to inspect its actual start,
  // midpoint and final bounds in both directions, independently of timing.
  await page.evaluate(() => {
    const animate = Element.prototype.animate;
    Element.prototype.animate = function(frames, options) {
      const motion = animate.call(this, frames, options);
      if (typeof options === 'object' && options?.id === 'molr-browser-view') { motion.pause(); motion.currentTime = 0; }
      return motion;
    };
  });
  const panel = page.locator('.browser-drawer');
  const normal = (await panel.boundingBox())!;
  await page.getByRole('button', { name: 'Full-page view', exact: true }).click();
  if (info.project.name === 'desktop') {
    expect(await panel.evaluate(el => getComputedStyle(el, '::backdrop').backgroundColor)).toBe('rgba(0, 0, 0, 0)');
    expect(await panel.evaluate(el => getComputedStyle(el, '::backdrop').backdropFilter)).toBe('none');
  }
  const start = (await panel.boundingBox())!;
  expect(start.x).toBeCloseTo(normal.x, 0); expect(start.y).toBeCloseTo(normal.y, 0);
  expect(start.width).toBeCloseTo(normal.width, 0); expect(start.height).toBeCloseTo(normal.height, 0);
  const finish = async () => panel.evaluate(el => { const animation = el.getAnimations().find(a => a.id === 'molr-browser-view'); if (!animation) throw Error('missing geometry animation'); animation.finish(); });
  await finish();
  await expect(page.getByRole('button', { name: 'Exit full-page view', exact: true })).toBeEnabled();
  const box = (await panel.boundingBox())!, viewport = page.viewportSize()!;
  expect(Math.round(box.x)).toBe(0); expect(Math.round(box.y)).toBe(0);
  expect(Math.round(box.width)).toBe(viewport.width); expect(Math.round(box.height)).toBe(viewport.height);
  expect(await page.evaluate(() => document.fullscreenElement)).toBeNull();
  await page.getByRole('button', { name: 'Exit full-page view', exact: true }).click();
  const collapsing = (await panel.boundingBox())!;
  expect(collapsing.width).toBeCloseTo(box.width, 0); expect(collapsing.height).toBeCloseTo(box.height, 0);
  await panel.evaluate(el => { el.getAnimations().find(a => a.id === 'molr-browser-view')!.currentTime = 130; });
  const middle = (await panel.boundingBox())!;
  expect(info.project.name === 'desktop' ? middle.width : middle.height).toBeLessThan(info.project.name === 'desktop' ? box.width : box.height);
  await finish();
  await expect(page.locator('.browser-drawer--expanded')).toHaveCount(0);
  const restored = (await panel.boundingBox())!;
  expect(restored.x).toBeCloseTo(normal.x, 0); expect(restored.y).toBeCloseTo(normal.y, 0);
  expect(restored.width).toBeCloseTo(normal.width, 0); expect(restored.height).toBeCloseTo(normal.height, 0);
  await expect(page.getByRole('button', { name: 'Return control', exact: true })).toBeEnabled(); expect(takeovers).toBe(1);
  tabs = Array.from({length: 24}, (_, i) => ({tabId: `t${i+1}`, title: `Tab ${i+1}`, url: `https://example.com/${i}`, active: i===0}));
  await page.evaluate(tabs => window.dispatchEvent(new CustomEvent('browser-test-tabs', {detail: tabs})), tabs);
  const list = page.getByRole('tablist', {name: 'Browser tabs'});
  await expect(page.getByRole('button', {name:'Scroll tabs right'})).toBeEnabled();
  expect(await list.evaluate(el => getComputedStyle(el).overflowX)).toBe('hidden');
  await page.getByRole('button', {name:'Scroll tabs right'}).click();
  await expect.poll(() => list.evaluate(el => el.scrollLeft)).toBeGreaterThan(0);
  await expect(page.getByRole('button', {name:'Scroll tabs left'})).toBeEnabled();
  const inactive = page.locator('.browser-tab:not([data-active])').first();
  expect(await inactive.evaluate(el => getComputedStyle(el).borderTopStyle)).toBe('solid');
  expect(await inactive.evaluate(el => parseFloat(getComputedStyle(el).borderTopWidth))).toBeGreaterThan(0);
  const tabBox = (await inactive.boundingBox())!, closeBox = (await inactive.locator('.ui-icon-button').boundingBox())!;
  expect(closeBox.height).toBeLessThan(tabBox.height - 8);
  expect(closeBox.y).toBeGreaterThan(tabBox.y);
  // A rejected stale input refreshes ownership and reports a dismissible toast.
  rejectInput = true;
  await page.getByRole('button', { name: 'Reload', exact: true }).click();
  const toast = page.locator('.ui-toast').filter({ hasText: 'Control changed. Take control again.' });
  await expect(toast).toBeVisible();
  await expect(page.getByRole('button', { name: 'Take control', exact: true })).toBeEnabled();
  await expect(page.getByRole('textbox', { name: 'Address', exact: true })).toBeDisabled();
  await toast.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(toast).toHaveCount(0);
  await expect(page.locator('.browser-notice')).toHaveCount(0);

  // Fast failures can happen between polls; the persisted result still produces
  // a dismissible notice, and polling/reopening must not resurrect it.
  running = false; lastFailure = { TaskID: 'failed-process-limit', Reason: 'process_limit' };
  const failureNotice = page.getByText('Browser stopped after reaching the authorized process/thread limit. Adjust the process limit in compute permissions and retry.', { exact: true });
  await expect(failureNotice).toBeVisible();
  const failureToast = page.locator('.ui-toast').filter({ hasText: 'Browser stopped after reaching the authorized process/thread limit.' });
  await failureToast.getByRole('button', { name: 'Close', exact: true }).click();
  await page.waitForTimeout(2300); await expect(failureNotice).toHaveCount(0);
  await page.getByRole('button', { name: 'Close browser panel', exact: true }).click(); await trigger.click();
  await page.waitForTimeout(2300); await expect(failureNotice).toHaveCount(0);
  lastFailure = { TaskID: 'failed-unknown', Reason: 'unknown' };
  await expect(page.getByText(/Browser exited unexpectedly. Check the browser task/)).toBeVisible();

  lastFailure = undefined; running = true;
  await expect(page.getByText(/Browser exited unexpectedly. Check the browser task/)).toHaveCount(0);
  await page.unrouteAll({behavior:'wait'});
});
