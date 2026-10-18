import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

test('real terminal shares workspace, fences old controllers and reconnects', async ({ page, context }, info) => {
  test.skip(process.env.MOLR_E2E_SANDBOX_SERVICE !== '1', 'requires a delegated real sandbox');
  test.setTimeout(120_000);
  const errors: string[] = [];
  context.on('page', p => p.on('pageerror', e => errors.push(e.message)));
  page.on('pageerror', e => errors.push(e.message));
  const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
  await page.goto('/'); await page.getByLabel('Username', { exact: true }).fill('owner'); await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!'); await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const csrf = (await context.cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  async function post(path: string, data: object) {
    const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
    expect(response.status(), await response.text()).toBe(200); return response.json();
  }
  const sandbox = await (await page.request.get('/api/sandbox')).json();
  expect(sandbox.Enabled).toBe(true); expect(sandbox.Capabilities).toMatchObject({ Reason:'available',Shell:true,FileIsolation:true,ProcessIsolation:true,NetworkIsolation:true,ResourceLimits:true });
  const name = `Terminal ${info.project.name}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const provider = await post('/api/resources', { Name:name, Kind:'model', Provider:'openai-compatible', Binding:{ Endpoint:origin,AllowedOrigins:[origin],Models:[{Name:name,ContextLimit:32768,SupportsTools:true,Pricing:{Currency:'USD',InputMicrosPerMillion:0,OutputMicrosPerMillion:0}}] } });
  const models=await (await page.request.get('/api/models')).json();
  const assistant=await post('/api/assistants',{Name:name,ModelID:models.find((m:{ResourceID:string})=>m.ResourceID===provider.ID).ID,Budget:{Limit:{Currency:'USD',Micros:0},Period:'daily'}});
  const resources=(await (await page.request.get('/api/resources')).json()).Items;
  const compute=resources.find((r:{Kind:string})=>r.Kind==='compute')??await post('/api/sandboxes/resource',{SandboxID:'local',Kind:'compute',SourceID:'compute',Name:'Terminal compute'});
  const denied=await page.request.post(`/api/assistants/${assistant.ID}/terminals`,{headers:{Origin:base,'X-CSRF-Token':csrf},data:{request_id:crypto.randomUUID(),client_id:'denied',compute_id:compute.ID}});
  expect(denied.ok()).toBe(false);
  await post('/api/grants',{AssistantID:assistant.ID,ResourceID:compute.ID,Actions:['execute'],Scope:{},Constraints:{compute:{cpu_percent:100,memory_bytes:536870912,processes:64,max_duration_seconds:180,unlimited_duration:false,temp_bytes:67108864}}});
  async function open(target:Page){await target.goto(`/#/assistants/${assistant.ID}`);await target.getByRole('button',{name:/^Terminal(?: \(\d+\))?$/}).click();}
  await open(page);await page.getByRole('button',{name:'New session',exact:true}).click();
  await expect(page.getByText('You control',{exact:true})).toBeVisible();
  await expect(page.getByRole('button', { name: 'Terminal (1)', exact: true })).toBeVisible();
  await expect(page.getByRole('button',{name:'Ctrl-C',exact:true})).toBeEnabled();
  const list=async()=> (await (await page.request.get(`/api/assistants/${assistant.ID}/shell-tasks`)).json()).Items;
  const initial=(await list()).find((t:{State:string})=>t.State==='running'); expect(initial.Terminal.Control).toBe('user');
  async function type(target:Page,command:string){const input=target.getByRole('textbox',{name:'Terminal input',exact:true});await expect(input).toBeEnabled();await input.focus();await target.keyboard.insertText(command);await target.keyboard.press('Enter');}
  await type(page,"printf '\\344\\270\\255\\346\\226\\207-terminal-ok\\n' > terminal-proof.txt; cat terminal-proof.txt");
  await expect(page.locator('.xterm-screen')).toContainText('中文-terminal-ok');
  const file=await (await page.request.get(`/api/assistants/${assistant.ID}/workspace?action=read&path=terminal-proof.txt`)).json();expect(file.Text).toContain('中文-terminal-ok');
  await page.getByRole('button',{name:'New session',exact:true}).click();
  await expect(page.getByRole('tab')).toHaveCount(2);
  await expect(page.getByRole('button', { name: 'Terminal (2)', exact: true })).toBeVisible();
  const other=(await list()).find((t:{ID:string;State:string})=>t.State==='running'&&t.ID!==initial.ID);
  const initialTab=page.locator(`[role="tab"][id$="-${initial.ID}"]`),otherTab=page.locator(`[role="tab"][id$="-${other.ID}"]`);
  await expect(otherTab).toHaveAttribute('aria-selected','true');
  await expect.poll(async()=>{
    const tab=await otherTab.locator('..').boundingBox(),list=await page.getByRole('tablist',{name:'Active sessions'}).boundingBox();
    return !!tab && !!list && tab.x>=list.x-1 && tab.x+tab.width<=list.x+list.width+1;
  },{message:'selected session, including its close button, stays fully visible'}).toBe(true);
  await initialTab.click();await expect(page.locator('.xterm-screen')).toContainText('中文-terminal-ok');
  await otherTab.click();
  await page.locator('.terminal-tab').filter({has:otherTab}).getByRole('button',{name:/^End session:/}).click();
  await expect(page.getByRole('tab')).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Terminal (1)', exact: true })).toBeVisible();
  await expect(initialTab).toHaveAttribute('aria-selected','true');
  await expect(page.locator('.xterm-screen')).toContainText('中文-terminal-ok');
  let listReads=0;
  const countLists=(request:import('@playwright/test').Request)=>{if(request.method()==='GET'&&request.url().endsWith(`/api/assistants/${assistant.ID}/shell-tasks`))listReads++;};
  page.on('request',countLists);
  for(let i=0;i<12;i++){await type(page,'true');await page.waitForTimeout(170);}
  page.off('request',countLists);
  expect(listReads,'typing must not reload every task list for every input chunk').toBeLessThan(12);
  const second=await context.newPage();await open(second);await second.getByRole('button',{name:'Take control',exact:true}).click();
  await expect(second.getByText('You control',{exact:true})).toBeVisible();await expect(page.getByText('Another window controls',{exact:true})).toBeVisible();
  const stale=await page.request.post(`/api/assistants/${assistant.ID}/shell-tasks`,{headers:{Origin:base,'X-CSRF-Token':csrf},data:{action:'write',task_id:initial.ID,generation:initial.Terminal.Generation,client_id:initial.Terminal.ControllerClientID,sequence:999,data:'echo forbidden\n'}});expect(stale.status()).toBe(409);
  await expect(second.getByRole('button',{name:'Ctrl-C',exact:true})).toBeEnabled();await type(second,'sleep 30');await second.getByRole('button',{name:'Ctrl-C',exact:true}).click();await type(second,'echo after-interrupt');await expect(second.locator('.xterm-screen')).toContainText('after-interrupt');
  await second.getByRole('button',{name:'Close terminal window',exact:true}).click();await second.getByRole('button',{name:/^Terminal(?: \(\d+\))?$/}).click();await expect(second.getByText('You control',{exact:true})).toBeVisible();await expect(second.locator('.xterm-screen')).toContainText('中文-terminal-ok');
  await second.reload();await second.getByRole('button',{name:/^Terminal(?: \(\d+\))?$/}).click();await second.getByRole('button',{name:'Take control',exact:true}).click();await expect(second.getByText('You control',{exact:true})).toBeVisible();
  const appearance = second.getByRole('combobox', { name: 'Terminal appearance' });
  const generation = (await list()).find((t:{ID:string}) => t.ID === initial.ID).Terminal.Generation;
  await second.evaluate(() => { document.documentElement.dataset.theme = 'light'; });
  await appearance.click(); await second.getByRole('option', { name: 'Dark', exact: true }).click();
  await expect(second.locator('.terminal-drawer')).toHaveAttribute('data-theme', 'dark');
  expect(await second.locator('html').getAttribute('data-theme')).toBe('light');
  await expect(second.locator('.xterm-screen')).toContainText('中文-terminal-ok');
  expect((await list()).find((t:{ID:string}) => t.ID === initial.ID).Terminal.Generation).toBe(generation);
  await second.reload(); await second.getByRole('button',{name:/^Terminal(?: \(\d+\))?$/}).click();
  await appearance.click();
  await expect(second.getByRole('option', { name: 'Dark', exact: true })).toHaveAttribute('aria-selected', 'true');
  await appearance.press('Escape');
  await expect(second.locator('.terminal-drawer')).toBeVisible();
  await second.getByRole('button',{name:'Take control',exact:true}).click();
  await appearance.click(); await second.getByRole('option', { name: 'Light', exact: true }).click();
  await second.evaluate(() => { document.documentElement.dataset.theme = 'dark'; });
  await expect(second.locator('.terminal-drawer')).toHaveAttribute('data-theme', 'light');
  await appearance.click(); await second.getByRole('option', { name: 'Follow app', exact: true }).click();
  await expect(second.locator('.terminal-drawer')).toHaveAttribute('data-theme', 'dark');
  await expect(second.locator('.ui-dialog__header [role="tab"]')).toHaveCount(1);
  for(const palette of ['forest','ocean','iris','sand','rose','monochrome'])for(const theme of ['light','dark']){
    await second.evaluate(({palette,theme})=>{document.documentElement.dataset.palette=palette;document.documentElement.dataset.theme=theme},{palette,theme});
    await expect(second.locator('.terminal-screen')).toBeVisible();
    await expect.poll(()=>second.evaluate(()=>{
      const background=(selector:string)=>getComputedStyle(document.querySelector(selector)!).backgroundColor;
      return background('.terminal-screen')===background('.xterm-viewport') && background('.terminal-screen')===background('.terminal-drawer');
    }),{message:'terminal viewport and window must share the active theme background'}).toBe(true);
    const bounds=await second.locator('.terminal-drawer').boundingBox();expect(bounds!.x).toBeGreaterThanOrEqual(0);expect(bounds!.x+bounds!.width).toBeLessThanOrEqual(second.viewportSize()!.width+1);
    await second.screenshot({animations:'disabled',path:info.outputPath(`terminal-${palette}-${theme}.png`)});
  }
  await second.getByRole('button',{name:'Return to assistant',exact:true}).click();await expect(second.getByText('Assistant controls',{exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Take control',exact:true}).click();await page.getByRole('button',{name:'End session',exact:true}).click();await expect(page.getByRole('tab')).toHaveCount(0); await expect(page.getByRole('button',{name:'Open terminal',exact:true})).toBeVisible();
  expect((await list()).filter((t:{State:string})=>t.State==='running')).toHaveLength(0);
  const entry = page.getByRole('button', { name: 'Terminal', exact: true });
  await expect(entry).toBeVisible(); await expect(entry.locator('.badge')).toHaveCount(0);
  expect(errors).toEqual([]);
});
