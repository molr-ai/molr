import {test,expect} from '@playwright/test';

declare global {interface Window {logoClockProbe:{visibility:DocumentVisibilityState;wakeups:number;listeners:Set<EventListenerOrEventListenerObject>}}}
async function visibility(page:import('@playwright/test').Page,value:DocumentVisibilityState){
 await page.evaluate(value=>{window.logoClockProbe.visibility=value;document.dispatchEvent(new Event('visibilitychange'));},value);
}
async function mount(page:import('@playwright/test').Page,initial:DocumentVisibilityState='visible'){
 await page.clock.install({time:new Date('2026-01-01T00:00:00Z')});
 await page.clock.pauseAt(new Date('2026-01-01T00:00:01Z'));
 await page.addInitScript(initial=>{
  const probe=window.logoClockProbe={visibility:initial,wakeups:0,listeners:new Set<EventListenerOrEventListenerObject>()};
  Object.defineProperty(document,'visibilityState',{configurable:true,get:()=>probe.visibility});
  const interval=window.setInterval.bind(window);
  window.setInterval=((handler:TimerHandler,delay?:number,...args:unknown[])=>{
   const tracked=new Error().stack?.includes('/InteractiveLogo.tsx');
   return interval(tracked&&typeof handler==='function'? (...values:unknown[])=>{probe.wakeups++;handler(...values);}:handler,delay,...args);
  }) as typeof window.setInterval;
  const add=document.addEventListener.bind(document),remove=document.removeEventListener.bind(document);
  document.addEventListener=((type:string,listener:EventListenerOrEventListenerObject,options?:boolean|AddEventListenerOptions)=>{
   if(type==='visibilitychange'&&new Error().stack?.includes('/InteractiveLogo.tsx'))probe.listeners.add(listener);
   add(type,listener,options);
  }) as typeof document.addEventListener;
  document.removeEventListener=((type:string,listener:EventListenerOrEventListenerObject,options?:boolean|EventListenerOptions)=>{
   if(type==='visibilitychange')probe.listeners.delete(listener);remove(type,listener,options);
  }) as typeof document.removeEventListener;
 },initial);
 await page.emulateMedia({reducedMotion:'no-preference'});
 await page.goto('/tests/fixtures/regressions.html');
 await expect(page.locator('#logo button')).toHaveAttribute('data-ready','true');
 await expect(page.locator('#custom-logo button')).toHaveAttribute('data-ready','true');
}

test('hidden logos stop polling and immediately read the SVG clock on return',async({page})=>{
 await mount(page);const logo=page.locator('#logo button');
 const seek=async(time:number)=>logo.locator('object').evaluate((element,time)=>{const svg=(element as HTMLObjectElement).getSVGDocument()!.documentElement as unknown as SVGSVGElement;svg.setCurrentTime(time);svg.pauseAnimations();},time);
 await seek(1);await page.clock.runFor(300);await expect(logo).toHaveAttribute('data-phase','playing');
 expect(await page.evaluate(()=>window.logoClockProbe.wakeups)).toBeGreaterThan(0);
 await visibility(page,'hidden');const before=await page.evaluate(()=>window.logoClockProbe.wakeups);
 await seek(3);await page.clock.runFor(2000);expect(await page.evaluate(()=>window.logoClockProbe.wakeups)).toBe(before);
 await expect(logo).toHaveAttribute('data-phase','playing');
 await visibility(page,'visible');await expect(logo).toHaveAttribute('data-phase','waiting');
 await logo.click();await expect(logo).toHaveAttribute('data-phase','playing');
 await page.clock.runFor(300);expect(await page.evaluate(()=>window.logoClockProbe.wakeups)).toBeGreaterThan(before);
 await visibility(page,'visible');await visibility(page,'visible');
 const repeated=await page.evaluate(()=>window.logoClockProbe.wakeups);
 await page.clock.runFor(300);expect(await page.evaluate(()=>window.logoClockProbe.wakeups)-repeated).toBe(4);
});

test('a logo loaded in a hidden page stays idle until it becomes visible',async({page})=>{
 await mount(page,'hidden');const before=await page.evaluate(()=>window.logoClockProbe.wakeups);
 await page.clock.runFor(2000);expect(await page.evaluate(()=>window.logoClockProbe.wakeups)).toBe(before);
 await visibility(page,'visible');await page.clock.runFor(300);expect(await page.evaluate(()=>window.logoClockProbe.wakeups)).toBeGreaterThan(before);
});

test('reduced motion and unmount release the visibility subscription and polling',async({page})=>{
 await mount(page);
 await expect.poll(()=>page.evaluate(()=>window.logoClockProbe.listeners.size)).toBe(2);
 await visibility(page,'hidden');await page.emulateMedia({reducedMotion:'reduce'});
 await expect(page.locator('#logo object')).toHaveCount(0);await expect.poll(()=>page.evaluate(()=>window.logoClockProbe.listeners.size)).toBe(0);
 await page.emulateMedia({reducedMotion:'no-preference'});await expect(page.locator('#logo button')).toHaveAttribute('data-ready','true');
 await expect.poll(()=>page.evaluate(()=>window.logoClockProbe.listeners.size)).toBe(2);
 const before=await page.evaluate(()=>window.logoClockProbe.wakeups);await page.clock.runFor(2000);expect(await page.evaluate(()=>window.logoClockProbe.wakeups)).toBe(before);
 await visibility(page,'visible');await page.clock.runFor(300);expect(await page.evaluate(()=>window.logoClockProbe.wakeups)).toBeGreaterThan(before);
 await page.getByRole('button',{name:'Toggle logos',exact:true}).click();await expect.poll(()=>page.evaluate(()=>window.logoClockProbe.listeners.size)).toBe(0);
 const unmounted=await page.evaluate(()=>window.logoClockProbe.wakeups);await page.clock.runFor(2000);expect(await page.evaluate(()=>window.logoClockProbe.wakeups)).toBe(unmounted);
});
