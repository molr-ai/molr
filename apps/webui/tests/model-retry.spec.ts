import { readFileSync } from 'node:fs';
import { test, expect, type Page } from '@playwright/test';

const port = process.env.MOLR_TEST_PORT ?? '5193';
const base = `http://127.0.0.1:${port}`;
const modelOrigin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? '18882'}`;
const password = process.env.MOLR_RETRY_PASSWORD_FILE ? readFileSync(process.env.MOLR_RETRY_PASSWORD_FILE, 'utf8').trim() : 'webui-e2e-password-2026!';
async function post(page: Page, path: string, data: object, mutation = true) {
 const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
 const response = await page.request.post(path, {headers:{Origin:base,'X-CSRF-Token':csrf},data:mutation ? {...data,Mutation:{IdempotencyKey:crypto.randomUUID()}} : data});
 expect(response.status(),await response.text()).toBe(200);return response.json();
}
async function prepare(page: Page, name: string) {
 await page.goto('/');
 await page.getByRole('textbox',{name:'Username',exact:true}).fill('owner');
 await page.getByLabel('Password',{exact:true}).fill(password);
 await page.getByRole('button',{name:'Sign in',exact:true}).click();
 await expect(page.getByRole('navigation',{name:'Molr'})).toBeVisible();
 const settings=await (await page.request.get('/api/settings')).json();
 await post(page,'/api/settings',{Settings:{...settings,WorkbenchVisible:true},Mutation:{IdempotencyKey:crypto.randomUUID(),ExpectedVersion:settings.Version}},false);
 const resource=await post(page,'/api/resources',{Name:name,Kind:'model',Provider:'openai-compatible',Binding:{Endpoint:modelOrigin,AllowedOrigins:[modelOrigin],Models:[{Name:name,ContextLimit:32768,SupportsTools:true,Pricing:{Currency:'USD',InputMicrosPerMillion:0,OutputMicrosPerMillion:0,CachedInputMicrosPerMillion:0}}]}});
 const models=await(await page.request.get('/api/models')).json();
 const assistant=await post(page,'/api/assistants',{Name:name,ModelID:models.find((m:any)=>m.ResourceID===resource.ID).ID,Budget:{Limit:{Currency:'USD',Micros:0},Period:'daily'}});
 await page.goto(`/#/workbench/${assistant.ID}`);await page.reload();
 await expect(page.getByRole('heading',{name:'Workbench',exact:true})).toBeVisible();
 // Birth is recorded even before the workbench is shown. Keep its independent
 // execution collapsed while inspecting the following user-triggered retries.
 await expect.poll(async()=>{
  const jobs=(await(await page.request.get(`/api/jobs?assistant_id=${assistant.ID}`)).json()).Items;
  return jobs.some((job:any)=>job.Event.Kind==='born' && ['done','failed'].includes(job.State));
 },{timeout:30000}).toBe(true);
 const rows=(await(await page.request.get(`/api/debug/runs?assistant_id=${assistant.ID}`)).json()).Items;
 const born=rows.filter((run:any)=>run.Trigger==='born').sort((a:any,b:any)=>a.Attempt-b.Attempt)[0];
 assistant.birthRoot=born.ID;
 assistant.birthModelCount=0;
 for(const run of rows.filter((run:any)=>run.Trigger==='born')){const detail=await(await page.request.get(`/api/debug/runs/${run.ID}`)).json();assistant.birthModelCount+=detail.Steps.filter((step:any)=>step.Kind==='model.start').length;}
 assistant.birthReplies=(await(await page.request.get(`/api/conversations/${assistant.ConversationID}/messages`)).json()).Items.filter((m:any)=>m.Sender==='assistant').length;
 await page.locator(`[data-record-id="run:${born.ID}"] .trajectory-fold`).click();
 return assistant;
}

