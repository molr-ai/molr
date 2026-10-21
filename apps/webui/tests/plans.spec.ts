import { selectPlanRange } from './plan-controls';
import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
const modelOrigin=`http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT??18882}`;
const base=`http://127.0.0.1:${process.env.MOLR_TEST_PORT??process.env.MOLR_E2E_PORT??5193}`;
async function login(page:Page, username='owner', password='webui-e2e-password-2026!'){await page.goto('/');await page.getByRole('textbox',{name:'Username',exact:true}).fill(username);await page.getByLabel('Password',{exact:true}).fill(password);await page.getByRole('button',{name:'Sign in',exact:true}).click();await page.getByRole('navigation',{name:'Molr'}).waitFor();}
async function post(page:Page,path:string,data:object){const token=(await page.context().cookies()).find(c=>c.name===`molr_dev_csrf_p${new URL(base).port}`)!.value;const response=await page.request.post(path,{headers:{Origin:base,'X-CSRF-Token':token},data:{Mutation:{IdempotencyKey:crypto.randomUUID(),ExpectedVersion:0},...data}});expect(response.status(),await response.text()).toBe(200);return response.json();}
// Exact day counts require a private calendar, independent of suite order and date.
async function isolatedCalendarAccount(page: Page) {
 const suffix=crypto.randomUUID(),username=`calendar-${suffix}`,password=`calendar-${suffix}-password!`;
 const group=await post(page,'/api/admin/groups',{Group:{Name:`Calendar ${suffix}`,Administrator:false,Resources:[]}});
 await post(page,'/api/admin/users',{Username:username,DisplayName:'Calendar review',Password:password,GroupID:group.ID});
 await post(page,'/api/auth/logout',{});
 await login(page,username,password);
 expect(await(await page.request.get('/api/plans')).json()).toEqual([]);
}
async function assistant(page:Page,name:string){const resource=await post(page,'/api/resources',{Name:name,Kind:'model',Provider:'openai-compatible',Binding:{Endpoint:modelOrigin,AllowedOrigins:[modelOrigin],Models:[{Name:name,ContextLimit:32768,SupportsTools:true,Pricing:{Currency:'USD',InputMicrosPerMillion:0,OutputMicrosPerMillion:0,CachedInputMicrosPerMillion:0}}]}});const models=await(await page.request.get('/api/models')).json();return post(page,'/api/assistants',{Name:name,ModelID:models.find((m:any)=>m.ResourceID===resource.ID).ID,Budget:{Limit:{Currency:'USD',Micros:0},Period:'daily'}});}
async function closePlanDrawer(page: Page) { const drawer = page.locator('.plans-details-drawer'); if (await drawer.isVisible()) { await drawer.getByRole('button', { name: 'Close details', exact: true }).click(); await expect(drawer).toBeHidden(); } }
async function planAction(page:Page,title:string,action:string){await closePlanDrawer(page);await row(page,title).first().getByRole('button',{name:'Plan actions',exact:true}).click();await page.getByRole('menuitem',{name:action,exact:true}).click();}
const details=(page:Page,title:string)=>page.getByRole('region',{name:`${title} · Plan details`,exact:true});
const row=(page:Page,title:string)=>page.locator('.plan-row').filter({has:page.getByRole('heading',{name:title,exact:true})});

