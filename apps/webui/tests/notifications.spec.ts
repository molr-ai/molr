import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
const port = process.env.MOLR_TEST_PORT ?? '5193', origin = `http://127.0.0.1:${port}`;
const modelOrigin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? '18882'}`;
async function login(page: Page) {
  await page.goto('/#/account/notifications');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('switch', { name: 'Notification alerts' })).toBeVisible();
}
async function post(page: Page, path: string, data: object, mutation = true) {
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: origin, 'X-CSRF-Token': csrf }, data: mutation ? { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: 0 } } : data });
  expect(response.ok(), await response.text()).toBe(true); return response.json();
}
async function assistant(page: Page) {
  const toggle=page.getByRole('switch',{name:'Notification alerts',exact:true}), enabled=await toggle.isChecked();
  if(enabled)await toggle.uncheck();
  const resource = await post(page, '/api/resources', { Name: `Notifications ${Date.now()}`, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: modelOrigin, AllowedOrigins: [modelOrigin], Models: [{ Name: 'notifications-model', ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const assistant = await post(page, '/api/assistants', { Name: 'Notification assistant', ModelID: models.find((item: { ResourceID: string }) => item.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
  // The born greeting is a separate real message. Consume it before measuring the next reply.
  await expect.poll(async()=>{const data=await(await page.request.get(`/api/conversations/${assistant.ConversationID}/messages`)).json();return data.Items.some((m:any)=>m.Sender==='assistant');}).toBe(true);
  const chat=await(await page.request.get(`/api/conversations/${assistant.ConversationID}`)).json();
  await post(page,'/api/conversations/read',{ConversationID:chat.ID,ThroughSequence:chat.LastSequence},false);
  if(enabled)await toggle.check();
  return assistant;
}
async function send(page: Page, conversation: string) {
  const before = await (await page.request.get(`/api/conversations/${conversation}`)).json();
  await post(page, '/api/conversations/messages', { ConversationID: conversation, Content: { Kind: 'text', Text: `Notify me ${Date.now()}` } });
  await expect.poll(async () => {
    const result = await (await page.request.get(`/api/conversations/${conversation}/messages?after_sequence=${before.LastSequence}`)).json();
    return result.Items?.some((message: { Sender: string }) => message.Sender === 'assistant');
  }).toBe(true);
}
async function expectUnreadBadge(page: Page) {
  const conversations = await (await page.request.get('/api/conversations')).json();
  const count = (conversations.Items ?? []).filter((item: { Archived: boolean }) => !item.Archived).reduce((sum: number, item: { UnreadCount: number }) => sum + item.UnreadCount, 0);
  const badge = page.getByRole('navigation', { name: 'Molr', exact: true }).getByRole('button',{name:'Assistants',exact:true}).locator('.nav-unread');
  if (count) { await expect(badge).toBeVisible(); await expect(badge).toHaveText(count > 99 ? '99+' : String(count)); await expect(badge).toHaveAttribute('aria-label', `${count} unread messages`); }
  else await expect(badge).toHaveCount(0);
}
const alerts = (page: Page) => page.getByRole('button', { name: 'Open conversation', exact: true });

test('active chat reads replies with a docked panel and after an obstructing drawer closes', async ({ page }, info) => {
  await login(page);
  const peer = await assistant(page);
  if (info.project.name === 'desktop') await page.setViewportSize({ width: 1920, height: 1000 });
  await page.goto(`/#/assistants/${peer.ID}`);
  const chat = () => page.request.get(`/api/conversations/${peer.ConversationID}`).then(response => response.json());
  await expect(page.getByRole('log')).toBeVisible();
  await expect.poll(async () => (await chat()).UnreadCount).toBe(0);
  await page.locator('.chat-assistant-info').click();
  const panel = page.locator('dialog.assistant-info[open]');
  await expect(panel).toBeVisible();
  if (info.project.name === 'desktop') await expect(panel).toHaveAttribute('data-docked', 'true');
  await send(page, peer.ConversationID);
  await expect(page.locator('.chat-history .ui-chat-message--assistant')).toHaveCount(2);
  if (info.project.name === 'desktop') {
    // The docked panel leaves the current conversation visible and usable.
    await expect.poll(async () => (await chat()).UnreadCount).toBe(0);
  } else {
    // A modal drawer really obscures the conversation until it is dismissed.
    await expect.poll(async () => (await chat()).UnreadCount).toBe(1);
  }
  await panel.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(panel).toHaveCount(0);
  await expect.poll(async () => (await chat()).UnreadCount).toBe(0);
  if (info.project.name === 'desktop') await expectUnreadBadge(page);
  await send(page, peer.ConversationID);
  await expect.poll(async () => (await chat()).UnreadCount).toBe(0);
  await expect(page).toHaveURL(new RegExp(`#/assistants/${peer.ID}$`));
  if (info.project.name === 'mobile') {
    await page.getByRole('button', { name: 'Back to assistants', exact: true }).click();
    await expectUnreadBadge(page);
  }
});

