import { test, expect, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object) {
 const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
 const r = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
 expect(r.status(), await r.text()).toBe(200); return r.json();
}
test('Slack pairs without losing drafts, locks chat only after confirmation, and disconnects', async ({ page }, info) => {
 await page.goto('/');
 if (process.env.MOLR_SLACK_TEST_STATE) {
  const state = JSON.parse(readFileSync(process.env.MOLR_SLACK_TEST_STATE, 'utf8'));
  await page.context().addCookies(state.cookies); await page.reload();
 } else {
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
 }
 await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
 const name = `Slack ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
 const r = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000 } }] } });
 const models = await (await page.request.get('/api/models')).json();
 const a = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === r.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
 let state = 'disconnected';
 await page.route(`**/api/assistants/${a.ID}/slack`, route => route.fulfill({ json: { Channel: state === 'connected' ? 'slack' : 'webui', State: state, ConnectedAt: null, LastInboundAt: null, LastError: '', Deliveries: [] } }));
 await page.route('**/api/slack/connect', route => { const input = route.request().postDataJSON(); expect(input.AppToken).toBe('xapp-test'); expect(input.BotToken).toBe('xoxb-test'); expect(input.UserID).toBe('UUSER'); state = 'pairing'; return route.fulfill({ json: { ID: 'pair', Code: 'molr-test-pairing-code', ExpiresAt: new Date(Date.now() + 300000).toISOString() } }); });
 await page.route('**/api/slack/disconnect', route => { state = 'disconnected'; return route.fulfill({ json: null }); });
 await page.goto(`/#/assistants/${a.ID}`);
 await expect(page.locator('.chat-input textarea')).toBeEnabled(); await page.locator('.chat-input textarea').fill('keep this draft');
 await page.locator('.chat-header').getByRole('button', { name: 'More', exact: true }).click(); await page.getByRole('menuitem', { name: 'Switch chat channel', exact: true }).click();
 await page.getByRole('button', { name: 'Slack', exact: true }).click();
 const dialog = page.getByRole('dialog', { name: 'Chat channel', exact: true });
 await page.getByLabel('App Token', { exact: true }).fill('xapp-test'); await page.getByLabel('Bot Token', { exact: true }).fill('xoxb-test'); await page.getByLabel('Slack member ID', { exact: true }).fill('UUSER');
 await page.getByRole('button', { name: 'Connect Slack', exact: true }).click(); await expect(page.getByText('molr-test-pairing-code', { exact: true })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Connect Weixin', exact: true })).toHaveCount(0);
 for (const theme of ['light', 'dark']) for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) {
  await page.evaluate(({ theme, palette }) => { document.documentElement.dataset.theme = theme; document.documentElement.dataset.palette = palette; }, { theme, palette });
  await expect(dialog).toBeVisible(); const box = await dialog.boundingBox(); expect(box!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
  await page.screenshot({ path: test.info().outputPath(`slack-${theme}-${palette}.png`) });
 }
 await dialog.locator('header').getByRole('button', { name: 'Close', exact: true }).click(); await expect(page.locator('.chat-input textarea')).toHaveValue('keep this draft');
 state = 'connected'; await page.reload(); await expect(page.locator('.message-composer--locked')).toContainText('Send messages in Slack');
 await page.getByRole('button', { name: 'Manage Slack connection', exact: true }).click(); await page.getByRole('button', { name: 'Disconnect Slack', exact: true }).click(); await page.getByRole('button', { name: 'Confirm', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Slack', exact: true })).toBeVisible(); await dialog.locator('header').getByRole('button', { name: 'Close', exact: true }).click(); await expect(page.locator('.chat-input textarea')).toHaveValue('keep this draft');
});
