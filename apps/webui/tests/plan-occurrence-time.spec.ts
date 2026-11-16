import { test, expect, type Page } from '@playwright/test';

async function setup(page: Page) {
  const now=Date.now()+60_000,at=new Date(now+10_000).toISOString();
  await page.clock.install({time:new Date(now-60_000)});
  await page.addInitScript(()=>{
    const timers=new Set<number>();
    (window as any).occurrenceTimers=timers;
    const schedule=window.setTimeout.bind(window),cancel=window.clearTimeout.bind(window);
    window.setTimeout=((handler:TimerHandler,timeout?:number,...args:unknown[])=>{
      const id=schedule(handler,timeout,...args);
      if(typeof handler==='function' && handler.name==='syncOccurrenceTime')timers.add(id);
      return id;
    }) as typeof window.setTimeout;
    Object.defineProperty(window,'clearTimeout',{configurable:true,value:(id?:number)=>{if(id!==undefined)timers.delete(id);cancel(id);}});
  });
  await page.goto('/');await page.getByLabel('Username',{exact:true}).fill('owner');
  await page.getByLabel('Password',{exact:true}).fill('webui-e2e-password-2026!');
  await page.getByRole('button',{name:'Sign in',exact:true}).click();
  await expect(page.getByRole('navigation',{name:'Molr'})).toBeVisible();
  const plan:any={Ref:{Kind:'schedule',ID:'occurrence-time'},Title:'Occurrence time',Kind:'personal_schedule',State:'active',AssistantID:'',
    Schedule:{ID:'occurrence-time',Kind:'personal_schedule',State:'active',Title:'Occurrence time',Instructions:'Time fixture',Trigger:{StartAt:at,Timezone:'Asia/Shanghai',Recurrence:'FREQ=DAILY;COUNT=3',MissedPolicy:'skip'},NextAt:at,Version:1},
    Links:{Dependencies:[],Version:1},Executions:[],ExecutionCount:0,Occurrences:[at],BlockedBy:[],Ready:true};
  let requests=0;const pending=new Set();
  page.on('request',request=>{if(new URL(request.url()).pathname.startsWith('/api/plans')){requests++;pending.add(request);}});
  page.on('requestfinished',request=>pending.delete(request));page.on('requestfailed',request=>pending.delete(request));
  await page.route(url=>url.pathname.startsWith('/api/plans'),route=>{
    const url=new URL(route.request().url()),path=url.pathname;
    if(path==='/api/plans/selection')return route.fulfill({json:{Plan:plan,At:url.searchParams.get('execution_id')?at:url.searchParams.get('at')}});
    if(path.endsWith('/history'))return route.fulfill({json:{Items:plan.Executions,NextCursor:''}});
    if(path.includes('/executions/'))return route.fulfill({json:{...plan.Executions[0],Title:plan.Title,PlanVersion:1,Instructions:'Recorded execution',Result:'Recorded result'}});
    if(path==='/api/plans/events')return route.fulfill({json:{Items:[],Plans:{},NextCursor:''}});
    if(path==='/api/plans/watches')return route.fulfill({json:{Items:[],NextCursor:''}});
    return route.fulfill({json:path==='/api/plans'?[]:plan});
  });
  async function open(query=`at=${encodeURIComponent(at)}`){
    await page.goto(`/#/plans/${plan.Ref.ID}${query?'?'+query:''}`);
    await expect(page.locator(query?'.plans-execution-panel':'.plans-history')).toBeVisible();
    await expect.poll(()=>pending.size).toBe(0);
  }
  return {now,at,plan,open,requests:()=>requests,pending:()=>pending.size};
}
const state=(page:Page)=>page.locator('.plans-execution-properties > div').first();
const timers=(page:Page)=>page.evaluate(()=>(window as any).occurrenceTimers.size);