test('notification preferences, fallback and click-to-conversation use real assistant messages', async ({ page }) => {
  await login(page);
  await expect(page.getByRole('switch', { name: 'Notification alerts' })).not.toBeChecked();
  const peer = await assistant(page);
  await send(page, peer.ConversationID);
  await expect(alerts(page)).toHaveCount(0);
  await expectUnreadBadge(page);
  await page.getByRole('switch', { name: 'Notification alerts' }).check();
  await send(page, peer.ConversationID);
  await expect(alerts(page)).toHaveCount(1);
  await expect(page.getByText('You have a new assistant message', { exact: true })).toBeVisible();
  await expect(page.getByText('Runtime delivered this reply through conversation_publish.', { exact: true })).toHaveCount(0);
  await alerts(page).click();
  await expect(page).toHaveURL(new RegExp(`#/assistants/${peer.ID}$`));
  await expect(page.getByRole('log').getByText('Runtime delivered this reply through conversation_publish.', { exact: true }).first()).toBeVisible();
  // A focused conversation is read normally and does not interrupt its reader.
  await send(page, peer.ConversationID);
  await expect.poll(async () => { const chat = await (await page.request.get(`/api/conversations/${peer.ConversationID}`)).json(); return chat.LastReadSequence === chat.LastSequence; }).toBe(true);
  await expect(alerts(page)).toHaveCount(0);
  await page.goto('/#/account/notifications');
  await page.reload();
  await expect(page.getByRole('switch', { name: 'Notification alerts' })).toBeChecked();
  // Reloading and replaying existing messages must not produce historical alerts.
  await page.waitForTimeout(900);
  await expect(alerts(page)).toHaveCount(0);
  await page.getByRole('switch', { name: 'Show message previews' }).check();
  await send(page, peer.ConversationID);
  await expect(page.getByText('Runtime delivered this reply through conversation_publish.', { exact: true })).toBeVisible();
  await page.getByRole('switch', { name: 'Desktop notifications', exact: true }).uncheck();
  await expect(alerts(page)).toHaveCount(1);
  await page.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
  await send(page, peer.ConversationID);
  await expect(alerts(page)).toHaveCount(1); await expectUnreadBadge(page);
  await page.getByRole('switch', { name: 'Notification alerts' }).uncheck();
  await send(page, peer.ConversationID);
  await page.waitForTimeout(900);
  await expect(alerts(page)).toHaveCount(0); await expectUnreadBadge(page);
});