test('personal preparation hands off exactly once; plan and immutable execution are shared with the assistant panel',async({page},info)=>{
 await login(page);const suffix=`${info.project.name}-${Date.now()}`,a=await assistant(page,`Analyst ${suffix}`),prepTitle=`Prepare ${suffix}`,analysisTitle=`Analyze ${suffix}`;
 const prep=await post(page,'/api/todos',{Title:prepTitle});const analysis=await post(page,'/api/todos',{Title:analysisTitle,AssistantID:a.ID,Notes:'Analyze the prepared figures'});
 await page.goto('/#/plans');await page.getByRole('button',{name:'List',exact:true}).click();await selectPlanRange(page, /No date/);await planAction(page,analysisTitle,'Handoff settings');
 const config=page.getByRole('dialog',{name:'Handoff settings',exact:true});await config.getByRole('combobox',{name:'Prerequisite plans',exact:true}).click();await page.getByRole('option',{name:prepTitle,exact:true}).click();await page.keyboard.press('Escape');await config.getByRole('combobox',{name:'Handoff settings',exact:true}).click();await page.getByRole('option',{name:'Start automatically',exact:true}).click();await config.getByRole('button',{name:'Save',exact:true}).click();await expect(config).not.toBeVisible();
 await expect(row(page,analysisTitle)).toContainText('Waiting for prerequisites');await row(page,analysisTitle).getByRole('button',{name:/Execution history/}).click();await expect(details(page,analysisTitle).getByRole('button',{name:'Start this execution'})).toBeDisabled();
 await closePlanDrawer(page);await row(page,prepTitle).getByRole('button',{name:/Execution history/}).click();await details(page,prepTitle).getByRole('button',{name:'Complete this execution'}).click();const completion=page.getByRole('dialog',{name:'Complete this execution'});await completion.getByLabel('Result or handoff notes').fill('Sales 120; cost 80.');await completion.getByRole('button',{name:'Complete this execution',exact:true}).click();await expect(completion).not.toBeVisible();
 await expect.poll(async()=>{const plans=await(await page.request.get('/api/plans')).json();return plans.find((p:any)=>p.Ref.ID===analysis.ID).Executions.length;}).toBe(1);
 await page.reload();await selectPlanRange(page, /No date/);await row(page,analysisTitle).getByRole('button',{name:/Execution history/}).click();await details(page,analysisTitle).locator('summary').click();await expect(details(page,analysisTitle)).toContainText('Sales 120; cost 80.');await page.screenshot({path:info.outputPath('plans-detail.png')});
 await planAction(page,analysisTitle,'Edit');const edit=page.getByRole('dialog',{name:'Edit',exact:true});await edit.getByLabel('What needs doing?').fill(`${analysisTitle} updated`);await edit.getByRole('button',{name:'Save',exact:true}).click();await expect(edit).not.toBeVisible();
 await page.goto(`/#/assistants/${a.ID}`);await page.locator('.chat-header').getByRole('button',{name:'Plans',exact:true}).click();const panel=page.locator('.assistant-plans');await expect(panel).toBeVisible();await expect(panel).toContainText(`${analysisTitle} updated`);await expect(panel.getByRole('heading',{name:prepTitle,exact:true})).toHaveCount(0);
 const item=panel.locator('.plan-row').filter({hasText:`${analysisTitle} updated`});await item.getByRole('button',{name:/Execution history/}).click();const detail=panel.locator('.plans-panel-detail');await detail.locator('summary').click();await expect(detail).toContainText(`Plan: ${analysisTitle}`);await expect(detail).toContainText('Sales 120; cost 80.');
 const plans=await(await page.request.get('/api/plans')).json();expect(plans.find((p:any)=>p.Ref.ID===analysis.ID).Executions).toHaveLength(1);expect(plans.find((p:any)=>p.Ref.ID===prep.ID).Executions[0].State).toBe('completed');expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});

test('ordinary tasks receive a one-time plan without changing normal chat behavior',async({page},info)=>{
 await login(page);const a=await assistant(page,`One time ${info.project.name}-${Date.now()}`),goal=`One-time report ${Date.now()}`;const task=await post(page,'/api/tasks',{AssistantID:a.ID,Goal:goal});expect(task.Plan.Kind).toBe('todo');
 await page.goto(`/#/assistants/${a.ID}`);await page.locator('.chat-header').getByRole('button',{name:'Plans',exact:true}).click();const panel=page.locator('.assistant-plans');await expect(panel).toContainText(goal);await panel.getByRole('button',{name:/Execution history/}).click();await expect(panel.locator('.plan-execution')).toHaveCount(1);await page.screenshot({path:info.outputPath('assistant-plans.png')});
 await page.goto(`/#/assistants/${a.ID}?panel=tasks&task=${task.ID}`);
 await expect(page.locator('.assistant-plans .plan-execution[open]')).toHaveCount(1);
 await expect(page.locator('.assistant-plans .plans-snapshot-source')).toContainText(goal);
});


