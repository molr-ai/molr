import { test, expect, type Page } from '@playwright/test';
import type { SystemNotice } from '@molr/sdk';
const port=process.env.MOLR_TEST_PORT??'5193',base=`http://127.0.0.1:${port}`;
async function login(page:Page){await page.goto(base);await page.getByLabel('Username',{exact:true}).fill('owner');await page.getByLabel('Password',{exact:true}).fill('webui-e2e-password-2026!');await page.getByRole('button',{name:'Sign in',exact:true}).click();await expect(page.getByRole('navigation',{name:'Molr'})).toBeVisible();}
async function post(page:Page,path:string,data:object,mutation=true){const token=(await page.context().cookies()).find(c=>c.name===`molr_dev_csrf_p${port}`)!.value;const response=await page.request.post(base+path,{headers:{Origin:base,'X-CSRF-Token':token},data:mutation?{Mutation:{IdempotencyKey:crypto.randomUUID()},...data}:data});expect(response.ok(),await response.text()).toBe(true);return response.json();}
async function notices(page:Page,id:string){return (await(await page.request.get(`${base}/api/notifications?assistant_id=${id}&filter=active`)).json()).Items as SystemNotice[];}
async function blockedAssistant(page:Page,name:string){const origin=`http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT??18882}`;const r=await post(page,'/api/resources',{Name:name,Kind:'model',Provider:'openai-compatible',Binding:{Endpoint:origin,AllowedOrigins:[origin],Models:[{Name:name,ContextLimit:32768,SupportsTools:true,Pricing:{Currency:'USD',InputMicrosPerMillion:1000000}}]}});const models=await(await page.request.get(base+'/api/models')).json();return post(page,'/api/assistants',{Name:name,ModelID:models.find((m:any)=>m.ResourceID===r.ID).ID,Budget:{Limit:{Currency:'USD',Micros:0},Period:'daily'}});}