test('multiple background tabs request one notification through the real browser service worker', async ({ page, context }) => {
  await context.grantPermissions(['notifications']);
  await context.addInitScript(() => {
    // Headless Chromium keeps tabs focused; explicitly model a background window.
    document.hasFocus = () => false;
    if (typeof ServiceWorkerRegistration === 'undefined') return;
    const original = ServiceWorkerRegistration.prototype.showNotification;
    Object.defineProperty(window, 'notificationCalls', { value: [], configurable: true });
    ServiceWorkerRegistration.prototype.showNotification = function(title, options) {
      const call = { title, options, accepted: false };
      (window as unknown as { notificationCalls: unknown[] }).notificationCalls.push(call);
      return original.call(this, title, options).then(() => { call.accepted = true; });
    };
  });
  await login(page);
  await page.getByRole('switch', { name: 'Notification alerts' }).check();
  const peer = await assistant(page);
  const second = await context.newPage(); await second.goto('/#/account/notifications');
  await expect(second.getByRole('switch', { name: 'Notification alerts' })).toBeChecked();
  const foreground = await context.newPage(); await foreground.goto('about:blank'); await foreground.bringToFront();
  await send(page, peer.ConversationID);
  const count = async () => (await Promise.all([page, second].map(tab => tab.evaluate(() => (window as unknown as { notificationCalls: unknown[] }).notificationCalls.length)))).reduce((a, b) => a + b, 0);
  await expect.poll(count).toBe(1);
  await page.waitForTimeout(900); expect(await count()).toBe(1);
  const calls = async () => (await Promise.all([page, second].map(tab => tab.evaluate(() => (window as unknown as { notificationCalls: { title: string; options: NotificationOptions; accepted: boolean }[] }).notificationCalls)))).flat();
  await expect.poll(async () => (await calls()).map(item => ({ title: item.title, body: item.options.body, accepted: item.accepted }))).toEqual([{ title: 'Molr', body: 'You have a new assistant message', accepted: true }]);
  expect(await page.evaluate(async () => (await navigator.serviceWorker.getRegistration())?.active?.state)).toBe('activated');
  expect((await calls())[0].options.silent).toBe(true);
  await page.getByRole('switch', { name: 'Desktop notifications', exact: true }).uncheck();
  await expect(second.getByRole('switch', { name: 'Desktop notifications', exact: true })).not.toBeChecked();
  await send(page, peer.ConversationID);
  await expect.poll(async () => await alerts(page).count() + await alerts(second).count()).toBe(1);
  expect(await count()).toBe(1); await expectUnreadBadge(page);
  // Headless Chromium accepts the request but has no OS notification center.
  // Platform close/click behavior is exercised with a controlled adapter below.

});

test('notification settings hide unavailable system controls and synchronize across tabs', async ({ page, context }) => {
  await page.addInitScript(() => Object.defineProperty(Notification, 'permission', { get: () => 'denied' }));
  await login(page);
  await expect(page.getByText(/Desktop notifications blocked/)).toBeVisible();
  await page.getByRole('button', { name: 'Send test notification' }).click();
  await expect(page.getByText('Molr notification test', { exact: true })).toBeVisible();
  const second = await context.newPage(); await second.goto('/#/account/notifications');
  await page.getByRole('switch', { name: 'Desktop notifications', exact: true }).uncheck();
  await expect(second.getByRole('switch', { name: 'Desktop notifications', exact: true })).not.toBeChecked();
  await second.addInitScript(() => Object.defineProperty(window, 'isSecureContext', { value: false }));
  await second.reload();
  await expect(second.getByRole('switch', { name: 'Desktop notifications', exact: true })).toHaveCount(0);
  await expect(second.getByRole('button', { name: 'Allow system notifications' })).toHaveCount(0);
  await expect(second.getByText(/HTTPS|certificate/)).toHaveCount(0);
  await expect(second.getByRole('switch', { name: 'Message sound', exact: true })).toBeVisible();
  await second.getByRole('button', { name: 'Send test notification' }).click();
  await expect(second.getByText('An in-app test alert was displayed.', { exact: true })).toBeVisible();
});

test('notification channels stay independent when migrating previous DND settings', async ({ page }) => {
  await login(page);
  const principal = await (await page.request.get('/api/auth/me')).json();
  await page.evaluate(owner => localStorage.setItem(`molr-notifications:${owner}`, JSON.stringify({ enabled: true, sound: true, system: true, quiet: true })), principal.OwnerID);
  await page.reload();
  await expect(page.getByRole('switch', { name: 'Do not disturb' })).toHaveCount(0);
  await expect(page.getByRole('switch', { name: 'Desktop notifications', exact: true })).not.toBeChecked();
  await expect(page.getByRole('switch', { name: 'Message sound', exact: true })).toBeChecked();
  await page.getByRole('switch', { name: 'Desktop notifications', exact: true }).check();
  await expect(page.getByRole('switch', { name: 'Message sound', exact: true })).toBeChecked();
  await page.getByRole('switch', { name: 'Message sound', exact: true }).uncheck();
  await expect(page.getByRole('switch', { name: 'Desktop notifications', exact: true })).toBeChecked();
  await page.reload();
  await expect(page.getByRole('switch', { name: 'Desktop notifications', exact: true })).toBeChecked();
  await expect(page.getByRole('switch', { name: 'Message sound', exact: true })).not.toBeChecked();
});

