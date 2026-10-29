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
// Auth and assistant creation use the isolated Runtime; external Feishu requests are simulated.
test('Feishu private chat connects, preserves drafts and disconnects', async ({ page }, info) => {
 await page.goto('/');
 await page.getByLabel('Username', { exact: true }).fill('owner');
 await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
 await page.getByRole('button', { name: 'Sign in', exact: true }).click();
 await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
 const name = `Feishu ${info.project.name}-${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
 const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1000000 } }] } });
 const models = await (await page.request.get('/api/models')).json();
 const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });

 let bound = false, stage = 'waiting';
 const state = () => ({ Channel: bound ? 'feishu' : 'webui', State: bound ? 'connected' : 'disconnected', AppID: bound ? 'cli_fixture' : '', LastInboundAt: null, ConnectedAt: null, LastError: '' });
 await page.route(`**/api/assistants/${assistant.ID}/chat-channel`, route => route.fulfill({ json: { Channel: state().Channel } }));
 await page.route(`**/api/assistants/${assistant.ID}/chat-channels`, route => route.fulfill({ json: channelSummaries('feishu', state()) }));
 await page.route(`**/api/assistants/${assistant.ID}/feishu*`, route => route.fulfill({ json: state() }));
 await page.route('**/api/feishu/login', route => route.fulfill({ json: { ID: 'registration', Status: 'waiting', QRCode: 'https://accounts.feishu.cn/authorize?test=fixture', ExpiresAt: new Date(Date.now()+600000).toISOString() } }));
 await page.route('**/api/feishu/poll', route => { if(stage==='confirmed')bound=true;return route.fulfill({ json:{ID:'registration',Status:stage,QRCode:'https://accounts.feishu.cn/authorize?test=fixture',ExpiresAt:new Date(Date.now()+600000).toISOString()} }); });
 await page.route('**/api/feishu/cancel', route => route.fulfill({ json: null }));
 await page.route('**/api/feishu/disconnect', route => {bound=false;return route.fulfill({json:null});});
 await page.route('**/api/feishu/connect', route => {const input=route.request().postDataJSON();expect(input.OpenID).toBe('ou_fixture');expect(input.AppSecret).toBe('fixture-secret');bound=true;return route.fulfill({json:state()});});
 await page.goto(`/#/assistants/${assistant.ID}`);
 await page.locator('.chat-input textarea').fill('preserved Feishu draft');
 await page.locator('.chat-header').getByRole('button',{name:'More',exact:true}).click();
 await page.getByRole('menuitem',{name:'Switch chat channel',exact:true}).click();
 await page.getByRole('button',{name:'Feishu',exact:true}).click();
 await page.getByRole('button',{name:'Connect Feishu',exact:true}).click();
 await expect(page.getByRole('img',{name:'Scan with Feishu to authorize'})).toBeVisible();
 await expect(page.getByText(/Chat content on this channel is transmitted to Feishu/)).toBeVisible();
 for(const theme of ['light','dark'])for(const palette of ['forest','ocean','iris','sand','rose','monochrome']){
  await page.evaluate(({theme,palette})=>{document.documentElement.dataset.theme=theme;document.documentElement.dataset.palette=palette;},{theme,palette});
  await expect(page.locator('.weixin-qr rect')).toHaveCSS('fill','rgb(255, 255, 255)');
  await expect(page.locator('.weixin-qr path')).toHaveCSS('fill','rgb(0, 0, 0)');
  await page.screenshot({path:test.info().outputPath(`feishu-${theme}-${palette}.png`)});
 }
 stage='confirmed';
 await expect(page.getByRole('button',{name:'Disconnect Feishu',exact:true})).toBeVisible({timeout:15000});
 await expect(page.getByRole('button',{name:'Connect Weixin',exact:true})).toHaveCount(0);
 await page.getByRole('dialog',{name:'Chat channel',exact:true}).getByRole('button',{name:'Close',exact:true}).click();
 await expect(page.locator('.chat-input .message-composer--locked')).toContainText('Send messages in Feishu');
 await page.reload();
 await expect(page.locator('.chat-input textarea')).toHaveCount(0);
 await page.getByRole('button',{name:'Manage Feishu connection',exact:true}).click();
 await page.getByRole('button',{name:'Disconnect Feishu',exact:true}).click();
 await page.getByRole('button',{name:'Confirm',exact:true}).click();
 await expect(page.getByRole('dialog',{name:'Disconnect and restore WebUI chat?',exact:true})).toHaveCount(0);
 await page.getByRole('button',{name:'Feishu',exact:true}).click();
 await page.getByRole('button',{name:'Use an existing app',exact:true}).click();
 await page.getByLabel('App ID',{exact:true}).fill('cli_fixture');
 await page.getByLabel('App Secret',{exact:true}).fill('fixture-secret');
 await page.getByLabel('Your Open ID',{exact:true}).fill('ou_fixture');
 await page.getByRole('button',{name:'Connect app',exact:true}).click();
 await expect(page.getByRole('dialog',{name:'Use an existing app',exact:true})).toHaveCount(0);
 await expect(page.getByRole('button',{name:'Disconnect Feishu',exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Disconnect Feishu',exact:true}).click();
 await page.getByRole('button',{name:'Confirm',exact:true}).click();
 await expect(page.getByRole('dialog',{name:'Disconnect and restore WebUI chat?',exact:true})).toHaveCount(0);
 await page.getByRole('dialog',{name:'Chat channel',exact:true}).getByRole('button',{name:'Close',exact:true}).click();
 await expect(page.locator('.chat-input textarea')).toHaveValue('preserved Feishu draft');
});