test('server notice delivery uses channel preferences changed during async work',async({page},info)=>{
 test.setTimeout(120_000);
 await page.addInitScript(()=>{
  document.hasFocus=()=>false;
  Object.defineProperty(Notification,'permission',{get:()=> 'granted'});
  const counts={desktop:0,sound:0};Object.defineProperty(window,'noticeChannelCounts',{value:counts});
  // Exercise provider policy with a controlled OS boundary and real Web Audio.
  const controls=window as unknown as {pauseNoticeDesktop:boolean;noticeDesktopWaiting:boolean;rejectNoticeDesktop:boolean;releaseNoticeDesktop?:()=>void};
  Object.defineProperty(navigator.serviceWorker,'register',{value:async()=>({active:{},showNotification:async()=>{
   counts.desktop++;
   if(controls.pauseNoticeDesktop){await new Promise<void>(resolve=>{controls.releaseNoticeDesktop=resolve;controls.noticeDesktopWaiting=true;});if(controls.rejectNoticeDesktop)throw new Error('Controlled platform failure');}
  },getNotifications:async()=>[]})});
  const oscillator=AudioContext.prototype.createOscillator;
  AudioContext.prototype.createOscillator=function(){counts.sound++;return oscillator.call(this);};
 });
 await login(page);await page.goto('/#/account/notifications');
 await page.getByRole('switch',{name:'Notification alerts',exact:true}).check();
 await page.getByRole('switch',{name:'Message sound',exact:true}).check();
 await page.getByRole('button',{name:'Preview sound',exact:true}).click();
 await expect(page.getByText('Preview sound played.',{exact:true})).toBeVisible();
 const counts=()=>page.evaluate(()=>(window as unknown as {noticeChannelCounts:{desktop:number;sound:number}}).noticeChannelCounts);
 const toast=page.locator('.ui-toast').filter({hasText:'A notification needs your attention'});
 const scenarios=[
  {stage:'lookup',disable:['Desktop notifications','Message sound'],desktop:0,sound:0},
  {stage:'lookup',disable:['Desktop notifications'],desktop:0,sound:2},
  {stage:'lookup',disable:['Message sound'],desktop:1,sound:0},
  {stage:'lookup',disable:[],desktop:1,sound:2},
  {stage:'sound',disable:['Desktop notifications'],desktop:0,sound:2},
  {stage:'sound',disable:['Message sound'],desktop:1,sound:0},
  {stage:'desktop',disable:['Desktop notifications'],desktop:1,sound:2,toast:1},
  {stage:'desktop-reject',disable:['Notification alerts'],desktop:1,sound:2,toast:0},
 ];
 for(const [index,scenario] of scenarios.entries()){
  await test.step(`${scenario.stage}: disable ${scenario.disable.join(', ')||'neither channel'}`,async()=>{
   // Respect the account-wide sound cooldown between independent notices.
   if(index)await page.waitForTimeout(5100);
   for(const button of await toast.getByRole('button',{name:'Close',exact:true}).all())await button.click();
   await page.getByRole('switch',{name:'Notification alerts',exact:true}).check();
   await page.getByRole('switch',{name:'Desktop notifications',exact:true}).check();
   await page.getByRole('switch',{name:'Message sound',exact:true}).check();
   await page.evaluate(()=>{const counts=(window as unknown as {noticeChannelCounts:{desktop:number;sound:number}}).noticeChannelCounts;counts.desktop=0;counts.sound=0;});
   let release!:()=>void, paused!:()=>void;
   const gate=new Promise<void>(resolve=>{release=resolve;});
   const entered=new Promise<void>(resolve=>{paused=resolve;});
   const reads=new Map<string,number>();
   if(scenario.stage.startsWith('desktop'))await page.evaluate(reject=>{
    const controls=window as unknown as {pauseNoticeDesktop:boolean;noticeDesktopWaiting:boolean;rejectNoticeDesktop:boolean};
    controls.pauseNoticeDesktop=true;controls.noticeDesktopWaiting=false;controls.rejectNoticeDesktop=reject;
   },scenario.stage==='desktop-reject');
   if(scenario.stage==='sound')await page.evaluate(async()=>{
    const path='/src/notifications/coordinator.ts';const {NotificationClaims}=await import(path);
    const original=NotificationClaims.prototype.claimSound;
    const controls=window as unknown as {noticeSoundWaiting:boolean;releaseNoticeSound:()=>void};controls.noticeSoundWaiting=false;
    NotificationClaims.prototype.claimSound=async function(account:string){
     const claimed=await original.call(this,account);
     await new Promise<void>(resolve=>{controls.releaseNoticeSound=resolve;controls.noticeSoundWaiting=true;});
     NotificationClaims.prototype.claimSound=original;
     return claimed;
    };
   });
   const routePattern='**/api/notifications/*';
   await page.route(routePattern,async route=>{
    if(route.request().method()!=='GET'){await route.continue();return;}
    const id=new URL(route.request().url()).pathname;
    const count=(reads.get(id)??0)+1;reads.set(id,count);
    const response=await route.fetch();
    if(count===2&&scenario.stage==='lookup'){paused();await gate;}
    await route.fulfill({response});
   });
   try{
    // Completing a prerequisite creates a real plan-ready notice without a model call.
    const suffix=`${info.project.name}-${index}-${Date.now()}`;
    const prep=await post(page,'/api/todos',{Title:`Prepare channels ${suffix}`}),next=await post(page,'/api/todos',{Title:`Review channels ${suffix}`});
    await post(page,'/api/plans/configure',{Ref:{Kind:'todo',ID:next.ID},Dependencies:[{Kind:'todo',ID:prep.ID}],Handoff:'notify',Mutation:{IdempotencyKey:crypto.randomUUID(),ExpectedVersion:0}});
    await post(page,'/api/plans/execute',{Ref:{Kind:'todo',ID:prep.ID},Action:'complete',Mutation:{IdempotencyKey:crypto.randomUUID(),ExpectedVersion:0}});
    if(scenario.stage==='lookup')await entered;
    else if(scenario.stage==='sound')await expect.poll(()=>page.evaluate(()=>(window as unknown as {noticeSoundWaiting:boolean}).noticeSoundWaiting)).toBe(true);
    else await expect.poll(()=>page.evaluate(()=>(window as unknown as {noticeDesktopWaiting:boolean}).noticeDesktopWaiting)).toBe(true);
    for(const name of scenario.disable)await page.getByRole('switch',{name,exact:true}).uncheck();
    release();
    if(scenario.stage==='sound')await page.evaluate(()=>(window as unknown as {releaseNoticeSound:()=>void}).releaseNoticeSound());
    if(scenario.stage.startsWith('desktop')){
     await page.evaluate(()=>(window as unknown as {releaseNoticeDesktop:()=>void}).releaseNoticeDesktop());
     // Let platform rejection and React's possible fallback settle before checking absence.
     if(scenario.stage==='desktop-reject')await page.waitForTimeout(200);
    }
    await expect.poll(async()=>{const c=await counts();return c.desktop+c.sound+await toast.count();}).toBeGreaterThan(0);
    expect(await counts()).toEqual({desktop:scenario.desktop,sound:scenario.sound});
    await expect(toast).toHaveCount(scenario.toast??(scenario.desktop?0:1));
   }finally{
    release();
    if(scenario.stage==='sound')await page.evaluate(()=>(window as unknown as {releaseNoticeSound?:()=>void}).releaseNoticeSound?.());
    if(scenario.stage.startsWith('desktop'))await page.evaluate(()=>{const controls=window as unknown as {releaseNoticeDesktop?:()=>void;pauseNoticeDesktop:boolean};controls.releaseNoticeDesktop?.();controls.pauseNoticeDesktop=false;});
    await page.unroute(routePattern);
   }
  });
 }
});