const dateKey=(at:Date)=>`${at.getFullYear()}-${String(at.getMonth()+1).padStart(2,'0')}-${String(at.getDate()).padStart(2,'0')}`;
const calendar=(page:Page)=>page.getByRole('group',{name:'Plan view'}).getByRole('button',{name:'Calendar',exact:true});
async function pickMonth(page:Page,value:string){
 if(await page.locator('.plans-month-calendar').getAttribute('data-month')===value)return;
 await page.locator('.plans-calendar-period-trigger').click();
 const picker=page.getByRole('dialog',{name:'Month and year',exact:true});
 await picker.getByRole('combobox',{name:'Year',exact:true}).click();await page.getByRole('option',{name:value.slice(0,4),exact:true}).click();
 await picker.getByRole('combobox',{name:'Month',exact:true}).click();await page.getByRole('option',{name:new Date(`${value}-01T12:00:00`).toLocaleString('en',{month:'short'}),exact:true}).click();
 await picker.getByRole('button',{name:'Apply',exact:true}).click();await expect(picker).not.toBeVisible();
}
async function pickerTime(page:Page,label:string,hour:number,minute:number){
 await page.getByRole('group',{name:label,exact:true}).getByRole('button',{name:'Custom',exact:true}).click();
 const picker=page.getByRole('dialog',{name:'Date and time',exact:true});
 for(const [name,value] of [['Hour',hour],['Minute',minute]] as const){await picker.getByRole('listbox',{name,exact:true}).focus();await page.keyboard.press('Home');for(let i=0;i<value;i++)await page.keyboard.press('ArrowDown');}
 await picker.getByRole('button',{name:'Apply',exact:true}).click();await expect(picker).not.toBeVisible();
}


test('month fills the workspace; cell capacity resizes and overflow opens every event',async({page},info)=>{
 await login(page);const at=new Date();at.setDate(info.project.name==='desktop'?8:9);at.setHours(9,0,0,0);const day=dateKey(at),sparse=new Date(at);sparse.setDate(12+(info.project.name==='desktop'?0:1));
 const prefix=`Calendar ${info.project.name}-${Date.now()}`;
 // Retain an overlapping event in the shared owner account to prove fixture isolation.
 await post(page,'/api/todos',{Title:`Other account ${prefix}`,DueAt:sparse.toISOString()});
 await isolatedCalendarAccount(page);
 for(let i=0;i<9;i++){const time=new Date(at.getTime()+i*60000);await post(page,'/api/todos',{Title:`${prefix} ${i}`,DueAt:time.toISOString()});}
 for(let i=0;i<3;i++)await post(page,'/api/todos',{Title:`Three ${prefix} ${i}`,DueAt:new Date(sparse.getTime()+i*60000).toISOString()});
 await page.goto('/#/plans');await page.getByRole('button',{name:'List',exact:true}).click();await page.getByRole('combobox',{name:'Owner',exact:true}).click();await page.getByRole('option',{name:'Me',exact:true}).click();await calendar(page).click();await page.getByRole('button',{name:'Month',exact:true}).click();await expect(page.locator('.plans-month-calendar')).toHaveCount(1);await expect(page.locator('.plans-month-calendar')).toHaveAttribute('data-month',dateKey(new Date()).slice(0,7));await expect(page.locator('.plans-calendar-return button')).toHaveCount(0);
 const cell=page.locator(`.plans-month-day[data-day="${day}"]`),few=page.locator(`.plans-month-day[data-day="${dateKey(sparse)}"]`);
 await expect(cell).toHaveAttribute('data-event-count','9');await expect(few.locator('.plans-month-event')).toHaveCount(3);await expect(few.locator('.plans-month-more')).toHaveCount(0);
 for(let i=0;i<3;i++)await expect(few.locator('.plans-month-event > span').nth(i)).toBeVisible();
 const capacity=Number(await cell.getAttribute('data-capacity'));expect(capacity).toBeGreaterThanOrEqual(3);expect(capacity).toBeLessThan(9);
 await expect(cell.locator('.plans-month-event')).toHaveCount(capacity-1);await expect(cell.locator('.plans-month-more')).toHaveText(`+${9-capacity+1}`);
 expect(await cell.evaluate(el=>Array.from(el.querySelectorAll('.plans-month-event,.plans-month-more')).every(row=>row.getBoundingClientRect().bottom<=el.getBoundingClientRect().bottom))).toBe(true);
 await cell.locator('.plans-month-more').click();const agenda=page.getByRole('dialog',{name:`${day} · Day agenda`,exact:true});await expect(agenda.locator('.plans-row')).toHaveCount(9);await agenda.getByRole('button',{name:`${prefix} 8`,exact:true}).click();const detail=page.getByRole('dialog',{name:'Plan details',exact:true});await expect(detail).toContainText(`${prefix} 8`);await page.keyboard.press('Escape');
 await page.setViewportSize({width:info.project.name==='desktop'?1440:393,height:1800});await expect.poll(async()=>Number(await cell.getAttribute('data-capacity'))).toBeGreaterThan(capacity);
 const taller=Number(await cell.getAttribute('data-capacity'));await expect(cell.locator('.plans-month-event')).toHaveCount(taller>=9?9:taller-1);if(taller<9)await expect(cell.locator('.plans-month-more')).toHaveText(`+${10-taller}`);else await expect(cell.locator('.plans-month-more')).toHaveCount(0);
 await page.setViewportSize(info.project.name==='desktop'?{width:1440,height:1000}:{width:393,height:851});await expect.poll(async()=>Number(await cell.getAttribute('data-capacity'))).toBe(capacity);
 await page.getByRole('button',{name:'Next month',exact:true}).click();const next=new Date(at.getFullYear(),at.getMonth()+1,1);await expect(page.locator('.plans-month-calendar')).toHaveAttribute('data-month',dateKey(next).slice(0,7));await page.getByRole('button',{name:'Back to this month',exact:true}).click();await expect(page.locator('.plans-month-calendar')).toHaveAttribute('data-month',dateKey(new Date()).slice(0,7));await expect(page.locator('.plans-calendar-return button')).toHaveCount(0);
 if(info.project.name==='mobile') {
  await expect(page.locator('.plans-statusbar')).toBeHidden();
  for(const height of [851,420]) {
   await page.setViewportSize({width:393,height});
   await page.locator('.plans-master').evaluate(el=>{el.scrollTop=el.scrollHeight;});
   await expect.poll(()=>page.evaluate(()=>{
    const grid=document.querySelector('.plans-month-grid')!.getBoundingClientRect(), menu=document.querySelector('.workspace-nav')!.getBoundingClientRect();
    return grid.bottom<menu.top;
   })).toBe(true);
  }
 }
 await expect(page.locator('.plans-month-calendar')).not.toContainText('Reserved for dreaming');expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);await page.screenshot({path:info.outputPath('month-calendar.png')});
});