for(const kind of ['personal_schedule','assistant_schedule'])test(`${kind} changes only at its occurrence deadline with no polling`,async({page})=>{
  const fixture=await setup(page);fixture.plan.Kind=fixture.plan.Schedule.Kind=kind;
  await fixture.open();await page.clock.pauseAt(new Date(fixture.now));
  await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));
  await expect.poll(fixture.pending).toBe(0);
  await expect(state(page)).toContainText('Not started');expect(await timers(page)).toBe(1);
  const before=fixture.requests();
  await page.clock.runFor(9999);await expect(state(page)).toContainText('Not started');
  await page.clock.runFor(1);await expect(state(page)).toContainText('Not executed');
  expect(await timers(page)).toBe(0);expect(fixture.requests()).toBe(before);
  // Keep this local idle budget separate from the app's session refresh cadence.
  await page.clock.runFor(10_000);expect(fixture.requests()).toBe(before);expect(await timers(page)).toBe(0);
});

test('occurrence deadline cancels on hide and unmount and rechecks on return',async({page})=>{
  const fixture=await setup(page);await fixture.open();await page.clock.pauseAt(new Date(fixture.now));
  await page.evaluate(()=>{Object.defineProperty(document,'visibilityState',{configurable:true,value:'hidden'});document.dispatchEvent(new Event('visibilitychange'));});
  expect(await timers(page)).toBe(0);
  await page.clock.runFor(15_000);
  await page.evaluate(()=>{Object.defineProperty(document,'visibilityState',{configurable:true,value:'visible'});document.dispatchEvent(new Event('visibilitychange'));});
  await expect(state(page)).toContainText('Not executed');expect(await timers(page)).toBe(0);
  const next=new Date(fixture.now+60_000).toISOString();
  await page.evaluate(at=>location.hash='/plans/occurrence-time?at='+encodeURIComponent(at),next);
  await expect(state(page)).toContainText('Not started');expect(await timers(page)).toBe(1);
  await page.evaluate(()=>location.hash='/plans');await expect(page.locator('.plans-execution-panel')).toHaveCount(0);
  expect(await timers(page)).toBe(0);
});

test('to-do deadlines, watches, whole plans and recorded executions keep their own states',async({page})=>{
  const fixture=await setup(page);await fixture.open();await page.clock.pauseAt(new Date(fixture.now+20_000));
  await fixture.open('');await expect(page.locator('.plans-execution-panel')).toHaveCount(0);await expect(page.locator('.plans-history')).toContainText('No execution records');expect(await timers(page)).toBe(0);
  const schedule=fixture.plan.Schedule;
  fixture.plan.Schedule=null;fixture.plan.Kind=fixture.plan.Ref.Kind='todo';fixture.plan.Todo={ID:fixture.plan.Ref.ID,Title:fixture.plan.Title,DueAt:fixture.at,Completed:false};
  await fixture.open();await expect(state(page)).toContainText('Not started');expect(await timers(page)).toBe(0);
  fixture.plan.Todo=null;fixture.plan.Kind=fixture.plan.Ref.Kind='watch';fixture.plan.Watch={ID:fixture.plan.Ref.ID,RunState:'paused',EndAt:fixture.at,Watcher:{health_seconds:60,compute_id:'fixture',script:'fixture'}};
  await fixture.open('');await expect(page.locator('.plans-execution-panel')).toHaveCount(0);await expect(page.locator('.plans-history')).not.toContainText('Not executed');expect(await timers(page)).toBe(0);
  fixture.plan.Watch=null;fixture.plan.Schedule=schedule;fixture.plan.Kind='assistant_schedule';fixture.plan.Ref.Kind='schedule';
  for(const [status,label] of [['pending','Pending'],['running','Running'],['failed','Failed'],['completed','Completed']]){
    fixture.plan.Executions=[{ID:'recorded-'+status,State:status,OccurrenceAt:fixture.at,StartedAt:fixture.at}];fixture.plan.ExecutionCount=1;
    await fixture.open('execution=recorded-'+status);await expect(state(page)).toContainText(label);expect(await timers(page)).toBe(0);
  }
});
