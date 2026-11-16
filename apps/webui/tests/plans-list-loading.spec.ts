import { test, expect, type Page } from '@playwright/test';
import { selectPlanRange } from './plan-controls';
type Source='watches'|'events';
function gate(){let release=()=>{},requested=()=>{};return {wait:new Promise<void>(resolve=>{release=resolve;}),requested:new Promise<void>(resolve=>{requested=resolve;}),release:()=>release(),notify:()=>requested()};}
async function fixture(page:Page){
  await page.goto('/');await page.getByLabel('Username',{exact:true}).fill('owner');await page.getByLabel('Password',{exact:true}).fill('webui-e2e-password-2026!');await page.getByRole('button',{name:'Sign in',exact:true}).click();await expect(page.getByRole('navigation',{name:'Molr'})).toBeVisible();
  const at=new Date(Date.now()+86400_000).toISOString();
  const common={State:'active',Executions:[],ExecutionCount:0,Occurrences:[],Links:{Version:1,Dependencies:[]},BlockedBy:[],Ready:true};
  const watch={...common,Ref:{Kind:'watch',ID:'watch-fixture'},Kind:'assistant_watch',Title:'Original watch',AssistantID:'fixture-assistant',Watch:{ID:'watch-fixture',Title:'Original watch',RunState:'paused',Version:1}};
  const todo={...common,Ref:{Kind:'todo',ID:'event-fixture'},Kind:'todo',Title:'Original event',AssistantID:'',Todo:{ID:'event-fixture',Title:'Original event',DueAt:at,Version:1}};
  const data={watches:{Items:[watch],NextCursor:'watch-next'},events:{Items:[{Key:'event-fixture',At:at,PlanKey:'event-fixture'}],Plans:{'event-fixture':todo},NextCursor:''}};
  const held:Partial<Record<Source,ReturnType<typeof gate>>>={},fail:Partial<Record<Source,boolean>>={},requests:{source:Source;url:URL}[]=[];
  await page.route(url=>url.pathname.startsWith('/api/plans'),async route=>{
    const url=new URL(route.request().url()),path=url.pathname;
    if(path==='/api/plans/watches'||path==='/api/plans/events'){
      const source:Source=path.endsWith('/watches')?'watches':'events',current=held[source],payload=structuredClone(data[source]),failed=fail[source];
      requests.push({source,url});current?.notify();if(current)await current.wait;
      if(source==='watches' && url.searchParams.has('cursor'))return route.fulfill({json:{Items:[{...watch,Title:'Second watch',Ref:{Kind:'watch',ID:'second-watch'}}],NextCursor:''}});
      await route.fulfill(failed?{status:400,json:{code:'invalid_argument'}}:{json:payload});return;
    }
    await route.fulfill({json:[]});
  });
  await page.goto('/#/plans');await expect(page.locator('.plans-week-calendar')).toBeVisible();
  return {data,held,fail,requests,hold(source:Source){return held[source]=gate();},async close(){Object.values(held).forEach(value=>value?.release());await page.unrouteAll({behavior:'wait'});}};
}
const rows=(page:Page)=>page.locator('.plans-master .plan-row');
const loading=(page:Page)=>page.locator('.plans-master .loading');
async function list(page:Page){await page.getByRole('button',{name:'List',exact:true}).click();}
async function refresh(page:Page){await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));}

test('list scopes request one source, keep watches separate and page only the selected list',async({page})=>{
  const f=await fixture(page),events=f.hold('events');
  try{
    await list(page);await events.requested;await expect(loading(page)).toHaveCount(1);await expect(rows(page)).toHaveCount(0);expect(f.requests.map(r=>r.source)).toEqual(['events']);
    events.release();await expect(rows(page)).toContainText(['Original event']);
    await page.getByRole('combobox',{name:'Plan type',exact:true}).click();await expect(page.getByRole('option',{name:'Continuous watches',exact:true})).toHaveCount(0);await page.keyboard.press('Escape');
    const watch=f.hold('watches');await selectPlanRange(page,'Continuous watches');await watch.requested;
    await expect(loading(page)).toHaveCount(1);await expect(rows(page)).toHaveCount(0);
    await expect(page.getByRole('combobox',{name:'Plan type',exact:true})).toHaveCount(0);
    await expect(page.getByRole('combobox',{name:'Owner',exact:true})).toBeVisible();await expect(page.getByRole('combobox',{name:'Status',exact:true})).toBeVisible();
    watch.release();await expect(rows(page)).toContainText(['Original watch']);
    const eventRequests=f.requests.filter(r=>r.source==='events').length;
    await page.getByRole('button',{name:'Load more watches',exact:true}).click();await expect(rows(page)).toContainText(['Original watch','Second watch']);
    expect(f.requests.filter(r=>r.source==='events')).toHaveLength(eventRequests);await expect(page.locator('.plans-load-more')).toHaveCount(0);
    await page.getByRole('button',{name:'Calendar',exact:true}).click();await expect(page.locator('.plans-week-calendar')).toBeVisible();await expect(rows(page)).toHaveCount(0);
    await page.getByRole('button',{name:'List',exact:true}).click();await expect(rows(page)).toContainText(['Original watch','Second watch']);
    await selectPlanRange(page,/Upcoming/);await expect(rows(page)).toContainText(['Original event']);await expect(page.getByRole('combobox',{name:'Plan type',exact:true})).toBeVisible();
    await selectPlanRange(page,/No date/);await expect(rows(page)).toHaveCount(0);await expect(page.locator('.plans-empty')).toBeVisible();
  }finally{await f.close();}
});

