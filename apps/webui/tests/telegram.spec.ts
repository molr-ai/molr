import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
const port = process.env.MOLR_TEST_PORT ?? '5193';
const base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object) {
 const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
 const r = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
 expect(r.status(), await r.text()).toBe(200); return r.json();
}
// External Telegram endpoints alone are simulated. Login, assistants and chat history use the isolated Runtime.
test('Telegram requires confirmation, preserves drafts, survives refresh and disconnects', async ({ page }, info) => {
 await page.goto('/'); await page.getByLabel('Username', { exact: true }).fill('owner'); await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!'); await page.getByRole('button', { name: 'Sign in', exact: true }).click();
 await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
 const name = `Telegram ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
 const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000 } }] } });
 const models = await (await page.request.get('/api/models')).json();
 const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
 let bound = false, ready = false, canceled = 0, generation = 0, state = 'connected';
 const status = () => ({ Channel: bound ? 'telegram' : 'webui', State: bound ? state : 'disconnected', BotUsername: 'fixture_bot', LastError: '', Deliveries: [] });
 const login = () => ({ ID: `connect-${generation}`, Status: ready ? 'ready' : 'waiting', URL: 'https://t.me/fixture_bot?start=fixture-code', BotUsername: 'fixture_bot', UserID: 12345, UserName: 'Fixture User', ExpiresAt: new Date(Date.now() + 300000).toISOString() });
 await page.route(`**/api/assistants/${assistant.ID}/telegram`, r => r.fulfill({ json: status() }));
 await page.route('**/api/telegram/login', async r => { expect(r.request().postDataJSON().Token).toBe('123456:synthetic-token-for-ui-tests'); generation++; await r.fulfill({ json: login() }); });
 await page.route('**/api/telegram/poll', r => r.fulfill({ json: login() }));
 await page.route('**/api/telegram/confirm', r => { expect(ready).toBe(true); bound = true; return r.fulfill({ json: null }); });
 await page.route('**/api/telegram/cancel', r => { canceled++; return r.fulfill({ json: null }); });
 await page.route('**/api/telegram/disconnect', r => { bound = false; return r.fulfill({ json: null }); });
 await page.goto(`/#/assistants/${assistant.ID}`);
 await expect(page.locator('.chat-input textarea')).toBeEnabled(); await page.locator('.chat-input textarea').fill('preserved draft');
 async function open() { await page.locator('.chat-header').getByRole('button', { name: 'More', exact: true }).click(); await page.getByRole('menuitem', { name: 'Switch chat channel', exact: true }).click(); }
 async function begin() { await page.getByRole('button', { name: 'Telegram', exact: true }).click(); await page.getByRole('dialog', { name: 'Chat channel', exact: true }).getByLabel('Bot Token', { exact: true }).fill('123456:synthetic-token-for-ui-tests'); await page.getByRole('button', { name: 'Verify and continue', exact: true }).click(); }
 await open(); await begin(); await expect(page.getByRole('link', { name: 'Open Telegram', exact: true })).toHaveAttribute('href', 'https://t.me/fixture_bot?start=fixture-code'); expect(bound).toBe(false);
 await page.getByRole('button', { name: 'Cancel connection', exact: true }).click(); expect(canceled).toBe(1); expect(bound).toBe(false);
 await begin();
 for (const theme of ['light', 'dark']) for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) {
  await page.evaluate(({ theme, palette }) => { document.documentElement.dataset.theme = theme; document.documentElement.dataset.palette = palette; }, { theme, palette });
  await expect(page.locator('.weixin-qr rect')).toHaveCSS('fill', 'rgb(255, 255, 255)'); await expect(page.locator('.weixin-qr path')).toHaveCSS('fill', 'rgb(0, 0, 0)');
  const dialog = page.getByRole('dialog', { name: 'Chat channel', exact: true });
  expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
  await page.screenshot({ path: test.info().outputPath(`telegram-${theme}-${palette}.png`) });
 }
 ready = true; await expect(page.getByText('Fixture User', { exact: true })).toBeVisible(); expect(bound).toBe(false);
 await page.getByRole('button', { name: 'Confirm connection', exact: true }).click(); await expect(page.getByRole('dialog', { name: 'Chat channel', exact: true }).getByRole('button', { name: 'Disconnect Telegram', exact: true })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Connect Weixin', exact: true })).toHaveCount(0);
 await page.getByRole('dialog', { name: 'Chat channel', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
 await expect(page.locator('.message-composer--locked')).toContainText('Send messages in Telegram'); await expect(page.locator('.chat-input textarea')).toHaveCount(0);
 state = 'blocked'; await page.reload(); await expect(page.locator('.message-composer--locked')).toBeVisible();
 await page.getByRole('button', { name: 'Manage Telegram connection', exact: true }).click();

 await expect(page.getByText('Bot cannot send messages; unblock it in Telegram and send a message', { exact: true })).toBeVisible();
 await page.getByRole('button', { name: 'Disconnect Telegram', exact: true }).click(); await page.getByRole('button', { name: 'Confirm', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Telegram', exact: true })).toBeVisible();
 await page.getByRole('dialog', { name: 'Chat channel', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
 await expect(page.locator('.chat-input textarea')).toBeEnabled();
});
