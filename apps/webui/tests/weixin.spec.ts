import { channelSummaries } from './channel-summary';
import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const port = process.env.MOLR_TEST_PORT ?? '5193';
const base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object) {
 const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
 const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
 expect(response.status(), await response.text()).toBe(200); return response.json();
}
// Only the new external-channel UI boundary is simulated here. Auth, assistant
// creation, history and ordinary chat use the real isolated Runtime. Go tests
// exercise binding/authorization and durable protocol processing separately.
test('Weixin binding keeps WebUI chat until confirmation and restores it on disconnect', async ({ page }, info) => {
 await page.goto('/');
 await page.getByLabel('Username', { exact: true }).fill('owner');
 await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
 await page.getByRole('button', { name: 'Sign in', exact: true }).click();
 await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
 const name = `Weixin ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
 const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000 } }] } });
 const models = await (await page.request.get('/api/models')).json();
 const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
 let bound = false, stage = 'need_verifycode', generation = 0;
 const status = () => ({ Channel: bound ? 'weixin' : 'webui', State: bound ? 'connected' : 'disconnected', LastInboundAt: null, ConnectedAt: null, LastError: '', Deliveries: [] });
 await page.route(`**/api/assistants/${assistant.ID}/chat-channel`, route => route.fulfill({ json: { Channel: bound ? 'weixin' : 'webui' } }));
 await page.route(`**/api/assistants/${assistant.ID}/chat-channels`, route => route.fulfill({ json: channelSummaries('weixin', { Channel: bound ? 'weixin' : 'webui', State: bound ? 'connected' : 'disconnected' }) }));
 await page.route(`**/api/assistants/${assistant.ID}/weixin*`, route => route.fulfill({ json: status() }));
 await page.route('**/api/weixin/login', route => route.fulfill({ json: { ID: `qr-${++generation}`, Status: 'wait', QRCode: 'https://weixin.qq.com/qr/test', ExpiresAt: new Date(Date.now() + 300000).toISOString() } }));
 await page.route('**/api/weixin/poll', async route => {
  const input = route.request().postDataJSON();
  if (input.VerifyCode === '123456') { bound = true; stage = 'confirmed'; }
  await route.fulfill({ json: { ID: input.ID, Status: stage, QRCode: 'https://weixin.qq.com/qr/test', ExpiresAt: new Date(Date.now() + 300000).toISOString() } });
 });
 await page.route('**/api/weixin/cancel', route => route.fulfill({ json: null }));
 await page.route('**/api/weixin/disconnect', route => { bound = false; return route.fulfill({ json: null }); });
 await page.goto(`/#/assistants/${assistant.ID}`);
 await expect(page.locator('.chat-input textarea')).toBeEnabled();
 await page.locator('.chat-input textarea').fill('preserved draft');
 await page.locator('.chat-assistant-info').click();
 await expect(page.getByRole('button', { name: 'Connect Weixin', exact: true })).toHaveCount(0);
 await page.getByRole('button', { name: 'Cancel', exact: true }).click();
 await page.locator('.chat-header').getByRole('button', { name: 'More', exact: true }).click();
 await page.getByRole('menuitem', { name: 'Switch chat channel', exact: true }).click();
 await expect(page.getByRole('dialog', { name: 'Chat channel', exact: true })).toContainText('Choose where to chat');
 await page.screenshot({ path: test.info().outputPath('chat-channel.png') });
 await page.getByRole('button', { name: 'Weixin', exact: true }).click();
 await page.getByRole('button', { name: 'Connect Weixin', exact: true }).click();
 await expect(page.getByRole('img', { name: 'Scan with Weixin and confirm' })).toBeVisible();
 await expect(page.getByText('Data privacy', { exact: true })).toBeVisible();
 await expect(page.getByText(/transmitted to Tencent, which may retain this data/)).toBeVisible();
 await expect(page.getByLabel('Pairing code', { exact: true })).toBeVisible();
 expect(bound).toBe(false);
 for (const theme of ['light', 'dark']) for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) {
  await page.evaluate(({ theme, palette }) => { document.documentElement.dataset.theme = theme; document.documentElement.dataset.palette = palette; }, { theme, palette });
  await expect(page.locator('.weixin-qr rect')).toHaveCSS('fill', 'rgb(255, 255, 255)');
  await expect(page.locator('.weixin-qr path')).toHaveCSS('fill', 'rgb(0, 0, 0)');
 }
 await page.screenshot({ path: test.info().outputPath('weixin-qr.png') });
 await page.getByLabel('Pairing code', { exact: true }).fill('123456');
 await page.getByRole('button', { name: 'Submit code', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Disconnect Weixin', exact: true })).toBeVisible();
 await expect(page.locator('.weixin-settings .ui-alert--positive')).toContainText('Connected');
 // Channel management is an independent dialog, not the assistant drawer.
 await page.getByRole('dialog', { name: 'Chat channel', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
 await expect(page.locator('.chat-input .message-composer--locked')).toContainText('Send messages in Weixin');
 await expect(page.locator('.chat-input textarea')).toHaveCount(0);
 await page.reload();
 await expect(page.locator('.chat-input .message-composer--locked')).toBeVisible();
 await page.getByRole('button', { name: 'Manage Weixin connection', exact: true }).click();
 await expect(page.getByRole('dialog', { name: 'Chat channel', exact: true })).toContainText('Bound');
 await expect(page.getByRole('tab', { name: 'Overview', exact: true })).toHaveCount(0);
 await page.getByRole('button', { name: 'Disconnect Weixin', exact: true }).click();
 await page.getByRole('button', { name: 'Confirm', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Weixin', exact: true })).toBeVisible();
 // Expiry can be refreshed without changing the current chat channel.
 stage = 'expired';
 await page.getByRole('button', { name: 'Weixin', exact: true }).click();
 await page.getByRole('button', { name: 'Connect Weixin', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Refresh QR code', exact: true })).toBeVisible();
 const previous = generation; stage = 'need_verifycode';
 await page.getByRole('button', { name: 'Refresh QR code', exact: true }).click();
 await expect(page.getByLabel('Pairing code', { exact: true })).toBeVisible();
 expect(generation).toBeGreaterThan(previous);
 await page.getByRole('button', { name: 'Cancel connection', exact: true }).click();
 await page.getByRole('dialog', { name: 'Chat channel', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
 await expect(page.locator('.chat-input textarea')).toBeEnabled();
 await expect(page.locator('.chat-input textarea')).toHaveValue('preserved draft');
});

async function noticeAssistant(page: Page, project: string) {
 await page.goto('/');
 await page.getByLabel('Username', { exact: true }).fill('owner');
 await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
 await page.getByRole('button', { name: 'Sign in', exact: true }).click();
 await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
 const name = `Notices ${project}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
 const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000 } }] } });
 const models = await (await page.request.get('/api/models')).json();
 return post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
}

test('successful deliveries stay quiet while abnormal outcomes remain visible', async ({ page }, info) => {
 const assistant = await noticeAssistant(page, info.project.name);
 let state = 'accepted', error = '', deliveryID = 'delivery', attempts = 1;
 const dismissed = new Set<string>();
 const noticeID = () => `${deliveryID}-${state}-${error}`;
 await page.route('**/api/notifications?*', async route => {
  const show = !['accepted','sending','canceled'].includes(state) && !dismissed.has(noticeID());
  const rows = show ? [{ ID:noticeID(),OwnerID:assistant.OwnerID,Kind:'weixin.delivery',Code:`${state}:${error}`,Source:{Kind:'weixin_delivery',ID:deliveryID},AssistantID:assistant.ID,Label:'',Severity:'warning',CreatedAt:new Date().toISOString(),UpdatedAt:new Date().toISOString(),ReadAt:null,DismissedAt:null,ResolvedAt:null,Version:1 }] : [];
  await route.fulfill({json:{Items:rows,NextCursor:'',UnreadCount:rows.length,PendingCount:rows.length}});
 });
 await page.route('**/api/notifications/update', async route => {
  const body=route.request().postDataJSON();if(body.Action==='dismiss')for(const id of body.IDs)dismissed.add(id);
  await route.fulfill({json:null});
 });
 let writes = 0;
 page.on('request', request => { if (request.method() === 'POST' && request.url().includes('/api/weixin/')) writes++; });
 await page.route(`**/api/assistants/${assistant.ID}/chat-channel`, route => route.fulfill({ json: { Channel: 'weixin' } }));
 await page.route(`**/api/assistants/${assistant.ID}/chat-channels`, route => route.fulfill({ json: channelSummaries('weixin', { Channel: 'weixin', State: 'reconnecting' }) }));
 await page.route(`**/api/assistants/${assistant.ID}/weixin*`, route => route.fulfill({ json: {
  Channel: 'weixin', State: 'connected', LastInboundAt: new Date().toISOString(), ConnectedAt: new Date().toISOString(), LastError: '',
  Deliveries: [{ ID: deliveryID, BindingID: 'binding', MessageID: 'message', Sequence: 1, Part: 0, State: state, Error: error, Attempts: attempts, CreatedAt: new Date().toISOString() }],
 } }));
 await page.goto(`/#/assistants/${assistant.ID}`);
 await expect(page.locator('.chat-input .message-composer--locked')).toBeVisible();
 await expect(page.getByText('Weixin delivery', { exact: true })).toHaveCount(0);
 for (const [next, reason, label] of [
  ['unknown', 'unknown', 'Delivery unconfirmed'],
  ['waiting', 'context_required', 'Waiting for a new Weixin message'],
  ['failed', 'attachment_failed', 'Attachment upload or read failed'],
  ['pending', 'rate_limited', 'Rate limited; waiting to retry'],
  ['failed', 'rate_limited', 'Rate limited; retries exhausted'],
 ]) {
  state = next; error = reason;
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
  await expect(page.getByText('Weixin delivery', { exact: true })).toHaveCount(1);
  await expect(page.getByText(label, { exact: true })).toBeVisible();
  const notice = page.locator('.ui-alert').filter({ hasText: 'Weixin delivery' });
  await notice.getByRole('button', { name: 'Close', exact: true }).focus();
  await page.keyboard.press('Enter');
  await expect(notice).toHaveCount(0);
  attempts++;
  const refreshed = page.waitForResponse(response => response.url().endsWith(`/api/assistants/${assistant.ID}/chat-channel`));
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
  await refreshed;
  await expect(notice).toHaveCount(0);
 }
 // A new delivery with the same outcome must still appear.
 deliveryID = 'new-delivery';
 await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
 await expect(page.getByText('Rate limited; retries exhausted', { exact: true })).toBeVisible();
 expect(writes).toBe(0);
 state = 'accepted'; error = '';
 await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
 await expect(page.getByText('Weixin delivery', { exact: true })).toHaveCount(0);
 await page.getByRole('button', { name: 'Manage Weixin connection', exact: true }).click();
 await expect(page.getByText('View recent deliveries', { exact: true })).toHaveCount(0);
 await expect(page.getByText(/Send and receive text, images and files/)).toBeVisible();
});

test('rate limit appears once, can be dismissed, and pauses background retries', async ({ page }, info) => {
 const assistant = await noticeAssistant(page, info.project.name);
 await page.goto(`/#/assistants/${assistant.ID}`);
 await expect(page.locator('.chat-input textarea')).toBeEnabled();
 await page.locator('.chat-header').getByRole('button', { name: 'More', exact: true }).click();
 await page.getByRole('menuitem', { name: 'Switch chat channel', exact: true }).click();
 const dialog = page.getByRole('dialog', { name: 'Chat channel', exact: true });
 await dialog.getByRole('button', { name: 'Weixin', exact: true }).click();
 await expect(dialog.getByRole('button', { name: 'Connect Weixin', exact: true })).toBeEnabled();
 let limited = 0;
 for (const suffix of ['status', 'weixin']) await page.route(`**/api/assistants/${assistant.ID}/${suffix}`, async route => {
  limited++;
  await route.fulfill({ status: 429, headers: { 'Retry-After': '30' }, json: { code: 'rate_limited', message: 'Too many requests', request_id: `request-${limited}` } });
 });
 await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
 const notices = page.locator('.ui-toast--warning:visible');
 await expect(notices).toHaveCount(1);
 await expect(dialog.locator('.ui-toast--warning:visible')).toHaveCount(1);
 await expect(notices.getByRole('button', { name: /^Retry/ })).toBeDisabled();
 await expect(notices.getByRole('button', { name: 'Close', exact: true })).toBeVisible();
 for (const theme of ['light', 'dark']) for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) {
  await page.evaluate(({ theme, palette }) => { document.documentElement.dataset.theme = theme; document.documentElement.dataset.palette = palette; }, { theme, palette });
  await expect(notices).toHaveCount(1);
  const colors = await notices.evaluate(element => ({ text: getComputedStyle(element).color, surface: getComputedStyle(element).backgroundColor }));
  expect(colors.text).not.toBe(colors.surface);
 }
 await page.screenshot({ path: test.info().outputPath('rate-limit-notice.png'), animations: 'disabled' });
 await notices.getByRole('button', { name: 'Close', exact: true }).click();
 await expect(notices).toHaveCount(0);
 const before = limited;
 for (let i = 0; i < 3; i++) await page.evaluate(async id => {
  document.dispatchEvent(new Event('visibilitychange'));
  // Use the same SDK singleton as background queries: Retry-After prevents I/O.
  const modulePath = '/@id/@molr/sdk';
  const { api } = await import(modulePath);
  await api.get(`/api/assistants/${id}/weixin`).catch(() => {});
 }, assistant.ID);
 expect(limited).toBe(before);
 await expect(notices).toHaveCount(0);
 await dialog.getByRole('button', { name: 'Close', exact: true }).click();
 await expect(notices).toHaveCount(0);
});

test('already connected login skips QR and preserves the connection', async ({ page }, info) => {
 const assistant = await noticeAssistant(page, `recognized-${info.project.name}`);
 let bound = false, polls = 0;
 await page.route(`**/api/assistants/${assistant.ID}/chat-channel`, route => route.fulfill({ json: { Channel: bound ? 'weixin' : 'webui' } }));
 await page.route(`**/api/assistants/${assistant.ID}/chat-channels`, route => route.fulfill({ json: channelSummaries('weixin', { Channel: bound ? 'weixin' : 'webui', State: bound ? 'connected' : 'disconnected' }) }));
 await page.route(`**/api/assistants/${assistant.ID}/weixin*`, route => route.fulfill({ json: { Channel: bound ? 'weixin' : 'webui', State: bound ? 'connected' : 'disconnected', ConnectedAt: bound ? new Date().toISOString() : null, LastInboundAt: null, LastError: '', Deliveries: [] } }));
 await page.route('**/api/weixin/login', route => { bound = true; return route.fulfill({ json: { ID: '', Status: 'already_connected', QRCode: '', ExpiresAt: '' } }); });
 await page.route('**/api/weixin/poll', route => { polls++; return route.fulfill({ json: { Status: 'already_connected' } }); });
 await page.goto(`/#/assistants/${assistant.ID}`);
 await page.locator('.chat-header').getByRole('button', { name: 'More', exact: true }).click();
 await page.getByRole('menuitem', { name: 'Switch chat channel', exact: true }).click();
 await page.getByRole('button', { name: 'Weixin', exact: true }).click();
 await page.getByRole('button', { name: 'Connect Weixin', exact: true }).click();
 await expect(page.getByText('Already connected; no need to scan again', { exact: true })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Disconnect Weixin', exact: true })).toBeVisible();
 await expect(page.getByRole('img', { name: 'Scan with Weixin and confirm' })).toHaveCount(0);
 expect(polls).toBe(0);
});


test('waiting notice dismissal survives polling, reload and another execution of the same problem', async ({ page }, info) => {
 const assistant = await noticeAssistant(page, `waiting-${info.project.name}`);
 await page.goto(`/#/assistants/${assistant.ID}`);
 const send = () => post(page, '/api/conversations/messages', { ConversationID: assistant.ConversationID, Content: { Kind: 'text', Text: 'Check the configured budget.' } });
 const notice = page.locator('.ui-alert').filter({ hasText: 'Assistant needs attention' });
 await expect(notice).toBeVisible();
 await notice.getByRole('button', { name: 'Close', exact: true }).click();
 await expect(notice).toHaveCount(0);
 const refreshed = page.waitForResponse(response => response.url().endsWith(`/api/assistants/${assistant.ID}/status`));
 await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
 await refreshed;
 await expect(notice).toHaveCount(0);
 await page.reload();
 await expect(page.locator('.chat-input textarea')).toBeVisible();
 await expect(notice).toHaveCount(0);
 await send();
 await expect(notice).toHaveCount(0);
});
