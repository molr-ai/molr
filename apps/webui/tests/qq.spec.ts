import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const port = process.env.MOLR_TEST_PORT ?? '5193';
const base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object) {
 const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
 const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
 expect(response.status(), await response.text()).toBe(200); return response.json();
}
// Auth and assistant creation use the isolated Runtime; external Feishu requests are simulated.
test('Channel drawer navigates, pairs QQ, restores drafts and supports manual setup', async ({ page }, info) => {
 await page.goto('/');
 await page.getByLabel('Username', { exact: true }).fill('owner');
 await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
 await page.getByRole('button', { name: 'Sign in', exact: true }).click();
 await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
 const name = `QQ ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
 const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000 } }] } });
 const models = await (await page.request.get('/api/models')).json();
 const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });

 let bound = false, stage = 'waiting', canceled = 0;
 const status = () => ({ Channel: bound ? 'qq' : 'webui', State: bound ? 'connected' : 'disconnected', AppID: '123456', LastInboundAt: null, ConnectedAt: null, LastError: '' });
 const login = () => ({ ID: 'qr-task', Status: stage, QRCode: 'https://q.qq.com/qqbot/openclaw/connect.html?task_id=fixture', Code: '', ExpiresAt: new Date(Date.now()+300000).toISOString() });
 await page.route(`**/api/assistants/${assistant.ID}/chat-channel*`, route => route.fulfill({ json: { Channel: status().Channel } }));
 await page.route(`**/api/assistants/${assistant.ID}/qq*`, route => route.fulfill({ json: status() }));
 await page.route('**/api/qq/login', route => route.fulfill({ json: login() }));
 await page.route('**/api/qq/poll', route => { if(stage==='confirmed')bound=true;return route.fulfill({json:login()}); });
 await page.route('**/api/qq/cancel', route => {canceled++;return route.fulfill({json:null});});
 await page.route('**/api/qq/disconnect', route => {bound=false;return route.fulfill({json:null});});
 await page.route('**/api/qq/connect', route => {const input=route.request().postDataJSON();expect(input.AppID).toBe('123456');expect(input.AppSecret).toBe('fixture-secret');return route.fulfill({json:{ID:'',Status:'pairing',Code:'molr-fixture-pair',QRCode:'',ExpiresAt:new Date(Date.now()+600000).toISOString()}});});
 await page.goto(`/#/assistants/${assistant.ID}`);
 await page.locator('.chat-input textarea').fill('preserved QQ draft');
 await page.locator('.chat-header').getByRole('button',{name:'More',exact:true}).click();
 await page.getByRole('menuitem',{name:'Switch chat channel',exact:true}).click();
 const drawer=page.getByRole('dialog',{name:'Chat channel',exact:true});
 await expect(drawer).toHaveClass(/ui-drawer/);
 await expect(drawer.locator('.channel-choice')).toHaveCount(6);
 await expect(page.getByLabel('App Secret',{exact:true})).toHaveCount(0);
 for(const theme of ['light','dark'])for(const palette of ['forest','ocean','iris','sand','rose','monochrome']){
  await page.evaluate(({theme,palette})=>{document.documentElement.dataset.theme=theme;document.documentElement.dataset.palette=palette;},{theme,palette});
  await page.screenshot({path:test.info().outputPath(`channels-${theme}-${palette}.png`)});
 }
 await drawer.getByRole('button',{name:'QQ',exact:true}).click();
 await expect(drawer.locator('.channel-list')).toHaveAttribute('inert','');
 await expect(drawer.getByRole('button',{name:'Weixin',exact:true})).toHaveCount(0);
 await page.getByRole('button',{name:'Connect QQ with QR code',exact:true}).click();
 await expect(page.getByRole('img',{name:'Scan with QQ and confirm'})).toBeVisible();
 await expect(page.getByRole('dialog')).toHaveCount(1);
 await page.getByRole('button',{name:'Back to channels',exact:true}).click();
 await expect(drawer.getByRole('button',{name:'QQ',exact:true})).toBeFocused();
 await expect.poll(()=>canceled).toBe(1);
 await drawer.getByRole('button',{name:'QQ',exact:true}).click();
 await page.getByRole('button',{name:'Connect QQ with QR code',exact:true}).click();
 stage='confirmed';
 await expect(page.getByRole('button',{name:'Disconnect QQ',exact:true})).toBeVisible({timeout:15000});
 await expect(drawer.getByText('Bound',{exact:true})).toBeVisible();
 await expect(drawer.getByRole('button',{name:'Back to channels',exact:true})).toHaveCount(0);
 await drawer.getByRole('button',{name:'Close',exact:true}).click();
 await expect(page.locator('.message-composer--locked')).toContainText('Send messages in QQ');
 await page.reload();await page.getByRole('button',{name:'Manage QQ connection',exact:true}).click();
 await expect(page.getByRole('button',{name:'Disconnect QQ',exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Disconnect QQ',exact:true}).click();await page.getByRole('button',{name:'Confirm',exact:true}).click();
 await expect(drawer.getByRole('button',{name:'QQ',exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Disconnect QQ',exact:true})).toHaveCount(0);
 await drawer.getByRole('button',{name:'QQ',exact:true}).click();await page.getByRole('button',{name:'Enter credentials manually',exact:true}).click();
 await page.getByLabel('App ID',{exact:true}).fill('123456');await page.getByLabel('App Secret',{exact:true}).fill('fixture-secret');await page.getByRole('button',{name:'Connect bot',exact:true}).click();
 await expect(page.getByLabel('Pairing code',{exact:true})).toHaveValue('molr-fixture-pair');
 await page.getByRole('button',{name:'Cancel connection',exact:true}).click();
 await drawer.getByRole('button',{name:'Close',exact:true}).click();
 await expect(page.locator('.chat-input textarea')).toHaveValue('preserved QQ draft');
});