test('upcoming list paginates occurrences in time order and calendar preserves repeating events',async({page},info)=>{
 await login(page);const a=await assistant(page,`Upcoming ${info.project.name}-${Date.now()}`),at=new Date();at.setDate(at.getDate()+1);at.setHours(3,5,0,0);const title=`Daily ${Date.now()}`;
 const preferences=await(await page.request.get('/api/settings')).json();await post(page,'/api/settings',{Settings:{...preferences,Timezone:'Asia/Shanghai'},Mutation:{IdempotencyKey:crypto.randomUUID(),ExpectedVersion:preferences.Version}});
 const config=await(await page.request.get(`/api/dreams/config?assistant_id=${a.ID}`)).json();await post(page,'/api/dreams/config',{AssistantID:a.ID,Enabled:true,Hour:3,Minute:0,MaxDurationSeconds:600,Mutation:{IdempotencyKey:crypto.randomUUID(),ExpectedVersion:config.Version}});
 await post(page,'/api/schedules',{Title:title,Kind:'assistant_plan',AssistantID:a.ID,Instructions:'Review notes',Trigger:{StartAt:at.toISOString(),Timezone:'Asia/Shanghai',Recurrence:'FREQ=DAILY;COUNT=120',MissedPolicy:'coalesce'}});
 await page.goto('/#/plans');await page.getByRole('button',{name:'List',exact:true}).click();await page.getByRole('combobox',{name:'Owner',exact:true}).click();await page.getByRole('option',{name:a.Name,exact:true}).click();await expect(row(page,title)).toHaveCount(50);
 const times=await row(page,title).evaluateAll(rows=>rows.map(row=>row.getAttribute('data-occurrence')!));expect(new Set(times).size).toBe(50);expect(times.map(Date.parse)).toEqual([...times.map(Date.parse)].sort((a,b)=>a-b));
 await page.getByRole('button',{name:'Load 50 more events',exact:true}).click();await expect(row(page,title)).toHaveCount(100);expect(new Set(await row(page,title).evaluateAll(rows=>rows.map(row=>row.getAttribute('data-occurrence')))).size).toBe(100);
 await expect(page.locator('.plans-statusbar, .plans-reservations')).toHaveCount(0);
 await planAction(page,title,'Edit');await expect(page.getByRole('dialog').locator('.plans-dream-conflict')).toContainText('Conflicts within 90 days');await page.keyboard.press('Escape');
 await calendar(page).click();await page.getByRole('button',{name:'Month',exact:true}).click();for(let offset=0;offset<3;offset++){const occurrence=new Date(at);occurrence.setDate(occurrence.getDate()+offset);await pickMonth(page,dateKey(occurrence).slice(0,7));await expect(page.locator(`.plans-month-day[data-day="${dateKey(occurrence)}"] .plans-month-event > span`).filter({hasText:title})).toBeVisible();}
 await expect(page.locator('.plans-month-calendar')).not.toContainText('Reserved for dreaming');await page.screenshot({path:info.outputPath('recurring-calendar.png')});
});