test('watch refresh preserves content, handles failure and does not poll or fetch ordinary plans',async({page})=>{
  const f=await fixture(page);f.data.watches.NextCursor='';
  try{
    await selectPlanRange(page,'Continuous watches');await expect(rows(page)).toContainText(['Original watch']);
    f.data.watches.Items[0].Title='Updated watch';let pending=f.hold('watches');await refresh(page);await pending.requested;
    await expect(rows(page)).toContainText(['Original watch']);await expect(loading(page)).toHaveCount(0);pending.release();await expect(rows(page)).toContainText(['Updated watch']);
    f.data.watches.Items[0].Title='Uncommitted watch';f.fail.watches=true;pending=f.hold('watches');await refresh(page);await pending.requested;pending.release();
    await expect(page.getByRole('button',{name:'Retry',exact:true})).toBeVisible();await expect(rows(page)).toContainText(['Updated watch']);await expect(loading(page)).toHaveCount(0);
    f.fail.watches=false;await page.getByRole('button',{name:'Retry',exact:true}).click();await expect(rows(page)).toContainText(['Uncommitted watch']);
    expect(f.requests.every(r=>r.source==='watches')).toBe(true);
    await page.clock.install();const count=f.requests.length;await page.clock.runFor(5000);expect(f.requests).toHaveLength(count);
    f.data.watches.Items=[];await refresh(page);await expect(page.locator('.plans-empty')).toContainText('No continuous watches');
  }finally{await f.close();}
});

test('a failed initial list can retry without displaying stale rows from the previous scope',async({page})=>{
  const f=await fixture(page);f.fail.watches=true;
  try{
    await list(page);await expect(rows(page)).toContainText(['Original event']);await selectPlanRange(page,'Continuous watches');
    await expect(page.locator('.plans-master')).toContainText('Unable to load plans.');await expect(rows(page)).toHaveCount(0);await expect(loading(page)).toHaveCount(0);
    f.fail.watches=false;await page.locator('.plans-master').getByRole('button',{name:'Retry',exact:true}).click();await expect(rows(page)).toContainText(['Original watch']);
  }finally{await f.close();}
});

test('switching away from an unfinished list ignores late responses',async({page})=>{
  const f=await fixture(page),events=f.hold('events');
  try{
    await list(page);await events.requested;await selectPlanRange(page,'Continuous watches');await expect(rows(page)).toContainText(['Original watch']);
    events.release();await page.unrouteAll({behavior:'wait'});await expect(rows(page)).toContainText(['Original watch']);
  }finally{events.release();await f.close();}
});

test('returning to a watch list retains its loaded pages while refreshing',async({page})=>{
  const f=await fixture(page);
  try{
    await selectPlanRange(page,'Continuous watches');await expect(rows(page)).toContainText(['Original watch']);
    await page.getByRole('button',{name:'Load more watches',exact:true}).click();await expect(rows(page)).toContainText(['Original watch','Second watch']);
    await page.getByRole('navigation').getByRole('button',{name:'Resources',exact:true}).click();
    await expect(page.getByRole('heading',{name:'Resources',exact:true})).toBeVisible();
    f.data.watches.Items[0].Title='Updated watch';const pending=f.hold('watches');
    await page.getByRole('navigation').getByRole('button',{name:'Plans',exact:true}).click();await selectPlanRange(page,'Continuous watches');await pending.requested;
    await expect(rows(page)).toContainText(['Original watch','Second watch']);await expect(loading(page)).toHaveCount(0);
    pending.release();await expect(rows(page)).toContainText(['Updated watch','Second watch']);
    await expect(page.getByRole('button',{name:'Load more watches',exact:true})).toHaveCount(0);
  }finally{await f.close();}
});
