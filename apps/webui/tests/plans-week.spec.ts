import { readLayout } from '../../../scripts/test-support/layout.ts';
import { test, expect, type Page } from '@playwright/test';
import { expectFitsWebView } from './viewport';
const port = process.env.MOLR_TEST_PORT ?? process.env.MOLR_E2E_PORT ?? '5193';
const base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: 0 }, ...data } });
  expect(response.ok(), await response.text()).toBe(true); return response.json();
}
async function login(page: Page, username = 'owner', password = 'webui-e2e-password-2026!') {
  await page.goto('/'); await page.getByLabel('Username', { exact: true }).fill(username);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
}
const key = (d: Date) => `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')}`;

test('calendar loading replaces the grid until data arrives and preserves it during refresh', async ({ page }) => {
  await login(page);
  let release = () => {};
  let requested = () => {};
  let responseGate = Promise.resolve();
  function holdPlans() {
    responseGate = new Promise<void>(resolve => { release = resolve; });
    return new Promise<void>(resolve => { requested = resolve; });
  }
  await page.route('**/api/plans?*', async route => {
    const gate = responseGate;
    requested();
    await gate;
    await route.continue();
  });
  const workspace = page.locator('.plans-workspace');
  const loading = workspace.locator('.loading');
  const grids = workspace.locator('.plans-week-calendar, .plans-month-calendar');
  async function expectLoading() {
    await expect(loading).toHaveCount(1);
    await expect(workspace.locator('.plans-calendar-toolbar')).toBeVisible();
    await expect(grids).toHaveCount(0);
    await expect(workspace.locator('.plans-empty')).toHaveCount(0);
  }
  try {
    let pending = holdPlans();
    await page.goto('/#/plans');
    await pending;
    await expectLoading();
    release();
    await expect(workspace.locator('.plans-week-calendar')).toBeVisible();
    await expect(loading).toHaveCount(0);

    pending = holdPlans();
    await page.getByRole('button', { name: 'Month', exact: true }).click();
    await pending;
    await expectLoading();
    release();
    await expect(workspace.locator('.plans-month-calendar')).toBeVisible();

    pending = holdPlans();
    await page.getByRole('button', { name: 'Next month', exact: true }).click();
    await pending;
    await expectLoading();
    release();
    await expect(workspace.locator('.plans-month-calendar')).toBeVisible();
    await expect(loading).toHaveCount(0);

    pending = holdPlans();
    await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
    await pending;
    await expect(workspace.locator('.plans-month-calendar')).toBeVisible();
    await expect(loading).toHaveCount(0);
  } finally {
    release();
    await page.unrouteAll({ behavior: 'wait' });
  }
});