test('notification claims are atomic across tabs and reject older out-of-order sequences', async ({ page, context }) => {
  await page.goto('/'); const second = await context.newPage(); await second.goto('/');
  const account = crypto.randomUUID();
  const claim = (tab: Page, sequence: number) => tab.evaluate(async ({ account, sequence }) => {
    const path = '/src/notifications/coordinator.ts';
    const { NotificationClaims } = await import(path), claims = new NotificationClaims();
    try { return await claims.claim(account, 'conversation', sequence); } finally { claims.close(); }
  }, { account, sequence });
  const results = await Promise.all([claim(page, 2), claim(second, 2)]);
  expect(results.sort()).toEqual([false, true]);
  expect(await claim(page, 1)).toBe(false);
  expect(await claim(second, 3)).toBe(true);
});

test('notification sounds share a five-second cooldown across tabs and conversations', async ({ page, context }) => {
  await page.goto('/'); const second = await context.newPage(); await second.goto('/');
  const account = crypto.randomUUID(), now = Date.now();
  const claim = (tab: Page, at: number, owner = account) => tab.evaluate(async ({ at, owner }) => {
    const original = Date.now; Date.now = () => at;
    const path = '/src/notifications/coordinator.ts', { NotificationClaims } = await import(path), claims = new NotificationClaims();
    try { return await claims.claimSound(owner); } finally { claims.close(); Date.now = original; }
  }, { at, owner });
  expect((await Promise.all([claim(page, now), claim(second, now)])).sort()).toEqual([false, true]);
  expect(await claim(second, now + 4999)).toBe(false);
  expect(await claim(page, now + 5000)).toBe(true);
  expect(await claim(second, now + 5000, `${account}-other`)).toBe(true);
});

test('notification sound uses real Web Audio once across tabs independently of system alerts', async ({ page, context }) => {
  test.setTimeout(60_000);
  await context.addInitScript(() => {
    if (typeof AudioContext === 'undefined') return;
    Object.defineProperty(window, 'soundNotes', { value: 0, writable: true });
    const original = AudioContext.prototype.createOscillator;
    AudioContext.prototype.createOscillator = function() {
      (window as unknown as { soundNotes: number }).soundNotes++;
      return original.call(this);
    };
  });
  await login(page); const peer = await assistant(page);
  await expect(page.getByRole('switch', { name: 'Message sound', exact: true })).not.toBeChecked();
  await page.getByRole('switch', { name: 'Notification alerts' }).check();
  await page.getByRole('switch', { name: 'Message sound', exact: true }).check();
  await page.getByRole('button', { name: 'Preview sound', exact: true }).click();
  await expect(page.getByText('Preview sound played.', { exact: true })).toBeVisible();
  const second = await context.newPage(); await second.goto('/#/account/notifications');
  await expect(second.getByRole('switch', { name: 'Message sound', exact: true })).toBeChecked();
  await second.getByRole('button', { name: 'Preview sound', exact: true }).click();
  await expect(second.getByText('Preview sound played.', { exact: true })).toBeVisible();
  for (const tab of [page, second]) await tab.evaluate(() => { (window as unknown as { soundNotes: number }).soundNotes = 0; });
  const notes = async () => (await Promise.all([page, second].map(tab => tab.evaluate(() => (window as unknown as { soundNotes: number }).soundNotes)))).reduce((a, b) => a + b, 0);
  await send(page, peer.ConversationID);
  await expect.poll(notes).toBe(2); // One two-note chime, not one per tab.
  await page.getByRole('switch', { name: 'Desktop notifications', exact: true }).uncheck();
  await expect(second.getByRole('switch', { name: 'Desktop notifications', exact: true })).not.toBeChecked();
  await expect(page.getByRole('button', { name: 'Preview sound', exact: true })).toBeEnabled();
  await expect(page.getByRole('switch', { name: 'Message sound', exact: true })).toBeChecked();
  // Let the intentional global cooldown expire before verifying sound with system alerts off.
  await page.waitForTimeout(5100);
  for (const tab of [page, second]) for (const close of await tab.getByRole('list', { name: 'Notifications', exact: true }).getByRole('button', { name: 'Close', exact: true }).all()) await close.click();
  await send(page, peer.ConversationID);
  await expect.poll(async () => await alerts(page).count() + await alerts(second).count()).toBe(1);
  await expect.poll(notes).toBe(4); await expectUnreadBadge(page);
  await page.getByRole('switch', { name: 'Message sound', exact: true }).uncheck();
  await page.getByRole('switch', { name: 'Desktop notifications', exact: true }).check();
  await expect(second.getByRole('switch', { name: 'Message sound', exact: true })).not.toBeChecked();
  await send(page, peer.ConversationID); await page.waitForTimeout(900);
  expect(await notes()).toBe(4);
});