test('completing a recurring event targets the selected occurrence only',async({page},info)=>{
 await login(page);const at=new Date();at.setDate(at.getDate()+1);at.setHours(10,0,0,0);const title=`Personal series ${info.project.name}-${Date.now()}`;
 const plan=await post(page,'/api/schedules',{Title:title,Kind:'personal',Trigger:{StartAt:at.toISOString(),Timezone:'Asia/Shanghai',Recurrence:'FREQ=DAILY;COUNT=3',MissedPolicy:'coalesce'}});
 await page.goto('/#/plans');await page.getByRole('button',{name:'List',exact:true}).click();await expect(row(page,title)).toHaveCount(3);const second=row(page,title).nth(1),selected=await second.getAttribute('data-occurrence');await second.getByRole('button',{name:/Execution history/}).click();await details(page,title).getByRole('button',{name:'Complete this execution',exact:true}).click();
 const response=page.waitForResponse(r=>r.url().endsWith('/api/plans/execute')&&r.request().method()==='POST');await page.getByRole('dialog',{name:'Complete this execution',exact:true}).getByRole('button',{name:'Complete this execution',exact:true}).click();const result=await response;expect(result.ok(),await result.text()).toBe(true);expect(result.request().postDataJSON().OccurrenceAt).toBe(selected);
 const plans=await(await page.request.get('/api/plans')).json(),saved=plans.find((p:any)=>p.Ref.ID===plan.ID);expect(saved.Executions).toHaveLength(1);expect(Date.parse(saved.Executions[0].OccurrenceAt)).toBe(Date.parse(selected!));
 await closePlanDrawer(page);await expect(row(page,title).nth(1)).toHaveAttribute('data-completed','true');await expect(row(page,title).nth(0)).toHaveAttribute('data-completed','false');
});

