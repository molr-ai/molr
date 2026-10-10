import {test,expect} from '@playwright/test';

const port=process.env.MOLR_TEST_PORT??'5193',origin=`http://127.0.0.1:${port}`;
test('turn information subscribes to visibility only while open and resumes current visibility',async({page})=>{
 await page.addInitScript(()=>{
  const listeners=new Set<EventListenerOrEventListenerObject>();
  const state={value:'visible'};Object.defineProperty(window,'turnVisibilityState',{value:state});
  Object.defineProperty(document,'visibilityState',{configurable:true,get:()=>state.value});
  const add=document.addEventListener.bind(document),remove=document.removeEventListener.bind(document);
  document.addEventListener=((type:string,listener:EventListenerOrEventListenerObject,options?:boolean|AddEventListenerOptions)=>{
   if(type==='visibilitychange'&&new Error().stack?.includes('/TurnInformation.tsx'))listeners.add(listener);
   add(type,listener,options);
  }) as typeof document.addEventListener;
  document.removeEventListener=((type:string,listener:EventListenerOrEventListenerObject,options?:boolean|EventListenerOptions)=>{
   if(type==='visibilitychange')listeners.delete(listener);remove(type,listener,options);
  }) as typeof document.removeEventListener;
  Object.defineProperty(window,'turnVisibilityListenerCount',{value:()=>listeners.size});
 });
 await page.goto('/');await page.getByLabel('Username',{exact:true}).fill('owner');await page.getByLabel('Password',{exact:true}).fill('webui-e2e-password-2026!');await page.getByRole('button',{name:'Sign in',exact:true}).click();await expect(page.getByRole('navigation',{name:'Molr',exact:true})).toBeVisible();
 const csrf=(await page.context().cookies()).find(c=>c.name===`molr_dev_csrf_p${port}`)!.value;
 const post=async(path:string,data:object)=>{const r=await page.request.post(path,{headers:{Origin:origin,'X-CSRF-Token':csrf},data:{...data,Mutation:{IdempotencyKey:crypto.randomUUID()}}});expect(r.ok(),await r.text()).toBe(true);return r.json();};
 const resource=await post('/api/resources',{Name:'Visibility fixture',Kind:'model',Provider:'openai-compatible',Binding:{Endpoint:'https://models.example.invalid/v1',AllowedOrigins:['https://models.example.invalid'],Models:[{Name:'visibility-model',SupportsTools:true,Pricing:{Currency:'USD',InputMicrosPerMillion:1000000}}]}});
 const models=await(await page.request.get('/api/models')).json();const model=models.find((m:{ResourceID:string})=>m.ResourceID===resource.ID);
 const assistant=await post('/api/assistants',{Name:'Visibility review',ModelID:model.ID,Budget:{Limit:{Currency:'USD',Micros:0},Period:'daily'}});
 const chatPath=`/api/conversations/${assistant.ConversationID}`;
 // Render a fixed long history; model execution and metrics accounting are not part of this check.
 await page.route(`**${chatPath}`,async route=>{const response=await route.fetch();await route.fulfill({json:{...await response.json(),LastSequence:50,LastReadSequence:50,UnreadCount:0}});});
 await page.route(`**${chatPath}/messages?*`,route=>route.fulfill({json:{Items:Array.from({length:50},(_,i)=>({ID:`visibility-message-${i}`,ConversationID:assistant.ConversationID,Sequence:i+1,Sender:'assistant',SenderID:assistant.ID,Content:{Kind:'text',Text:`Visibility message ${i}`},CreatedAt:new Date().toISOString()})),NextCursor:''}}));
 const requests:string[]=[];
 await page.route(`**${chatPath}/messages/*/metrics`,async route=>{
  requests.push(route.request().url());
  await route.fulfill({json:{State:'running',Finalized:false,CostComplete:true,Charges:[],Models:['fixture'],Usage:{InputTokens:10,CachedTokens:0,OutputTokens:2},CompletedCalls:1,ModelCalls:1,ToolCalls:0,ElapsedMs:1000,UpdatedAt:new Date().toISOString()}});
 });
 await page.goto(`/#/assistants/${assistant.ID}`);
 await expect(page.getByRole('button',{name:'Turn information',exact:true})).toHaveCount(50);
 const count=()=>page.evaluate(()=>(window as unknown as {turnVisibilityListenerCount:()=>number}).turnVisibilityListenerCount());
 const visibility=async(value:'visible'|'hidden')=>page.evaluate(value=>{(window as unknown as {turnVisibilityState:{value:string}}).turnVisibilityState.value=value;document.dispatchEvent(new Event('visibilitychange'));},value);
 const trigger=page.getByRole('button',{name:'Turn information',exact:true}).last();
 const dialog=page.getByRole('dialog',{name:'Turn information',exact:true});
 await expect.poll(count).toBe(0);expect(requests).toHaveLength(0);
 await trigger.click();await expect(dialog).toBeVisible();await expect.poll(count).toBe(1);await expect.poll(()=>requests.length).toBe(1);
 await expect(dialog.getByText('Running',{exact:true})).toBeVisible();
 await test.step('an open hidden panel pauses and resumes requests',async()=>{
  await visibility('hidden');const before=requests.length;await page.waitForTimeout(2200);expect(requests).toHaveLength(before);expect(await count()).toBe(1);
  await visibility('visible');await expect.poll(()=>requests.length).toBe(before+1);
 });
 await dialog.getByRole('button',{name:'Close',exact:true}).click();await expect.poll(count).toBe(0);
 await test.step('closed panels remain idle and reopening reads current visibility',async()=>{
  const before=requests.length;await page.waitForTimeout(2200);expect(requests).toHaveLength(before);
  await visibility('hidden');await trigger.click();await expect(dialog).toBeVisible();await expect.poll(count).toBe(1);await page.waitForTimeout(350);expect(requests).toHaveLength(before);
  await visibility('visible');await expect.poll(()=>requests.length).toBe(before+1);
  await visibility('hidden');await dialog.getByRole('button',{name:'Close',exact:true}).click();await expect.poll(count).toBe(0);
  await visibility('visible');await trigger.click();await expect.poll(()=>requests.length).toBe(before+2);await expect.poll(count).toBe(1);
 });
 await page.evaluate(()=>{location.hash='/plans';});await expect(page.getByRole('heading',{name:'Plans',exact:true})).toBeVisible();await expect.poll(count).toBe(0);
});
