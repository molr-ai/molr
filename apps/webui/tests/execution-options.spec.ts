import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

async function setup(page: Page, kind: string, count: number, modes: boolean[], ordinary = true, expiry?: string) {
 const requests: Record<string, unknown>[] = [];
 await page.route('**/api/auth/me', r => r.fulfill({ headers: { 'X-Molr-CSRF-Cookie': 'molr_dev_csrf' }, json: {} }));
 await page.route('**/api/sandbox', r => r.fulfill({ json: { Enabled:true,Capabilities:{OS:'linux',Shell:ordinary,Terminal:ordinary,FullAccess:true,FullAccessTerminal:true} } }));
 await page.route('**/api/resources?*', r => r.fulfill({ json:{Items:Array.from({length:count},(_,i)=>({ID:`compute-${i}`,Name:`Compute ${i}`,State:'available',SandboxID:'local'}))} }));
 await page.route('**/api/grants?*', r => r.fulfill({ json:{Items:Array.from({length:count},(_,i)=>modes.map(full=>({ResourceID:`compute-${i}`,Scope:{FullAccess:full,FullAccessScope:full?{mode:'unrestricted'}:undefined},Actions:['execute'],ExpiresAt:full?null:expiry}))).flat()} }));
 await page.route('**/api/assistants/fixture/shell-tasks*', r => r.fulfill({json:{Items:[]}}));
 await page.route(/\/api\/assistants\/fixture\/browser(?:\?.*)?$/, r => {
  if(r.request().method()==='POST'){requests.push(r.request().postDataJSON());return r.fulfill({status:412,json:{code:'failed_precondition'}});}
  return r.fulfill({json:{Available:true,SandboxAvailable:true,FullAccessAvailable:true}});
 });
 await page.route('**/api/assistants/fixture/terminals', r => {requests.push(r.request().postDataJSON());return r.fulfill({status:412,json:{code:'failed_precondition'}});});
 await page.goto(`/tests/fixtures/execution-options.html?kind=${kind}`);
 return {requests,start:page.getByRole('button',{name:kind==='browser'?'Start browser':'Open terminal',exact:true})};
}
for(const kind of ['browser','terminal']) {
 for(const modes of [[false],[true],[false,true]]) test(`${kind}: a single resource is hidden and submits the authorized mode ${modes}`,async({page})=>{
  const {requests,start}=await setup(page,kind,1,modes,!modes.every(Boolean));
  await expect(start).toBeEnabled();
  await expect(page.getByRole('combobox',{name:'Compute resource',exact:true})).toHaveCount(0);
  const selector=page.getByRole('combobox',{name:'Execution mode',exact:true});
  await expect(selector).toHaveCount(modes.length===2?1:0);
  if(modes.length===2){await expect(selector).toContainText('Sandbox');await selector.click();await page.getByRole('option',{name:'Full Access',exact:true}).click();}
  await start.click();await expect.poll(()=>requests.length).toBe(1);
  expect(requests[0]).toMatchObject({compute_id:'compute-0',full_access:modes.includes(true)});
 });
 test(`${kind}: multiple resources select the chosen resource`,async({page})=>{
  const {requests,start}=await setup(page,kind,2,[false]);
  const selector=page.getByRole('combobox',{name:'Compute resource',exact:true});
  await expect(selector).toBeVisible();await selector.click();await page.getByRole('option',{name:'Compute 1',exact:true}).click();
  await start.click();await expect.poll(()=>requests.length).toBe(1);expect(requests[0]).toMatchObject({compute_id:'compute-1',full_access:false});
 });
 test(`${kind}: no grant cannot start`,async({page})=>{
  const {requests,start}=await setup(page,kind,1,[]);
  await expect(start).toBeDisabled();
  expect(requests).toHaveLength(0);
 });
 test(`${kind}: expiring ordinary grant never silently selects Full Access`,async({page})=>{
  const now=new Date('2026-10-22T08:00:00Z');await page.clock.install({time:now});
  const {requests,start}=await setup(page,kind,1,[false,true],true,new Date(+now+60000).toISOString());
  await expect(start).toBeEnabled();await page.clock.fastForward(65000);
  await expect(start).toBeDisabled();
  expect(requests).toHaveLength(0);
  await page.getByRole('combobox',{name:'Execution mode',exact:true}).click();await page.getByRole('option',{name:'Full Access',exact:true}).click();
  await start.click();await expect.poll(()=>requests.length).toBe(1);expect(requests[0].full_access).toBe(true);
 });
}

for (const kind of ['browser', 'terminal']) test(`${kind}: program-only Full Access does not authorize generic sessions`, async ({ page }) => {
  const { requests, start } = await setup(page, kind, 1, [], false);
  await page.route('**/api/grants?*', r => r.fulfill({ json: { Items: [{ ResourceID: 'compute-0', Scope: { FullAccess: true, FullAccessScope: { mode: 'programs', programs: [{ executable: '/usr/bin/printf', arguments: 'any' }] } }, Actions: ['execute'] }] } }));
  await page.reload();
  await expect(start).toBeDisabled();
  expect(requests).toHaveLength(0);
});