test('workbench retains each real retry request, response, timing and final outcome',async({page},info)=>{
 test.setTimeout(120000);
 const assistant=await prepare(page,`Retry success ${info.project.name} ${Date.now()}`);
 await post(page,'/api/conversations/messages',{ConversationID:assistant.ConversationID,Content:{Kind:'text',Text:'Test two rate limits, then publish exactly once.'}});
 const timeline=page.getByRole('region',{name:'Execution timeline',exact:true});
 await expect(timeline.locator('.trajectory-run-status').filter({hasText:'Retrying'}).first()).toBeVisible();
 await expect.poll(async()=> (await (await page.request.get(`/api/jobs?assistant_id=${assistant.ID}`)).json()).Items.find((job:any)=>job.Event.Kind==='user_message')?.State,{timeout:25000}).toBe('done');
 const models=timeline.locator('.trajectory-record--model');
 await expect(timeline.locator('.trajectory-run')).toHaveCount(2);
 await expect(page.locator('.trajectory-time-bar--trigger')).toHaveCount(2);
 await expect(page.locator('.trajectory-time-bar--trigger').last()).toHaveAttribute('data-state', 'succeeded');
 await expect(timeline.locator('.trajectory-run-status').last()).not.toContainText('Retrying');
 await expect(models.nth(2)).toContainText('Retry 2/3');
 for(let index=0;index<2;index++){
  if (index > 0) await expect(models.nth(index)).toContainText(`Retry ${index}/3`);
  else await expect(models.nth(index)).not.toContainText('Retry 0/3');
  await expect(models.nth(index)).not.toContainText('HTTP 429');
  await expect(models.nth(index)).toContainText('Next retry');
  await expect(models.nth(index).locator('.trajectory-state')).toContainText('Failed');
 }
 await expect(timeline).not.toContainText('HTTP 200');
 await expect(timeline).not.toContainText('Request attempt');
 await expect(models.nth(2).locator('.trajectory-state')).toContainText('Completed');
 await models.first().locator('.trajectory-record-body').click();
 const inspector=page.getByRole('region',{name:'Step details',exact:true});
 await expect(inspector.getByTestId('model-retry-details')).toContainText('1/4');
 await expect(inspector.getByTestId('model-retry-details')).toContainText('429');
 await inspector.getByRole('tab',{name:'Model response',exact:true}).click();
 await inspector.getByRole('tab',{name:'Response metadata',exact:true}).click();
 await expect(inspector).toContainText('429');
 await expect(inspector).not.toContainText('Retry fixture attempt 1');
 await inspector.getByRole('tab',{name:'Model request',exact:true}).click();
 await expect(inspector).toContainText(assistant.ConversationID);
 await expect(inspector).toContainText('user_message');
 await inspector.getByRole('button',{name:'Close',exact:true}).click();
 await models.nth(1).locator('.trajectory-record-body').click();
 await inspector.getByRole('tab',{name:'Model response',exact:true}).click();
 await inspector.getByRole('tab',{name:'Response metadata',exact:true}).click();
 await expect(inspector).toContainText('429');
 await expect(inspector).not.toContainText('Retry fixture attempt 1');
 await inspector.getByRole('button',{name:'Close',exact:true}).click();
 await models.nth(2).locator('.trajectory-record-body').click();
 await inspector.getByRole('tab',{name:'Model response',exact:true}).click();
 await inspector.getByRole('tab',{name:'Response metadata',exact:true}).click();
 await expect(inspector).toContainText('200');
 await inspector.getByRole('button',{name:'Close',exact:true}).click();
 await timeline.locator('.trajectory-run-bar .trajectory-fold').last().click();
 await expect(models).toHaveCount(0);
 await page.locator('.trajectory-time-bar--model').nth(assistant.birthModelCount + 2).click();
 await expect(models.nth(2)).toContainText('Retry 2/3');
 await expect(inspector.getByTestId('model-retry-details')).toContainText('3/4');
 await inspector.getByRole('button',{name:'Close',exact:true}).click();
 await expect(timeline.locator('.trajectory-run')).toHaveCount(2);
 const messages=await(await page.request.get(`/api/conversations/${assistant.ConversationID}/messages`)).json();
 expect(messages.Items.filter((m:any)=>m.Sender==='assistant')).toHaveLength(assistant.birthReplies + 1);
 await page.reload();
 await page.locator(`[data-record-id="run:${assistant.birthRoot}"] .trajectory-fold`).click();
 await expect(timeline.locator('.trajectory-record--model').nth(2)).toContainText('Retry 2/3');
 if(process.env.MOLR_RETRY_CAPTURE) await page.screenshot({path:`${process.env.MOLR_RETRY_CAPTURE}/retry-${info.project.name}.png`,fullPage:true});
});

test('workbench shows all exhausted attempts and no successful tool effect',async({page},info)=>{
 test.setTimeout(120000);
 const assistant=await prepare(page,`Retry exhausted ${info.project.name} ${Date.now()}`);
 await post(page,'/api/conversations/messages',{ConversationID:assistant.ConversationID,Content:{Kind:'text',Text:'Exhaust retry attempts.'}});
 await expect.poll(async()=> (await (await page.request.get(`/api/jobs?assistant_id=${assistant.ID}`)).json()).Items.find((job:any)=>job.Event.Kind==='user_message')?.State,{timeout:30000}).toBe('failed');
 const timeline=page.getByRole('region',{name:'Execution timeline',exact:true});
 await expect(timeline.locator('.trajectory-record--model')).toHaveCount(4);
 await expect(timeline.locator('.trajectory-run')).toHaveCount(2);
 await expect(timeline.locator('.trajectory-run-status').last()).toContainText('Failed');
 await expect(page.locator('.trajectory-time-bar--trigger')).toHaveCount(2);
 await expect(page.locator('.trajectory-time-bar--trigger').last()).toHaveAttribute('data-state', 'failed');
 await expect(timeline.locator('.trajectory-record--tool')).toHaveCount(0);
 await expect(timeline.locator('.trajectory-record--model').last()).toContainText('Retry 3/3');
 await expect(timeline.locator('.trajectory-record--model').last()).toContainText('retry or waiting limit');
 await timeline.locator('.trajectory-record--model').last().locator('.trajectory-record-body').click();
 const inspector=page.getByRole('region',{name:'Step details',exact:true});
 await expect(inspector.getByTestId('model-retry-details')).toContainText('4/4');
 await inspector.getByRole('tab',{name:'Model response',exact:true}).click();
 await inspector.getByRole('tab',{name:'Response metadata',exact:true}).click();
 await expect(inspector).toContainText('429');
 await expect(inspector).not.toContainText('Retry fixture attempt 4');
});