test('server notices synchronize read/dismiss/recovery across independent browsers and survive reload',async({page,browser},info)=>{
 await login(page);const a=await blockedAssistant(page,`Notice ${info.project.name}-${Date.now()}`);
 await expect.poll(async()=>(await notices(page,a.ID)).length).toBe(1);const n=(await notices(page,a.ID))[0];
 const other=await browser.newContext({baseURL:base,viewport:info.project.use.viewport});const peer=await other.newPage();
 try{
  await login(peer);await page.goto(`/#/assistants/${a.ID}`);await peer.goto(`/#/assistants/${a.ID}`);
  const banner=(p:Page)=>p.locator('.ui-alert').filter({hasText:'Assistant needs attention'});
  await expect(banner(page)).toBeVisible();await expect(banner(peer)).toBeVisible();
  await post(page,'/api/notifications/update',{IDs:[n.ID],Action:'read'},false);
  await expect(banner(peer)).toBeVisible();
  const stillActive=(await notices(page,a.ID))[0];expect(stillActive.ReadAt).toBeTruthy();expect(stillActive.DismissedAt).toBeNull();
  await banner(page).getByRole('button',{name:'Close',exact:true}).click();await expect(banner(peer)).toHaveCount(0);
  await peer.reload();await expect(peer.locator('.chat-input textarea')).toBeVisible();await expect(banner(peer)).toHaveCount(0);
  const stored=await(await page.request.get(`${base}/api/notifications/${n.ID}`)).json();expect(stored.DismissedAt).toBeTruthy();expect(stored.ResolvedAt).toBeNull();
  // A rejected update must leave the notice intact; CSRF is enforced by the common API boundary.
  expect((await page.request.post(base+'/api/notifications/update',{headers:{Origin:base},data:{IDs:[n.ID],Action:'dismiss'}})).status()).toBe(403);
  await post(page,`/api/assistants/${a.ID}/stop`,{},false);
  await expect.poll(async()=>(await(await page.request.get(`${base}/api/notifications/${n.ID}`)).json()).ResolvedAt).toBeTruthy();
  const next=await blockedAssistant(page,`New notice ${info.project.name}-${Date.now()}`);await expect.poll(async()=>(await notices(page,next.ID)).length).toBe(1);
  await peer.goto(`/#/assistants/${next.ID}`);await expect(banner(peer)).toBeVisible();
  await expect(peer.getByRole('button',{name:'Notification center',exact:true})).toHaveCount(0);
 }finally{await other.close();}
});

