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
test('browser panel preserves control, commits IME once, closes the selected tab and preserves proportional floating geometry', async ({ page }, info) => {
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
  let proxyMode = 'proxy';
  let lastFailure: { TaskID: string; Reason: string } | undefined;
  let client = '', generation = 1, sequence = 0, takeovers = 0, running = false, rejectInput = false;
  let tabs = [{ tabId: 't1', title: 'First tab', url: 'https://example.com', active: true }, { tabId: 't2', title: 'Second tab', url: 'about:blank', active: false }];
  const text: string[] = [], closedTabs: string[] = [];
  let inputDelay = 0, batches = 0, grantReads = 0, partialNext = false;
  const inputs: Record<string, any>[] = [];
  const pointers: { x: number; y: number; event: string }[] = [];
  await page.addInitScript(() => {
    const Native = window.EventSource;
    const metrics = (window as any).__browserMetrics = { streams: 0, frames: 0, draws: 0, resets: 0 };
    const draw = CanvasRenderingContext2D.prototype.drawImage;
    CanvasRenderingContext2D.prototype.drawImage = function(...args: any[]) { if (this.canvas.matches('[aria-label="Live browser view"]')) metrics.draws++; return (draw as any).apply(this, args); };
    for (const name of ['width', 'height']) {
      const descriptor = Object.getOwnPropertyDescriptor(HTMLCanvasElement.prototype, name)!;
      Object.defineProperty(HTMLCanvasElement.prototype, name, { ...descriptor, set(value) { if (this.matches('[aria-label="Live browser view"]')) metrics.resets++; descriptor.set!.call(this, value); } });
    }
    class BrowserStream {
      onopen?: () => void; onmessage?: (event: { data: string }) => void; onerror?: () => void;
      ended = false;
      timer?: ReturnType<typeof setTimeout>; frames?: ReturnType<typeof setInterval>;
      listener = (event: Event) => this.onmessage?.({ data: JSON.stringify({ type: 'tabs', tabs: (event as CustomEvent).detail }) });
      fail = () => this.onerror?.();
      constructor() {
        metrics.streams++;
        window.addEventListener('browser-test-tabs', this.listener);
        window.addEventListener('browser-test-error', this.fail);
        this.timer = setTimeout(() => {
          this.onopen?.();
          const frame = document.createElement('canvas'); frame.width = 1280; frame.height = 800;
          const data = frame.toDataURL('image/jpeg').split(',')[1];
          const send = () => { metrics.frames++; this.onmessage?.({ data: JSON.stringify({ type: 'frame', data }) }); };
          send(); this.frames = setInterval(send, 100);
        }, 20);
      }
      close() {
        if (this.ended) return;
        this.ended = true; metrics.streams--;
        clearTimeout(this.timer); clearInterval(this.frames);
        window.removeEventListener('browser-test-tabs', this.listener);
        window.removeEventListener('browser-test-error', this.fail);
      }
    }
    window.EventSource = new Proxy(Native, { construct(target, args) { return String(args[0]).includes('/browser/stream') ? new BrowserStream() : Reflect.construct(target, args); } });
  });
  await page.goto('about:blank'); // Install the transport in a fresh document before routing requests.
  await page.route('**/api/resources?kind=compute&limit=200', route => route.fulfill({ json: { Items: [{ ID: 'local-one', Name: 'Local one' }, { ID: 'local-two', Name: 'Local two' }] } }));
  await page.route(`**/api/grants?assistant_id=${assistant.ID}&limit=200`, route => { grantReads++; return route.fulfill({ json: { Items: ['local-one', 'local-two'].map(ResourceID => ({ ResourceID, Scope: {}, Actions: ['execute'] })) } }); });
  await page.route(`**/api/assistants/${assistant.ID}/browser`, async route => {
    if (route.request().method() === 'POST') {
      const q = route.request().postDataJSON();
      if (rejectInput) { rejectInput = false; client = ''; generation++; await route.fulfill({ status: 409, json: { code: 'conflict' } }); return; }
      if (q.action === 'start') { expect(q.compute_id).toBe('local-two'); running = true; }
      else if (q.action === 'takeover') { client = q.client_id; generation++; takeovers++; sequence = q.sequence; }
      else {
        expect(q.client_id).toBe(client); expect(q.generation).toBe(generation); expect(q.sequence).toBeGreaterThan(sequence); sequence = q.sequence;
        let actions = q.action === 'input_batch' ? q.actions : [q];
        if (q.action === 'input_batch' && partialNext) { actions = actions.slice(0, 1); partialNext = false; }
        for (const a of actions) {
          inputs.push(a);
          if (a.action === 'insert_text') text.push(a.text);
          if (a.action === 'pointer') pointers.push(a);
          if (a.action === 'close_tab') { closedTabs.push(a.tab); tabs = tabs.filter(t => t.tabId !== a.tab); }
        }
        if (q.action === 'input_batch') {
          batches++; sequence += actions.length - 1;
          if (inputDelay) await new Promise(resolve => setTimeout(resolve, inputDelay));
          await route.fulfill({ json: { completed: actions.length } }); return;
        }
      }
    }
    await route.fulfill({ json: { Available: true, Proxy: { Mode: proxyMode }, ProxyAppliesTo: running ? 'current_session' : 'next_start', RestartRequired: running, LastFailure: lastFailure, TabCount: tabs.length, ControllerIsCurrentSession: !!client, ControllerClientID: client, Task: running ? { ID: 'browser-test', Kind: 'browser', State: 'running', Browser: { Control: client ? 'user' : 'assistant', Generation: generation } } : null } });
    await page.evaluate(tabs => window.dispatchEvent(new CustomEvent('browser-test-tabs', { detail: tabs })), tabs);
  });
  await page.goto(`/#/assistants/${assistant.ID}`);
  const trigger = page.getByRole('button', { name: /^Browser(?: \(\d+\))?$/ });
  await expect(trigger).toHaveAccessibleName('Browser');
  const iconPosition = () => trigger.evaluate(el => { const r = el.getBoundingClientRect(), icon = el.querySelector('svg')!.getBoundingClientRect(); return { width: r.width, x: icon.x-r.x, y: icon.y-r.y }; });
  const bareIcon = await iconPosition();
  await trigger.click();
  await expect(page.locator('.browser-window-heading')).toContainText('Browser');
  await expect(page.locator('.browser-proxy-badge')).toHaveCount(0);
  await expect(page.getByText(/Network settings/)).toHaveCount(0);
  await expect(page.getByRole('tablist', { name: 'Browser tabs' })).toHaveCount(0);
  expect(await page.locator('.browser-panel').evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgba(0, 0, 0, 0)');
  const resourceSelect = page.getByRole('combobox', { name: 'Compute resource' });
  await expect(resourceSelect).toHaveText('Local one'); await resourceSelect.click();
  await page.getByRole('option', { name: 'Local two' }).click();
  await page.getByRole('button', { name: 'Start browser', exact: true }).click();
  await expect(trigger).toHaveAccessibleName('Browser (2)');
  const proxyBadge = page.locator('.browser-drawer .ui-dialog__header .browser-proxy-badge');
  await expect(proxyBadge).toHaveText('Proxy enabled');
  proxyMode = 'direct';
  await expect(proxyBadge).toHaveCount(0); // A pending proxy configuration must not label a direct session.
  await expect(page.getByText(/Network settings/)).toHaveCount(0);
  proxyMode = 'proxy';
  await expect(proxyBadge).toBeVisible();
  expect(await iconPosition()).toEqual(bareIcon);
  const badge = trigger.locator('.badge');
  await expect(badge).toHaveCSS('position', 'absolute');
  await expect(badge).toHaveCSS('font-size', '9px');
  await page.getByRole('button', { name: 'Take control', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Return control', exact: true })).toBeEnabled();
  const metrics = () => page.evaluate(() => ({ ...(window as any).__browserMetrics }));
  await expect.poll(async () => (await metrics()).draws).toBeGreaterThan(2);
  const firstFrames = await metrics();
  await expect.poll(async () => (await metrics()).draws).toBeGreaterThan(firstFrames.draws + 2);
  expect((await metrics()).resets).toBe(firstFrames.resets); // Same-size frames keep the backing store.
  const visibility = (value: 'visible' | 'hidden') => page.evaluate(value => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, value });
    document.dispatchEvent(new Event('visibilitychange'));
  }, value);
  await visibility('hidden');
  await expect.poll(async () => (await metrics()).streams).toBe(0);
  const paused = await metrics();
  await page.waitForTimeout(350); expect(await metrics()).toEqual(paused);
  await visibility('visible');
  await expect.poll(async () => (await metrics()).streams).toBe(1);
  await expect(page.getByRole('button', { name: 'Return control', exact: true })).toBeEnabled();
  expect(takeovers).toBe(1);
  // A pending reconnect must not outlive hiding or unmounting the panel.
  await page.evaluate(() => window.dispatchEvent(new Event('browser-test-error')));
  await visibility('hidden'); await page.waitForTimeout(2100);
  expect((await metrics()).streams).toBe(0);
  await visibility('visible');
  await expect(page.getByRole('button', { name: 'Return control', exact: true })).toBeEnabled();
  for (let i = 0; i < 5; i++) {
    await page.getByRole('button', { name: 'Close browser panel', exact: true }).click();
    await expect(page.locator('.browser-drawer')).toHaveCount(0);
    expect((await metrics()).streams).toBe(0);
    const stopped = await metrics(); await page.waitForTimeout(150); expect(await metrics()).toEqual(stopped);
    await trigger.click();
    await expect(page.getByRole('button', { name: 'Return control', exact: true })).toBeEnabled();
    expect((await metrics()).streams).toBe(1);
  }

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
  // A slow round trip must not turn a wheel/move burst into hundreds of
  // requests or drop rapid text across a keyboard focus boundary.
  inputDelay = 200;
  const beforeBatches = batches, beforeInputs = inputs.length, beforeGrants = grantReads;
  await page.getByLabel('Live browser view').evaluate(canvas => {
    for (let i = 0; i < 150; i++) canvas.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, clientX: 150 + i, clientY: 200 }));
    for (let i = 0; i < 150; i++) canvas.dispatchEvent(new WheelEvent('wheel', { bubbles: true, clientX: 300, clientY: 200, deltaY: 2 }));
    const input = document.querySelector<HTMLTextAreaElement>('.browser-direct-input')!;
    const setValue = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!;
    for (const ch of 'abcdefghijklmnopqrst') { setValue.call(input, ch); input.dispatchEvent(new InputEvent('input', { bubbles: true, data: ch })); }
    input.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Tab' }));
    setValue.call(input, '第二个字段'); input.dispatchEvent(new InputEvent('input', { bubbles: true, data: input.value }));
  });
  await expect.poll(() => text.at(-1)).toBe('第二个字段');
  expect(text.slice(1, -1).join('')).toBe('abcdefghijklmnopqrst');
  const burst = inputs.slice(beforeInputs);
  expect(burst.filter(a => a.action === 'wheel').reduce((n, a) => n + a.delta_y, 0)).toBe(300);
  expect(burst.filter(a => a.action === 'pointer').length).toBeLessThanOrEqual(2);
  expect(burst.slice(-2).map(a => a.action)).toEqual(['press', 'insert_text']);
  expect(batches - beforeBatches).toBeLessThanOrEqual(3);
  expect(grantReads).toBe(beforeGrants);
  await expect(page.getByText('Input is busy; please wait.', { exact: true })).toHaveCount(0);
  inputDelay = 0;
  partialNext = true;
  await input.evaluate(element => {
    const set = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!;
    for (const ch of ['1', '2', '3']) { set.call(element, ch); element.dispatchEvent(new InputEvent('input', { bubbles: true, data: ch })); }
  });
  await expect.poll(() => text.slice(-3)).toEqual(['1', '2', '3']);
  expect(text.filter(ch => ['1', '2', '3'].includes(ch))).toEqual(['1', '2', '3']);
  await page.getByRole('button', { name: 'Close tab: New tab', exact: true }).click();
  await expect.poll(() => closedTabs).toEqual(['t2']); await expect(trigger).toHaveAccessibleName('Browser (1)');
  const panel = page.locator('.browser-drawer');
  const canvas = page.getByLabel('Live browser view');
  if (info.project.name === 'desktop') {
    await expect(panel).toHaveClass(/browser-drawer--floating/);
    await expect(page.locator('.chat-side-panel')).not.toHaveAttribute('data-docked');
    expect(await panel.evaluate(el => el.matches(':modal'))).toBe(false);
    const checkRatio = async () => {
      const image = (await canvas.boundingBox())!;
      expect(image.width / image.height).toBeCloseTo(1.6, 2);
    };
    await checkRatio();
    const before = (await panel.boundingBox())!;
    const grip = page.getByRole('button', { name: 'Move window (arrow keys to adjust)' });
    await grip.hover(); await page.mouse.down(); await page.mouse.move(before.x - 150, before.y + 140, { steps: 5 }); await page.mouse.up();
    await expect(grip).toBeFocused();
    await page.keyboard.press('ArrowLeft');
    const moved = (await panel.boundingBox())!;
    expect(moved.x).toBeLessThan(before.x); expect(moved.y).toBeGreaterThan(before.y);
    const resize = page.getByRole('button', { name: 'Resize window (arrow keys to adjust)' });
    await resize.hover(); await page.mouse.down(); await page.mouse.move(moved.x + moved.width - 160, moved.y + moved.height - 100, { steps: 5 }); await page.mouse.up();
    const smaller = (await panel.boundingBox())!;
    expect(smaller.width).toBeLessThan(moved.width - 100); await checkRatio();
    await resize.focus(); await page.keyboard.press('ArrowRight');
    expect((await panel.boundingBox())!.width).toBeGreaterThan(smaller.width);
    await checkRatio();
    // A scaled click reaches the same point in the remote 1280 x 800 page.
    const image = (await canvas.boundingBox())!;
    await canvas.click({ position: { x: image.width / 4, y: image.height / 2 } });
    await expect.poll(() => pointers.filter(p => p.event === 'down').length).toBeGreaterThan(0);
    const click = pointers.filter(p => p.event === 'down').at(-1)!;
    expect(click.x).toBeCloseTo(320, 0); expect(click.y).toBeCloseTo(400, 0);
    // The nonmodal window leaves the chat composer usable.
    const composer = page.locator('.chat-input textarea');
    await composer.fill('Chat remains available'); await expect(composer).toHaveValue('Chat remains available');
    const saved = (await panel.boundingBox())!;
    await page.getByRole('button', { name: 'Dock to sidebar', exact: true }).click();
    await expect(page.locator('.chat-side-panel')).toHaveAttribute('data-docked');
    await expect(panel).not.toHaveClass(/browser-drawer--floating/);
    await page.getByRole('button', { name: 'Floating window', exact: true }).click();
    await expect(page.locator('.chat-side-panel')).not.toHaveAttribute('data-docked');
    const restored = (await panel.boundingBox())!;
    expect(restored.x).toBeCloseTo(saved.x, 0); expect(restored.y).toBeCloseTo(saved.y, 0);
    expect(restored.width).toBeCloseTo(saved.width, 0);
    await page.getByRole('button', { name: 'Close browser panel', exact: true }).click(); await trigger.click();
    expect((await panel.boundingBox())!.width).toBeCloseTo(saved.width, 0);
    await page.getByRole('button', { name: 'Keyboard input', exact: true }).click();
    await checkRatio();
    await page.getByRole('button', { name: 'Hide keyboard input', exact: true }).click();
    await page.setViewportSize({ width: 900, height: 580 });
    await expect.poll(async () => { const r = (await panel.boundingBox())!; return r.x + r.width; }).toBeLessThanOrEqual(885);
    await expect.poll(async () => { const r = (await panel.boundingBox())!; return r.y + r.height; }).toBeLessThanOrEqual(565);
    await checkRatio();
    await page.setViewportSize({ width: 1920, height: 1000 });
  } else {
    await expect(panel).not.toHaveClass(/browser-drawer--floating/);
    await expect(page.getByRole('button', { name: 'Dock to sidebar', exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Resize window (arrow keys to adjust)' })).toHaveCount(0);
  }
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
