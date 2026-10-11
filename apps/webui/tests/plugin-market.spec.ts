import { test, expect, type Page } from '@playwright/test';
import type { PluginPackage } from '@molr/sdk/plugins';
const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object, status = 200) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data });
  expect(response.status(), await response.text()).toBe(status); return response.json();
}
async function login(page: Page, username = 'owner', password = 'webui-e2e-password-2026!') {
  await page.goto('/#/extensions'); await page.getByLabel('Username', { exact: true }).fill(username); await page.getByLabel('Password', { exact: true }).fill(password); await page.getByRole('button', { name: 'Sign in', exact: true }).click(); await expect(page.getByRole('heading', { name: 'Plugins', exact: true })).toBeVisible();
}
const card = (page: Page, name: string) => page.locator('.plugin-card').filter({ has: page.getByRole('heading', { name, exact: true }) });
async function menu(page: Page, name: string, action: string) { await card(page,name).getByRole('button', { name: `More · ${name}`, exact: true }).click(); await page.getByRole('menuitem', { name: action, exact: true }).click(); }

test('marketplace group authorization, independent enablement and optional settings', async ({ page, browser }, info) => {
  test.setTimeout(60000); await login(page);
  const id = `fixture.market-${info.project.name}`, name = `Market ${info.project.name}`;
  const pkg: PluginPackage = { manifest: { manifestVersion: 1, pluginApi: 3, id, name, description: 'A plugin with administrator configuration and no personal settings.', version: '1.0.0', adminSettings: { label: { type: 'string', title: 'Shared label', default: 'Shared default' } }, contributes: { themes: [], tools: [], pages: [{ id: 'home', title: 'Market home', entry: 'page.html', capabilities: [] }] } }, files: { 'page.html': `<p>Page without settings</p><script>parent.postMessage({type:'molr.plugin.ready',apiVersion:2},'*')</script>` } };
  const installed = await post(page, '/api/plugins/install', pkg);
  await post(page, '/api/plugins/configure', { ID: id, ExpectedVersion: installed.Version, Enabled: true, Grants: [], Bindings: [] });
  const mutate = (path: string, data: object) => post(page,path,{...data,Mutation:{IdempotencyKey:crypto.randomUUID()}});
  const group = await mutate('/api/admin/groups', { Group: { Name: `Market group ${info.project.name}`, Resources: [] } });
  const username = `market-${info.project.name}`; await mutate('/api/admin/users', { Username: username, DisplayName: username, Password: 'market-member-password-2026!', GroupID: group.ID });
  await page.reload(); await expect(card(page,name).getByRole('button',{name:'Configure',exact:true})).toHaveCount(0);
  await menu(page,name,'Admin settings'); await page.getByLabel('Shared label',{exact:true}).fill('Only administrators edit this'); await page.getByRole('button',{name:'Save admin settings',exact:true}).click(); await expect(page.getByRole('status').filter({hasText:'Saved.'})).toBeVisible();
  const context = await browser.newContext({ baseURL: base }), member = await context.newPage(); await login(member,username,'market-member-password-2026!');
  await expect(card(member,name)).toHaveCount(0); await expect(member.getByRole('button',{name:'Import plugin package',exact:true})).toHaveCount(0);
  await post(member,'/api/plugins/ui',{ID:id,Digest:installed.Digest,Kind:'page',Contribution:'home',Method:'document'},403);
  await page.goto('/#/extensions'); await menu(page,name,'Authorize groups');
  const dialog = page.getByRole('dialog',{name:`${name} · Authorize groups`,exact:true});
  const row = dialog.locator('.plugin-group-access').filter({has:page.getByRole('checkbox',{name:group.Name,exact:true})});
  await row.getByRole('checkbox').check(); await row.getByRole('button',{name:'Save access',exact:true}).click(); await expect(row.getByRole('button',{name:'Save access',exact:true})).toBeDisabled();
  await dialog.locator('footer').getByRole('button',{name:'Close',exact:true}).click(); await member.reload(); await expect(card(member,name)).toBeVisible();
  await expect(card(member,name).getByRole('button',{name:'Configure',exact:true})).toHaveCount(0);
  await menu(member,name,'Open · Market home'); await expect(member.frameLocator('iframe').getByText('Page without settings')).toBeVisible();
  await post(member,'/api/plugins/settings/read',{ID:id,Digest:installed.Digest,Scope:'admin'},403);
  await post(member,'/api/plugins/settings/read',{ID:id,Digest:installed.Digest},404);
  await post(member,'/api/plugins/configure',{ID:id,ExpectedVersion:2,Enabled:false,Grants:[],Bindings:[]},403);
  await member.goto('/#/extensions'); await card(member,name).getByRole('switch',{name:'Enable plugin',exact:true}).click(); await expect(card(member,name).getByRole('button',{name:'Open',exact:true})).toBeDisabled();
  await post(member,'/api/plugins/ui',{ID:id,Digest:installed.Digest,Kind:'page',Contribution:'home',Method:'document'},403);
  await post(page,'/api/plugins/ui',{ID:id,Digest:installed.Digest,Kind:'page',Contribution:'home',Method:'document'});
  await member.reload(); await expect(card(member,name).getByRole('switch')).not.toBeChecked(); await card(member,name).getByRole('switch').click(); await expect(card(member,name).getByRole('button',{name:'Open',exact:true})).toBeEnabled();
  await menu(page,name,'Authorize groups'); await expect(row.getByRole('checkbox')).toBeChecked(); await row.getByRole('checkbox').uncheck(); await row.getByRole('button',{name:'Save access',exact:true}).click(); await expect(row.getByRole('button',{name:'Save access',exact:true})).toBeDisabled();
  await member.reload(); await expect(card(member,name)).toHaveCount(0);
  await post(member,'/api/plugins/ui',{ID:id,Digest:installed.Digest,Kind:'page',Contribution:'home',Method:'document'},403);
  await dialog.locator('footer').getByRole('button',{name:'Close',exact:true}).click();
  await page.getByLabel('Search plugins',{exact:true}).fill(name); await page.screenshot({path:info.outputPath('plugin-market.png')});
  await page.goto('/#/account'); await expect(page.locator('.settings-nav').getByRole('button',{name:'Plugins',exact:true})).toHaveCount(0);
  await page.goto('/#/settings'); await expect(page.locator('.settings-nav').getByRole('button',{name:'Plugins',exact:true})).toHaveCount(0);
  await context.close();
});