test('offline plan handoff remains accessible in its business page',async({page},info)=>{
 await login(page);const suffix=`${info.project.name}-${Date.now()}`;
 const prep=await post(page,'/api/todos',{Title:`Prepare ${suffix}`}),next=await post(page,'/api/todos',{Title:`Review ${suffix}`});
 await post(page,'/api/plans/configure',{Ref:{Kind:'todo',ID:next.ID},Dependencies:[{Kind:'todo',ID:prep.ID}],Handoff:'notify',Mutation:{IdempotencyKey:crypto.randomUUID(),ExpectedVersion:0}});
 await page.goto('about:blank');
 await post(page,'/api/plans/execute',{Ref:{Kind:'todo',ID:prep.ID},Action:'complete',Mutation:{IdempotencyKey:crypto.randomUUID(),ExpectedVersion:0}});
 await expect.poll(async()=>{const result=await(await page.request.get(base+'/api/notifications?kind=plan.ready')).json();return result.Items.some((n:SystemNotice)=>n.Source.ID===next.ID);}).toBe(true);
 await page.goto(`/#/plans/${next.ID}`);
 await expect(page.getByRole('heading',{name:'Plans',exact:true})).toBeVisible();
 await expect(page.getByText(next.Title,{exact:true}).first()).toBeVisible();
});


test('server notices use proactive reminders without treating toast closure as acknowledgement',async({page},info)=>{
 // A client clock ahead of Runtime must not classify a new notification as history.
 await page.addInitScript(()=>{const now=Date.now;Date.now=()=>now()+60000;});
 await login(page);await page.goto('/#/account/notifications');
 await page.getByRole('switch',{name:'Notification alerts',exact:true}).check();
 await page.goto('/#/plans');
 const a=await blockedAssistant(page,`Proactive notice ${info.project.name}-${Date.now()}`);
 const toast=page.locator('.ui-toast').filter({hasText:'A notification needs your attention'});
 await expect(toast).toHaveCount(1);
 const n=(await notices(page,a.ID))[0];expect(n).toBeTruthy();
 await toast.getByRole('button',{name:'Close',exact:true}).click();await expect(toast).toHaveCount(0);
 const unchanged=await(await page.request.get(`${base}/api/notifications/${n.ID}`)).json();expect(unchanged.ReadAt).toBeNull();expect(unchanged.DismissedAt).toBeNull();
 await page.reload();await expect(page.getByRole('heading',{name:'Plans',exact:true})).toBeVisible();await expect(toast).toHaveCount(0);
 const b=await blockedAssistant(page,`Another notice ${info.project.name}-${Date.now()}`);await expect(toast).toHaveCount(1);
 const next=(await notices(page,b.ID))[0];
 await post(page,'/api/notifications/update',{IDs:[next.ID],Action:'dismiss'},false);
 await expect(toast).toHaveCount(0);
 const c=await blockedAssistant(page,`Open source ${info.project.name}-${Date.now()}`);await expect(toast).toHaveCount(1);
 await toast.getByRole('button',{name:'View details',exact:true}).click();
 await expect(page).toHaveURL(new RegExp(`/assistants/${c.ID}\\?panel=info`));
 await expect.poll(async()=>(await notices(page,c.ID))[0]?.ReadAt).toBeTruthy();
});


