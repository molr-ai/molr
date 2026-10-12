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
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Browser ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: { ResourceID: string }) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  let client = '', generation = 1, sequence = 0, takeovers = 0;
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
  await page.route(`**/api/assistants/${assistant.ID}/browser`, async route => {
    if (route.request().method() === 'POST') {
      const q = route.request().postDataJSON();
      if (q.action === 'takeover') { client = q.client_id; generation++; takeovers++; sequence = q.sequence; }
      else {
        expect(q.client_id).toBe(client); expect(q.generation).toBe(generation); expect(q.sequence).toBeGreaterThan(sequence); sequence = q.sequence;
        if (q.action === 'insert_text') text.push(q.text);
        if (q.action === 'close_tab') { closedTabs.push(q.tab); tabs = tabs.filter(t => t.tabId !== q.tab); }
      }
    }
    await route.fulfill({ json: { Available: true, TabCount: tabs.length, ControllerIsCurrentSession: !!client, ControllerClientID: client, Task: { ID: 'browser-test', Kind: 'browser', State: 'running', Browser: { Control: client ? 'user' : 'assistant', Generation: generation } } } });
    await page.evaluate(tabs => window.dispatchEvent(new CustomEvent('browser-test-tabs', { detail: tabs })), tabs);
  });
  await page.goto(`/#/assistants/${assistant.ID}`);
  await page.reload(); // Hash navigation alone does not install the transport fixture.
  const trigger = page.getByRole('button', { name: /^Browser \(/ });
  await expect(trigger).toHaveAccessibleName('Browser (2)'); await trigger.click();
  await page.getByRole('button', { name: 'Take control', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Return control', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: 'Close browser panel', exact: true }).click();
  await expect(page.locator('.browser-drawer')).toHaveCount(0); await trigger.click();
  await expect(page.getByRole('button', { name: 'Return control', exact: true })).toBeEnabled(); expect(takeovers).toBe(1);
  const input = page.getByRole('textbox', { name: 'Page keyboard input', exact: true });
  await input.focus(); await input.dispatchEvent('compositionstart', { data: '' });
  await input.evaluate((element: HTMLTextAreaElement) => { element.value = '中文组合'; element.dispatchEvent(new InputEvent('input', { bubbles: true, data: '中文组合', isComposing: true })); });
  expect(text).toEqual([]); await input.dispatchEvent('compositionend', { data: '中文组合' });
  await expect.poll(() => text).toEqual(['中文组合']);
  await page.getByRole('button', { name: 'Close tab: New tab', exact: true }).click();
  await expect.poll(() => closedTabs).toEqual(['t2']); await expect(trigger).toHaveAccessibleName('Browser (1)');
  await page.getByRole('button', { name: 'Full-page view', exact: true }).click();
  await expect.poll(async () => Math.round((await page.locator('.browser-drawer--expanded').boundingBox())!.x)).toBe(0);
  const box = (await page.locator('.browser-drawer--expanded').boundingBox())!, viewport = page.viewportSize()!;
  expect(Math.round(box.width)).toBe(viewport.width); expect(Math.round(box.height)).toBe(viewport.height);
  expect(await page.evaluate(() => document.fullscreenElement)).toBeNull();
  await page.getByRole('button', { name: 'Exit full-page view', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Return control', exact: true })).toBeEnabled(); expect(takeovers).toBe(1);
});
