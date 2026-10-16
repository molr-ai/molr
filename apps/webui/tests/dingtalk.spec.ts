import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const port = process.env.MOLR_TEST_PORT ?? '5193';
async function post(page: Page, path: string, data: object) {
 const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
 const response = await page.request.post(path, { headers: { Origin: `http://127.0.0.1:${port}`, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
 expect(response.status(), await response.text()).toBe(200); return response.json();
}
// External platform callbacks are simulated. Authentication, assistant creation
// and ordinary chat use the real isolated Runtime; Go tests cover Stream and routing.
test('DingTalk pairing preserves WebUI input until bound and restores it on disconnect', async ({ page }, info) => {
 await page.goto('/');
 await page.getByLabel('Username', { exact: true }).fill('owner');
 await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
 await page.getByRole('button', { name: 'Sign in', exact: true }).click();
 await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
 const name = `DingTalk ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
 const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000 } }] } });
 const models = await (await page.request.get('/api/models')).json();
 const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
 let state = 'disconnected', bound = false;
 const expires = new Date(Date.now() + 600000).toISOString();
 await page.route(`**/api/assistants/${assistant.ID}/dingtalk`, route => route.fulfill({ json: { Channel: bound ? 'dingtalk' : 'webui', State: state, ClientID: 'ding-fixture', LastError: '', ConnectedAt: null, LastInboundAt: null, ExpiresAt: expires } }));
 await page.route('**/api/dingtalk/connect', async route => { const body = route.request().postDataJSON(); expect(body.ClientID).toBe('ding-fixture'); expect(body.ClientSecret).toBe('fixture-app-secret'); state = 'pairing'; await route.fulfill({ json: { Code: 'molr-fixture-pairing-code', ExpiresAt: expires } }); });
 await page.route('**/api/dingtalk/disconnect', route => { bound = false; state = 'disconnected'; return route.fulfill({ json: null }); });
 await page.goto(`/#/assistants/${assistant.ID}`);
 await expect(page.locator('.chat-input textarea')).toBeEnabled();
 await page.locator('.chat-input textarea').fill('preserved draft');
 await page.locator('.chat-header').getByRole('button', { name: 'More', exact: true }).click();
 await page.getByRole('menuitem', { name: 'Switch chat channel', exact: true }).click();
 await page.getByRole('button', { name: 'Connect DingTalk', exact: true }).click();
 await page.getByLabel('Client ID (AppKey)', { exact: true }).fill('ding-fixture');
 await page.getByLabel('Client Secret (AppSecret)', { exact: true }).fill('fixture-app-secret');
 await page.getByRole('dialog', { name: 'Connect a DingTalk robot', exact: true }).getByRole('button', { name: 'Connect DingTalk', exact: true }).click();
 await expect(page.getByLabel('One-time pairing code', { exact: true })).toHaveValue('molr-fixture-pairing-code');
 await expect(page.locator('.chat-input textarea')).toBeEnabled();
 for (const theme of ['light', 'dark']) for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) {
  await page.evaluate(({ theme, palette }) => { document.documentElement.dataset.theme = theme; document.documentElement.dataset.palette = palette; }, { theme, palette });
  await page.screenshot({ path: test.info().outputPath(`dingtalk-${palette}-${theme}.png`) });
 }
 state = 'connected'; bound = true;
 await expect(page.getByRole('button', { name: 'Disconnect DingTalk', exact: true })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Connect Weixin', exact: true })).toHaveCount(0);
 await expect(page.getByLabel('One-time pairing code', { exact: true })).toHaveCount(0);
 await page.getByRole('dialog', { name: 'Chat channel', exact: true }).locator('header').getByRole('button', { name: 'Close', exact: true }).click();
 await expect(page.locator('.message-composer--locked')).toContainText('Send messages in DingTalk');
 await page.reload();
 await expect(page.locator('.message-composer--locked')).toContainText('Send messages in DingTalk');
 await page.getByRole('button', { name: 'Manage DingTalk connection', exact: true }).click();
 await page.getByRole('button', { name: 'Disconnect DingTalk', exact: true }).click();
 await page.getByRole('button', { name: 'Confirm', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Connect DingTalk', exact: true })).toBeVisible();
 await page.getByRole('dialog', { name: 'Chat channel', exact: true }).locator('header').getByRole('button', { name: 'Close', exact: true }).click();
 await expect(page.locator('.chat-input textarea')).toBeEnabled();
 await expect(page.locator('.chat-input textarea')).toHaveValue('preserved draft');
});