test('a declared custom settings page is used directly', async ({ page }, info) => {
  await login(page); const id = `fixture.custom-${info.project.name}`;
  const pkg: PluginPackage = { manifest: { manifestVersion: 1, pluginApi: 3, id, name: `Custom ${info.project.name}`, description: 'Custom settings', version: '1.0.0', settings: { language: { type: 'string', title: 'Host language field', default: 'en' } }, settingsPage: { id:'settings',title:'Custom settings',entry:'ui.html',capabilities:['settings.read'] }, contributes:{themes:[],tools:[],pages:[{id:'home',title:'Custom home',entry:'ui.html',capabilities:['settings.read']}]}},files:{'ui.html':`<p id="value">waiting</p><script>addEventListener('message',e=>{if(e.source!==parent||e.data?.type!=='molr.plugin.connect')return;const port=e.ports[0];port.onmessage=({data})=>{document.getElementById('value').textContent=data.error||('Custom value: '+data.value.Values.language)};port.postMessage({id:1,method:'settings.get'})});parent.postMessage({type:'molr.plugin.ready',apiVersion:2},'*')</script>`}};
  const v = await post(page,'/api/plugins/install',pkg); await post(page,'/api/plugins/configure',{ID:id,ExpectedVersion:v.Version,Enabled:true,Grants:['settings.read'],Bindings:[]});
  await page.reload(); await card(page,pkg.manifest.name).getByRole('button',{name:'Configure',exact:true}).click(); await expect(page.frameLocator('iframe').getByText('Custom value: en',{exact:true})).toBeVisible(); await expect(page.getByLabel('Host language field',{exact:true})).toHaveCount(0);
});