test('old recurring plans warn in edit and exact event details using the shared alert',async({page},info)=>{
 test.setTimeout(75000);
 await login(page);const a=await assistant(page,`Alert ${info.project.name}-${Date.now()}`),old=new Date();old.setDate(old.getDate()-150);old.setHours(3,5,0,0);
 const preferences=await(await page.request.get('/api/settings')).json();await post(page,'/api/settings',{Settings:{...preferences,Timezone:'Asia/Shanghai'},Mutation:{IdempotencyKey:crypto.randomUUID(),ExpectedVersion:preferences.Version}});
 const cfg=await(await page.request.get(`/api/dreams/config?assistant_id=${a.ID}`)).json();await post(page,'/api/dreams/config',{AssistantID:a.ID,Enabled:true,Hour:3,Minute:0,MaxDurationSeconds:600,Mutation:{IdempotencyKey:crypto.randomUUID(),ExpectedVersion:cfg.Version}});
 const title=`Old recurring ${Date.now()}`;await post(page,'/api/schedules',{Title:title,Kind:'assistant_plan',AssistantID:a.ID,Instructions:'Review notes',Trigger:{StartAt:old.toISOString(),Timezone:'Asia/Shanghai',Recurrence:'FREQ=DAILY',MissedPolicy:'coalesce'}});
 await page.goto('/#/plans');await page.getByRole('button',{name:'List',exact:true}).click();await page.getByRole('combobox',{name:'Owner',exact:true}).click();await page.getByRole('option',{name:a.Name,exact:true}).click();await expect(row(page,title)).toHaveCount(50);
 const first=row(page,title).first();await expect(first.locator('.plans-row-summary .plans-type-dot')).toHaveAttribute('data-kind','automatic');await expect(first.locator('.plans-row-summary .plans-type-dot')).not.toHaveAttribute('tabindex');await expect(first.getByRole('checkbox')).toHaveCount(0);
 await planAction(page,title,'Edit');const edit=page.getByRole('dialog',{name:'Edit',exact:true});await expect(edit.locator('.ui-alert--warning.plans-dream-conflict')).toBeVisible();await expect(edit.locator('input[type="date"],input[type="datetime-local"],input[type="time"]')).toHaveCount(0);
 const query=new URLSearchParams({assistant_id:a.ID,start_at:old.toISOString(),timezone:'Asia/Shanghai',recurrence:'FREQ=DAILY'}),windows=await(await page.request.get('/api/dreams/conflicts?'+query)).json();expect(windows.length).toBeGreaterThan(0);expect(windows.every((w:any)=>Date.parse(w.Start)>Date.now()-600000)).toBe(true);
 await edit.locator('.plans-dream-conflict').getByRole('button',{name:'Close',exact:true}).click();await expect(edit.locator('.plans-dream-conflict')).toHaveCount(0);
 await pickerTime(page,'Start time',5,5);await expect(edit.locator('.plans-dream-conflict')).toHaveCount(0);await pickerTime(page,'Start time',3,5);await expect(edit.locator('.ui-alert--warning.plans-dream-conflict')).toBeVisible();await edit.getByRole('button',{name:'Cancel',exact:true}).click();await expect(edit).not.toBeVisible();
 await first.getByRole('button',{name:title,exact:true}).click();await expect(details(page,title).locator('.ui-alert--warning.plans-dream-conflict').first()).toBeVisible();
 await closePlanDrawer(page);
 await expect(page.locator('.plans-statusbar')).toHaveCount(0);
 await calendar(page).click();await page.getByRole('button',{name:'Month',exact:true}).click();await expect(page.locator('input[type="month"]')).toHaveCount(0);await pickMonth(page,'2027-02');await expect(page.locator('.plans-month-calendar')).toHaveAttribute('data-month','2027-02');await expect(page.locator('.plans-statusbar')).toHaveCount(0);
});

test('shared date picker preserves the plan timezone and type dots have distinct colors',async({page},info)=>{
 await login(page);const at=new Date();at.setDate(at.getDate()+2);const iso=at.toISOString().slice(0,10)+'T09:15:00Z',title=`UTC meeting ${info.project.name}-${Date.now()}`;
 const schedule=await post(page,'/api/schedules',{Title:title,Kind:'personal',Trigger:{StartAt:iso,Timezone:'UTC',Recurrence:'',MissedPolicy:'coalesce'}});const itemTitle=`Item ${Date.now()}`;await post(page,'/api/todos',{Title:itemTitle,DueAt:iso});
 await page.goto('/#/plans');await page.getByRole('button',{name:'List',exact:true}).click();await planAction(page,title,'Edit');const edit=page.getByRole('dialog',{name:'Edit',exact:true});await edit.getByRole('group',{name:'Start time',exact:true}).getByRole('button',{name:'Custom',exact:true}).click();const picker=page.getByRole('dialog',{name:'Date and time',exact:true});await expect(picker.getByRole('listbox',{name:'Hour',exact:true}).getByRole('option',{name:'09',exact:true})).toHaveAttribute('aria-selected','true');await picker.getByRole('button',{name:'Cancel',exact:true}).click();
 await pickerTime(page,'Start time',10,20);const response=page.waitForResponse(r=>r.url().endsWith('/api/schedules/update')&&r.request().method()==='POST');await edit.getByRole('button',{name:'Save',exact:true}).click();const saved=await response;expect(saved.ok(),await saved.text()).toBe(true);expect(saved.request().postDataJSON().Trigger.StartAt).toBe(iso.slice(0,10)+'T10:20:00.000Z');expect(saved.request().postDataJSON().ID).toBe(schedule.ID);
 const color=(title:string)=>row(page,title).first().locator('.plans-type-dot').evaluate(el=>getComputedStyle(el).backgroundColor);expect(await color(title)).not.toBe(await color(itemTitle));await expect(row(page,itemTitle).locator('.plans-type-dot')).toHaveAttribute('aria-label','Item');await expect(row(page,title).locator('.plans-type-dot')).toHaveAttribute('aria-label','Time block');
});