test('week is default; events retain their duration, overlaps, midnight boundaries and details', async ({ page }, info) => {
  await login(page);
  // A private calendar keeps geometry assertions independent of other tests
  // and repeated runs against a retained acceptance Runtime.
  const suffix=crypto.randomUUID(),username=`week-${suffix}`,password=`week-${suffix}-password!`;
  const group=await post(page,'/api/admin/groups',{Group:{Name:`Week ${suffix}`,Administrator:false,Resources:[]}});
  await post(page,'/api/admin/users',{Username:username,DisplayName:'Week review',Password:password,GroupID:group.ID});
  await post(page,'/api/auth/logout',{});await login(page,username,password);
  const monday = new Date(); monday.setDate(monday.getDate()-(monday.getDay()+6)%7); monday.setHours(9,0,0,0);
  const at = (day: number, hour: number, minute = 0) => { const d = new Date(monday); d.setDate(d.getDate()+day); d.setHours(hour,minute); return d.toISOString(); };
  const prefix = `Week ${info.project.name} ${Date.now()}`;
  await post(page, '/api/todos', { Title: `${prefix} long`, StartAt: at(0,9), EndAt: at(0,11) });
  await post(page, '/api/todos', { Title: `${prefix} overlap`, StartAt: at(0,10), EndAt: at(0,11) });
  await post(page, '/api/todos', { Title: `${prefix} midnight`, StartAt: at(-1,23), EndAt: at(0,1) });
  await post(page, '/api/todos', { Title: `${prefix} point`, DueAt: at(0,23,55) });
  await post(page, '/api/schedules', { Title: `${prefix} repeat`, Kind: 'personal', EndAt: at(0,15), Trigger: { StartAt: at(0,14), Timezone: 'Asia/Shanghai', Recurrence: 'FREQ=DAILY;COUNT=3', MissedPolicy: 'coalesce' } });
  await page.goto('/#/plans');
  const week = page.locator('.plans-week-calendar');
  await expect(week).toHaveAttribute('data-week',key(monday));await expect(page.locator('.plans-calendar-return button')).toHaveCount(0);
  await expect(page.getByRole('button',{name:'Week',exact:true})).toHaveAttribute('aria-pressed','true');
  await expect(week.locator('.plans-week-day')).toHaveCount(7);
  await expect(week.locator('.plans-week-now')).toHaveCount(1);
  const event = (name: string) => week.locator('.plans-week-event').filter({hasText:`${prefix} ${name}`});
  await expect(event('repeat')).toHaveCount(3);
  const [long, overlap] = await readLayout(event('long'), event('overlap'));
  expect(long!.height).toBeCloseTo(126,0); expect(overlap!.height).toBeCloseTo(62,0);
  expect(long!.x+long!.width).toBeLessThanOrEqual(overlap!.x);
  expect(await event('midnight').evaluate(el => (el as HTMLElement).style.top)).toBe('0%');
  expect(await event('point').evaluate(el => {const r=el.getBoundingClientRect(),p=el.parentElement!.getBoundingClientRect();return r.bottom<=p.bottom;})).toBe(true);
  await event('long').click();
  const drawer=page.getByRole('dialog',{name:'Plan details',exact:true});await expect(drawer).toContainText(`${prefix} long`);
  await page.keyboard.press('Escape');await expect(drawer).toBeHidden();await expect(event('long')).toBeFocused();
  await event('repeat').nth(1).click();
  await drawer.getByRole('button',{name:'Complete this execution',exact:true}).click();
  await page.getByRole('dialog',{name:'Complete this execution',exact:true}).getByRole('button',{name:'Complete this execution',exact:true}).click();
  await expect(event('repeat').nth(1)).toHaveAttribute('data-completed','true');
  await expect(event('repeat').nth(0)).toHaveAttribute('data-completed','false');
  await expect(page.getByRole('dialog',{name:'Complete this execution',exact:true})).toBeHidden();
  await drawer.getByRole('button',{name:'Close details',exact:true}).click();
  await expect(drawer).toBeHidden();
  await page.locator('.plans-week-date').first().click();
  await expect(page.getByRole('dialog',{name:`${key(monday)} · Day agenda`,exact:true})).toContainText(`${prefix} midnight`);
  await page.keyboard.press('Escape');
  await page.getByRole('button',{name:'Next week',exact:true}).click(); const next=new Date(monday);next.setDate(next.getDate()+7);
  await expect(week).toHaveAttribute('data-week',key(next));await expect(event('repeat')).toHaveCount(0);
  await page.getByRole('button',{name:'Previous week',exact:true}).click();await expect(event('repeat')).toHaveCount(3);
  await page.getByRole('combobox',{name:'Plan type',exact:true}).click();await page.getByRole('option',{name:'Time block',exact:true}).click();
  await expect(event('long')).toHaveCount(0);await expect(event('repeat')).toHaveCount(3);
  await page.getByRole('button',{name:'Clear filters',exact:true}).click();await expect(event('long')).toHaveCount(1);
  await page.getByRole('button',{name:'Month',exact:true}).click();await expect(page.locator('.plans-month-calendar')).toBeVisible();
  await page.getByRole('button',{name:'List',exact:true}).click();await expect(page.locator('.plans-workspace')).toHaveAttribute('data-view','list');await expect(week).toHaveCount(0);
  await page.reload();await expect(week).toBeVisible();
  await expectFitsWebView(page);
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  const scroll=page.locator('.plans-week-scroll');
  await scroll.evaluate(el=>{el.scrollTop=el.scrollHeight;el.scrollLeft=el.scrollWidth;});
  if(info.project.name==='mobile') expect(await scroll.evaluate(el=>el.scrollLeft)).toBeGreaterThan(0);
  await expectFitsWebView(page);
  await page.setViewportSize({...page.viewportSize()!,height:420});await expectFitsWebView(page);
});

test('week navigation crosses a year and returns to the current week', async ({page}) => {
  await login(page);
  const plan=await post(page,'/api/todos',{Title:`Year boundary ${Date.now()}`,StartAt:'2026-12-31T01:00:00Z',EndAt:'2026-12-31T02:00:00Z'});
  await page.goto(`/#/plans/${plan.ID}`);
  await expect(page.getByRole('dialog',{name:'Plan details',exact:true})).toBeVisible();await page.keyboard.press('Escape');
  const week=page.locator('.plans-week-calendar');await expect(week).toHaveAttribute('data-week','2026-12-28');
  await page.getByRole('button',{name:'Next week',exact:true}).click();await expect(week).toHaveAttribute('data-week','2027-01-04');
  await page.getByRole('button',{name:'Previous week',exact:true}).click();await expect(week).toHaveAttribute('data-week','2026-12-28');
  await page.getByRole('button',{name:'Back to this week',exact:true}).click();const monday=new Date();monday.setDate(monday.getDate()-(monday.getDay()+6)%7);await expect(week).toHaveAttribute('data-week',key(monday));await expect(page.locator('.plans-calendar-return button')).toHaveCount(0);
});


