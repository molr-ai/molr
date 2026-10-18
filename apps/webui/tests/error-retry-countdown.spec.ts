import {test,expect} from '@playwright/test';

test.beforeEach(async({page})=>{
 await page.clock.install({time:new Date('2026-01-01T00:00:00Z')});
 await page.clock.pauseAt(new Date('2026-01-01T00:00:01Z'));
 await page.addInitScript(()=>{
  window.retryNoticeWakeups=0;
  const timeout=window.setTimeout.bind(window),interval=window.setInterval.bind(window);
  const wrap=(handler:TimerHandler)=>{
   const tracked=new Error().stack?.includes('/src/error-toasts.tsx');
   return tracked&&typeof handler==='function'? (...args:unknown[])=>{window.retryNoticeWakeups++;handler(...args);}:handler;
  };
  window.setTimeout=((handler:TimerHandler,delay?:number,...args:unknown[])=>timeout(wrap(handler),delay,...args)) as typeof window.setTimeout;
  window.setInterval=((handler:TimerHandler,delay?:number,...args:unknown[])=>interval(wrap(handler),delay,...args)) as typeof window.setInterval;
 });
 await page.goto('/tests/fixtures/error-retry.html');
 await expect(page.getByRole('heading',{name:'Error retry countdown'})).toBeVisible();
});

test('expired retry countdown stops waking while the notice remains visible',async({page})=>{
 await page.getByRole('button',{name:'Wait 2.5 seconds',exact:true}).click();
 const retry=page.locator('.ui-toast__action');
 await expect(retry).toBeDisabled();
 await page.clock.runFor(3000);await expect(retry).toBeEnabled();
 // Let the render and its passive effect finish before measuring idle work.
 await page.clock.runFor(50);
 const before=await page.evaluate(()=>window.retryNoticeWakeups);
 expect(before).toBeGreaterThan(0); // Prove instrumentation saw the active countdown.
 await page.clock.runFor(10000);
 expect(await page.evaluate(()=>window.retryNoticeWakeups)).toBe(before);
 await retry.click();await expect(page.getByLabel('Retry attempts')).toHaveText('1');
});

test('fractional deadline enables retry when it expires and never before',async({page})=>{
 await page.getByRole('button',{name:'Wait 2.5 seconds',exact:true}).click();
 const retry=page.locator('.ui-toast__action');
 await page.clock.runFor(2499);await expect(retry).toBeDisabled();
 await page.clock.runFor(1);await expect(retry).toBeEnabled();await expect(retry).toHaveText('Retry');
});

test('an error without a retry action updates its remaining wait',async({page})=>{
 await page.getByRole('button',{name:'Toggle retry action',exact:true}).click();
 await page.getByRole('button',{name:'Wait 5 seconds',exact:true}).click();
 await expect(page.locator('.ui-toast--warning')).toBeVisible();
 // Let the render and its passive effect finish before measuring idle work.
 await page.clock.runFor(50);
 const before=await page.evaluate(()=>window.retryNoticeWakeups);
 await page.clock.runFor(3000);
 expect(await page.evaluate(()=>window.retryNoticeWakeups)).toBeGreaterThan(before);
 await page.getByRole('button',{name:'Toggle retry action',exact:true}).click();
 await expect(page.locator('.ui-toast__action')).toBeDisabled();
 await page.clock.runFor(2000);await expect(page.locator('.ui-toast__action')).toBeEnabled();
});

test('replacing a deadline cancels the previous countdown',async({page})=>{
 const retry=page.locator('.ui-toast__action');
 await page.getByRole('button',{name:'Wait 2.5 seconds',exact:true}).click();
 await page.clock.runFor(1000);
 await page.getByRole('button',{name:'Wait 5 seconds',exact:true}).click();
 await page.clock.runFor(4999);await expect(retry).toBeDisabled();
 await page.clock.runFor(1);await expect(retry).toBeEnabled();
 await page.getByRole('button',{name:'Wait 5 seconds',exact:true}).click();await expect(retry).toBeDisabled();
 await page.getByRole('button',{name:'Expired deadline',exact:true}).click();await expect(retry).toBeEnabled();
 // Let the render and its passive effect finish before measuring idle work.
 await page.clock.runFor(50);
 const before=await page.evaluate(()=>window.retryNoticeWakeups);await page.clock.runFor(10000);
 expect(await page.evaluate(()=>window.retryNoticeWakeups)).toBe(before);
});

for(const action of ['Clear error','Toggle notice','Close']) {
 test(`${action} cancels the active countdown`,async({page})=>{
  await page.getByRole('button',{name:'Wait 5 seconds',exact:true}).click();
  await expect(page.locator('.ui-toast__action')).toBeDisabled();await page.clock.runFor(1000);
  await page.getByRole('button',{name:action,exact:true}).click();
  if(action==='Clear error'||action==='Close')await expect(page.locator('.ui-toast--warning')).toBeHidden();
  else await expect(page.locator('.ui-toast__action')).toHaveCount(0);
  // Let the render and its passive effect finish before measuring idle work.
  await page.clock.runFor(50);
  const before=await page.evaluate(()=>window.retryNoticeWakeups);await page.clock.runFor(10000);
  expect(await page.evaluate(()=>window.retryNoticeWakeups)).toBe(before);
 });
}