test('plan details open in a drawer without resizing the list and restore focus on close', async ({ page }, info) => {
 await login(page);
 const title = `Drawer plan ${info.project.name}-${Date.now()}`;
 await post(page, '/api/todos', { Title: title, DueAt: new Date(Date.now() + 3600_000).toISOString() });
 await page.goto('/#/plans');await page.getByRole('button',{name:'List',exact:true}).click();
 const trigger = row(page, title).getByRole('button', { name: title, exact: true });
 await expect(trigger).toBeVisible();
 const list = page.locator('.plans-master'), width = await list.evaluate(el => el.clientWidth);
 await trigger.focus(); await page.keyboard.press('Enter');
 const drawer = page.getByRole('dialog', { name: 'Plan details', exact: true });
 await expect(drawer).toHaveClass(/ui-drawer/);
 await expect(drawer.getByRole('heading', { name: title, exact: true })).toBeVisible();
 await expect(page.locator('.plans-inspector')).toHaveCount(0);
 expect(await list.evaluate(el => el.clientWidth)).toBe(width);
 await page.keyboard.press('Escape'); await expect(drawer).toBeHidden(); await expect(trigger).toBeFocused();
 await row(page, title).click({ position: { x: 5, y: 5 } }); await expect(drawer).toBeVisible();
 await closePlanDrawer(page); await expect(trigger).toBeFocused();
});

test('plan lists omit execution bodies and load history pages and snapshots only on demand',async({page},info)=>{
 await login(page);await isolatedCalendarAccount(page);
 const at=new Date();at.setDate(at.getDate()+1);at.setHours(9,0,0,0);
 const title=`Lazy history ${info.project.name}-${Date.now()}`,notes='Editable source text',result='Execution body only '.repeat(100);
 const schedule=await post(page,'/api/schedules',{Title:title,Kind:'personal',Instructions:notes,Trigger:{StartAt:at.toISOString(),Timezone:'Asia/Shanghai',Recurrence:'FREQ=DAILY;COUNT=30',MissedPolicy:'skip'}});
 for(let i=0;i<25;i++){
  const occurrence=new Date(at);occurrence.setDate(occurrence.getDate()+i);
  await post(page,'/api/plans/execute',{Ref:{Kind:'schedule',ID:schedule.ID},Action:'complete',OccurrenceAt:occurrence.toISOString(),Result:result,Mutation:{IdempotencyKey:crypto.randomUUID(),ExpectedVersion:i}});
 }
 const url=`/api/plans/events?from=${encodeURIComponent(at.toISOString())}&limit=50`;
 const response=await page.request.get(url),raw=await response.text(),data=JSON.parse(raw);
 expect(Object.keys(data.Plans)).toHaveLength(1);expect(data.Items).toHaveLength(30);
 expect(raw.length).toBeLessThan(20000);expect(raw).not.toContain(result);expect(raw).not.toContain(notes);
 expect(Object.values(data.Plans).map((p:any)=>p.ExecutionCount)).toEqual([25]);
 const bodies:string[]=[],history:string[]=[],catalog:string[]=[];
 page.on('request',request=>{const u=new URL(request.url());if(u.pathname.includes('/executions/'))bodies.push(u.pathname);if(u.pathname.endsWith('/history'))history.push(u.pathname);if(u.pathname==='/api/plans')catalog.push(u.href);});
 await page.goto('/#/plans');await page.getByRole('button',{name:'List',exact:true}).click();await expect(row(page,title)).toHaveCount(30);
 expect(bodies).toHaveLength(0);expect(history).toHaveLength(0);
 const catalogCount=catalog.length;
 await page.evaluate(()=>document.dispatchEvent(new Event('visibilitychange')));
 await expect(row(page,title)).toHaveCount(30);expect(catalog).toHaveLength(catalogCount);
 await row(page,title).first().getByRole('button',{name:/Execution history/}).click();
 const detail=details(page,title);await expect(detail.locator('.plan-execution')).toHaveCount(20);expect(bodies).toHaveLength(0);
 await detail.getByRole('button',{name:'Load more history'}).click();await expect(detail.locator('.plan-execution')).toHaveCount(25);
 await detail.locator('.plan-execution summary').first().click();await expect(detail.locator('.plans-result')).toContainText(result.trim());expect(bodies).toHaveLength(1);
 await planAction(page,title,'Edit');await expect(page.getByRole('dialog',{name:'Edit',exact:true}).getByLabel('Notes',{exact:true})).toHaveValue(notes);
});