// Mixed and repeated kinds are fixture data at the rendering boundary; persistence
// and cross-device behavior above use real Runtime records.
test('chat queues one notice, keeps the current item stable and advances only after acknowledgement',async({page},info)=>{
 await login(page);const a=await blockedAssistant(page,`Queue ${info.project.name}-${Date.now()}`);
 await expect.poll(async()=>(await notices(page,a.ID)).length).toBe(1);
 const seed=(await notices(page,a.ID))[0];
 let rows=[{...seed,ID:'second',Kind:'weixin.delivery',Code:'unknown:'},{...seed,ID:'first',Kind:'weixin.delivery',Code:'unknown:'}];
 let reject=true;const updates:string[][]=[];
 await page.route('**/api/notifications?*',async route=>route.fulfill({json:{Items:rows,NextCursor:'',UnreadCount:rows.length,PendingCount:rows.length}}));
 await page.route('**/api/notifications/update',async route=>{
  const body=route.request().postDataJSON();updates.push(body.IDs);
  if(reject){await route.fulfill({status:503,json:{error:{code:'unavailable',message:'Try again'}}});return;}
  rows=rows.filter(n=>!body.IDs.includes(n.ID));await route.fulfill({json:{}});
 });
 await page.goto(`/#/assistants/${a.ID}`);
 const region=page.getByRole('region',{name:'Assistant notices',exact:true}),banner=region.locator('.assistant-notice');
 await expect(region).toHaveAttribute('data-notification-id','first');await expect(banner).toHaveCount(1);
 await expect(region).toContainText('1 more notice');
 // Query refresh from another business event must not replace the selected item.
 rows.unshift({...seed,ID:'third',Kind:'permission.request',Code:'pending'});
 await post(page,`/api/assistants/${a.ID}/stop`,{},false);
 await expect(region).toContainText('2 more notices');
 await expect(region).toHaveAttribute('data-notification-id','first');
 await banner.getByRole('button',{name:'Close',exact:true}).click();
 await expect.poll(()=>updates.length).toBe(1);
 await expect(region).toHaveAttribute('data-notification-id','first');
 reject=false;
 await banner.getByRole('button',{name:'Close',exact:true}).click();
 await expect(region).toHaveAttribute('data-notification-id','second');
 expect(updates).toEqual([['first'],['first']]);
 await expect(banner.getByRole('button',{name:'Close',exact:true})).toBeFocused();
 expect(await region.evaluate(el=>el.scrollHeight<=el.clientHeight)).toBe(true);
 await page.screenshot({path:info.outputPath('notification-queue.png')});
 await banner.getByRole('button',{name:'Close',exact:true}).click();
 await expect(region).toHaveAttribute('data-notification-id','third');
 await expect(banner).toHaveCount(1);
 await banner.getByRole('button',{name:'Close',exact:true}).click();
 await expect(region).toHaveCount(0);
 expect(updates).toEqual([['first'],['first'],['second'],['third']]);
 await expect(page.locator('.chat-input textarea')).toBeVisible();
});

test('Weixin reconnect notice explains automatic retries and opens channel management',async({page},info)=>{
 await login(page);const a=await blockedAssistant(page,`Weixin reconnect ${info.project.name}-${Date.now()}`);
 await expect.poll(async()=>(await notices(page,a.ID)).length).toBe(1);
 const seed=(await notices(page,a.ID))[0];
 const notice={...seed,ID:'weixin-reconnect',Kind:'weixin.connection',Code:'reconnecting'};
 await page.route('**/api/notifications?*',route=>route.fulfill({json:{Items:[notice],NextCursor:'',UnreadCount:1,PendingCount:1}}));
 await page.route(`**/api/assistants/${a.ID}/weixin`,route=>route.fulfill({json:{Channel:'weixin',State:'reconnecting',LastError:'unknown',Deliveries:[]}}));
 await page.goto(`/#/assistants/${a.ID}`);
 const region=page.getByRole('region',{name:'Assistant notices',exact:true});
 await expect(region).toContainText('Weixin connection interrupted');
 await expect(region).toContainText('Reconnecting automatically.');
 await expect(region).not.toContainText('Scan again');
 await region.getByRole('button',{name:'View details',exact:true}).click();
 const dialog=page.getByRole('dialog',{name:'Chat channel',exact:true});
 await expect(dialog).toBeVisible();await expect(dialog).toContainText('Reconnecting');
 await expect(dialog.getByRole('button',{name:'Scan again',exact:true})).toHaveCount(0);
});