test('notification adapter cancels pending delivery, scopes cleanup and validates click routes', async ({ page }) => {
  await page.goto('/');
  const result = await page.evaluate(async () => {
    Object.defineProperty(Notification, 'permission', { get: () => 'granted' });
    const path = '/src/notifications/browser.ts', { createBrowserNotifications } = await import(path);
    let release!: () => void, shown = 0;
    const routes: string[] = [], closed: string[] = [];
    const gate = new Promise<void>(resolve => { release = resolve; });
    const items = ['molr:account:one', 'molr:account:two', 'molr:other:one'].map(tag => ({ tag, close: () => closed.push(tag) }));
    const registration = { active: {}, showNotification: async () => { shown++; }, getNotifications: async () => items };
    Object.defineProperty(navigator.serviceWorker, 'register', { value: async () => { await gate; return registration; } });
    const adapter = createBrowserNotifications('account', (route: string) => routes.push(route));
    const pending = adapter.show({ tag: 'one', title: 'test', body: 'test', route: '#/assistants/one' });
    const cleanup = adapter.close('one'); release(); await Promise.all([pending, cleanup]);
    const canceled = shown === 0;
    await adapter.show({ tag: 'two', title: 'test', body: 'test', route: '#/assistants/two' });
    navigator.serviceWorker.dispatchEvent(new MessageEvent('message', { data: { type: 'molr.notification.open', account: 'other', route: '#/assistants/one' } }));
    navigator.serviceWorker.dispatchEvent(new MessageEvent('message', { data: { type: 'molr.notification.open', account: 'account', route: 'https://evil.invalid' } }));
    navigator.serviceWorker.dispatchEvent(new MessageEvent('message', { data: { type: 'molr.notification.open', account: 'account', route: '#/assistants/two' } }));
    for (const route of ['#/plans/plan1', '#/assistants/two?panel=channel', '#/assistants/two?panel=tasks&task=task1', '#/workbench/two?event=event1', '#/workbench/two?event=../../account', '#/workbench/two?event=event1&redirect=evil', '#/workbench/two?event=', '#/notifications/old', '#/account/security', '#/plans/../../account']) {
      navigator.serviceWorker.dispatchEvent(new MessageEvent('message', { data: { type: 'molr.notification.open', account: 'account', route } }));
    }
    await adapter.close(); adapter.dispose();
    return { canceled, shown, closed, routes };
  });
  expect(result.canceled).toBe(true); expect(result.shown).toBe(1);
  expect(result.closed).toContain('molr:account:one'); expect(result.closed).toContain('molr:account:two');
  expect(result.closed).not.toContain('molr:other:one'); expect(result.routes).toEqual(['#/assistants/two', '#/plans/plan1', '#/assistants/two?panel=channel', '#/assistants/two?panel=tasks&task=task1', '#/workbench/two?event=event1']);
});