test.describe('local dates across daylight saving', () => {
  test.use({timezoneId:'America/New_York'});
  test('keeps Monday boundaries across the autumn clock change', async ({page}) => {
    await login(page);
    const plan=await post(page,'/api/todos',{Title:`DST boundary ${Date.now()}`,StartAt:'2026-11-01T03:30:00Z',EndAt:'2026-11-01T07:30:00Z'});
    await page.goto(`/#/plans/${plan.ID}`);
    await expect(page.getByRole('dialog',{name:'Plan details',exact:true})).toBeVisible();await page.keyboard.press('Escape');
    const week=page.locator('.plans-week-calendar');await expect(week).toHaveAttribute('data-week','2026-10-26');
    await expect(week.locator('.plans-week-event').filter({hasText:plan.Title})).toHaveCount(2);
    const sunday=week.locator('[data-day="2026-11-01"] .plans-week-event').filter({hasText:plan.Title});
    expect(await sunday.evaluate(el=>(el as HTMLElement).style.top)).toBe('0%');
    expect((await sunday.boundingBox())!.height).toBeCloseTo(158,0);
    await page.getByRole('button',{name:'Next week',exact:true}).click();await expect(week).toHaveAttribute('data-week','2026-11-02');
    await page.getByRole('button',{name:'Previous week',exact:true}).click();await expect(week).toHaveAttribute('data-week','2026-10-26');
  });
});


test('centered period opens the shared picker and follows week and month navigation', async ({page}) => {
  await login(page);
  const plan=await post(page,'/api/todos',{Title:`Period controls ${Date.now()}`,StartAt:'2026-12-31T01:00:00Z',EndAt:'2026-12-31T02:00:00Z'});
  await page.goto(`/#/plans/${plan.ID}`);
  await expect(page.getByRole('dialog',{name:'Plan details',exact:true})).toBeVisible();await page.keyboard.press('Escape');
  const period=page.locator('.plans-calendar-period-trigger'),toolbar=page.locator('.plans-calendar-toolbar');
  const label=(start:string,end:string)=>new Intl.DateTimeFormat('en',{year:'numeric',month:'short',day:'numeric'}).formatRange(new Date(start+'T12:00:00'),new Date(end+'T12:00:00'));
  await expect(period).toHaveText(label('2026-12-28','2027-01-03'));
  await page.getByRole('button',{name:'Next week',exact:true}).click();
  await expect(period).toHaveText(label('2027-01-04','2027-01-10'));
  await period.click();
  const picker=page.getByRole('dialog',{name:'Date and time',exact:true});await expect(picker).toBeVisible();
  await expect(picker.getByRole('combobox',{name:'Year',exact:true})).toContainText('2027');
  await expect(picker.getByRole('combobox',{name:'Month',exact:true})).toContainText('Jan');
  await picker.getByRole('button',{name:'Cancel',exact:true}).click();await expect(period).toBeFocused();
  await expect(period).toHaveText(label('2027-01-04','2027-01-10'));
  await period.click();await picker.locator('[data-date="2027-0-20"]').click();await picker.getByRole('button',{name:'Apply',exact:true}).click();
  await expect(period).toHaveText(label('2027-01-18','2027-01-24'));await expect(page.locator('.plans-week-calendar')).toHaveAttribute('data-week','2027-01-18');
  await page.getByRole('button',{name:'Month',exact:true}).click();await expect(period).toHaveText('January 2027');
  await period.click();const month=page.getByRole('dialog',{name:'Month and year',exact:true});
  await month.getByRole('combobox',{name:'Month',exact:true}).click();await page.getByRole('option',{name:'Feb',exact:true}).click();await month.getByRole('button',{name:'Apply',exact:true}).click();await expect(month).toBeHidden();await expect(period).toBeFocused();
  await expect(period).toHaveText('February 2027');await expect(page.locator('.plans-month-calendar')).toHaveAttribute('data-month','2027-02');
  await toolbar.getByRole('button',{name:'Next month',exact:true}).click();await expect(period).toHaveText('March 2027');
  await expect(toolbar.locator('.plans-date-jump')).toHaveCount(0);
  const [bounds, nav, modes] = await readLayout(toolbar, page.locator('.plans-calendar-navigation'), page.getByRole('group',{name:'Calendar mode',exact:true}));
  expect(Math.abs(nav!.x+nav!.width/2-bounds!.x-bounds!.width/2)).toBeLessThan(2);
  expect(Math.abs(modes!.x+modes!.width-bounds!.x-bounds!.width)).toBeLessThan(2);
  expect(modes!.height).toBeLessThanOrEqual(34);
  await expectFitsWebView(page);expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});
