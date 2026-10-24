import { test, expect, type Page } from '@playwright/test';
import { compareBuilds, upgradeCommand } from '../src/updates/build';
import type { UpdateStatus } from '@molr/sdk';

async function login(page: Page) {
 await page.goto('/');
 await page.getByLabel('Username', { exact: true }).fill('owner');
 await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
 await page.getByRole('button', { name: 'Sign in', exact: true }).click();
 await expect(page.locator('.workspace')).toBeVisible();
}
async function reopenVersionDetails(page: Page) {
 await page.getByRole('button', { name: 'Close version details', exact: true }).click();
 await expect(page.getByRole('dialog')).toHaveCount(0);
 await page.getByRole('button', { name: 'View version details', exact: true }).click();
}
test('build comparison distinguishes missing metadata, versions and commits', () => {
 const front = { version: '0.1.0', commit: 'a'.repeat(40) };
 expect(compareBuilds(front, front)).toBe('consistent');
 expect(compareBuilds(front, { ...front, version: '0.2.0' })).toBe('version_mismatch');
 expect(compareBuilds(front, { ...front, commit: 'b'.repeat(40) })).toBe('commit_mismatch');
 expect(compareBuilds(front, { ...front, commit: '' })).toBe('unknown');
 expect(compareBuilds(front)).toBe('unknown');
 const command = upgradeCommand({ name: 'molr-test', system: false, setup_path: "/tmp/a'b/bin/molr-setup", source: 'https://example.com/releases' }, '0.2.0');
 expect(command).toContain("--version '0.2.0'");
 expect(command).toContain("'/tmp/a'\\''b/bin/molr-setup' update");
 expect(command).not.toContain("install.sh");
});
test('real Runtime exposes authenticated build status without contacting an unconfigured source', async ({ page }) => {
 expect((await page.request.get('/api/updates')).status()).toBe(401);
 await login(page);
 const response = await page.request.get('/api/updates');
 expect(response.ok()).toBeTruthy();
 const status: UpdateStatus = await response.json();
 expect(status.current.version).toBeTruthy();
 expect(status.source_configured).toBe(false);
 expect(status.check.last_attempt_at).toBeNull();
 expect((await page.request.post('/api/updates/check', { data: { automatic: false } })).status()).toBe(403);
 await page.goto('/#/account/about');
 await expect(page.getByRole('heading', { name: 'Service updates', exact: true })).toHaveCount(0);
 await expect(page.getByText('WebUI', { exact: true })).toHaveCount(0);
 const trigger = page.getByRole('button', { name: 'View version details', exact: true });
 await expect(trigger).toContainText(status.current.version);
 await expect(page.locator('.settings-about-version button')).toHaveCount(1);
 await trigger.click();
 await expect(page.getByRole('dialog', { name: 'Version details', exact: true })).toBeVisible();
 await expect(page.locator('.version-releases').getByText('No update source configured', { exact: true })).toBeVisible();
 await expect(page.getByRole('button', { name: 'No update source configured', exact: true })).toBeDisabled();
 await expect(page.getByRole('region', { name: 'Service · Runtime', exact: true })).toContainText(status.current.version);
 await expect(page.getByText('Updated with the service', { exact: true })).toBeVisible();
 await expect(page.getByRole('dialog').locator('.ui-dialog__footer button')).toHaveCount(1);
 await expect(page.getByRole('button', { name: 'Recheck build consistency', exact: true })).toHaveCount(0);
 await expect(page.getByText(/Last confirmed/)).toHaveCount(0);
 await page.keyboard.press('Escape');
 await expect(page.getByRole('dialog')).toHaveCount(0);
 await expect(trigger).toBeFocused();
});
test('about reactivation checks releases and keeps build mismatch separate from release status', async ({ page }) => {
 await login(page);
 const runtime: UpdateStatus = await (await page.request.get('/api/updates')).json();
 const frontend = await page.evaluate(async () => { const path = '/src/build-info.ts'; return (await import(path)).buildInfo; });
 const state: UpdateStatus = { ...runtime, auto_check: true, source_configured: true, supported: true, current: { ...runtime.current, version: frontend.version, commit: frontend.commit }, comparison: 'current', latest: { version: frontend.version, commit: frontend.commit, published_at: '2026-10-11T00:00:00Z', notes_url: 'https://example.com/releases', file: 'fixture.tar.gz', sha256: 'a'.repeat(64) } };
 let checks = 0;
 await page.route(/\/api\/updates(?:\?.*)?$/, route => route.fulfill({ json: state }));
 await page.route('**/api/updates/check', route => { checks++; return route.fulfill({ json: state }); });
 await page.goto('/#/account/about');
 await expect(page.getByText('All component versions match', { exact: true })).toBeHidden();
 await page.getByRole('button', { name: 'View version details', exact: true }).click();
 await expect(page.getByText('All component versions match', { exact: true })).toBeVisible();
 await expect(page.getByText('All component versions match', { exact: true }).locator('svg')).toHaveCount(1);
 await expect.poll(() => checks).toBeGreaterThan(0);
 const initialChecks = checks;
 await page.waitForTimeout(800);
 expect(checks).toBe(initialChecks); // Checking must not invalidate its own effect and loop.
 await page.getByRole('button', { name: 'Close version details', exact: true }).click();
 await expect(page.getByRole('dialog')).toHaveCount(0);
 const before = checks;
 await page.goto('/#/account/general');
 await page.goto('/#/account/about');
 await expect.poll(() => checks).toBeGreaterThan(before);
 await page.getByRole('button', { name: 'View version details', exact: true }).click();
 state.current = { ...state.current, commit: 'b'.repeat(40) };
 await reopenVersionDetails(page);
 await expect(page.getByRole('dialog', { name: 'Version details' })).toContainText('WebUI and Runtime commits differ');
 await expect(page.getByText('Service is up to date with the stable release', { exact: true })).toBeVisible();
 await expect(page.getByText(/Recommended version|Last successful check|Next check available/)).toHaveCount(0);
 await page.getByRole('button',{name:'Close version details',exact:true}).click();
 await expect(page.getByRole('dialog')).toHaveCount(0);
 await expect(page.locator('.version-trigger .version-tags').getByText('Version mismatch', {exact:true})).toBeVisible();
 await expect(page.locator('.version-trigger .version-tags').getByText('Update available', {exact:true})).toHaveCount(0);
 await page.locator('.version-trigger .version-tags').getByText('Version mismatch', {exact:true}).click();
 state.check.error_code = 'network_error';
 state.comparison = 'available'; state.latest!.version = '0.2.0';
 await reopenVersionDetails(page);
 await expect(page.getByText('Service update available', { exact: true })).toBeVisible();
 if (await page.locator('.nav-account-actions').isVisible()) await expect(page.locator('.nav-account-actions .update-badge')).toBeVisible();
 await expect(page.getByText('Unable to check for updates', { exact: true })).toBeVisible();
 await page.route(/\/api\/updates(?:\?.*)?$/, route => route.abort('failed'));
 await reopenVersionDetails(page);
 await expect(page.getByText('Cannot check consistency while Runtime is unreachable', { exact: true })).toBeVisible();
 await expect(page.getByText('Service update available', { exact: true })).toBeVisible();
});

test('component maintenance shows progress, survives panel navigation and exposes cancellation', async ({ page }) => {
 await login(page);
 const task = { id: 'a'.repeat(32), operation: 'component_install', component: 'browser', target: 'fixture', state: 'running', stage: 'downloading', downloaded: 50, total: 100, error_code: '' };
 let started = false, cancelled = false;
 await page.route(/\/api\/components(?:\?.*)?$/, route => route.fulfill({ json: [{ id: 'browser', state: 'missing', can_install: true, auto_install: started, download_bytes: 100, disk_bytes: 200, task: started ? task : undefined }] }));
 await page.route('**/api/components/browser/install', route => { const q=route.request().postDataJSON(); expect(q.idempotency_key).toMatch(/^[a-f0-9]{32}$/); started=true; return route.fulfill({status:202,json:task}); });
 await page.route(`**/api/maintenance/tasks/${task.id}`, route => route.fulfill({ json: task }));
 await page.route(`**/api/maintenance/tasks/${task.id}/cancel`, route => { cancelled=true;task.state='cancelled';task.error_code='maintenance_cancelled';return route.fulfill({json:task}); });
 await page.goto('/#/account/about');
 await expect(page.getByRole('button', { name: 'View version details', exact: true })).toBeVisible();
 await expect(page.getByRole('heading', { name: 'Browser components', exact: true })).toHaveCount(0);
 await page.getByRole('button', { name: 'Admin settings', exact: true }).click();
 await page.getByRole('navigation', { name: 'Admin settings', exact: true }).getByRole('button', { name: 'Components', exact: true }).click();
 await expect(page).toHaveURL(/#\/settings\/components$/);
 await page.getByRole('button',{name:'Install components',exact:true}).click();
 await expect(page.getByRole('progressbar',{name:'Downloading'})).toHaveAttribute('aria-valuenow','50');
 await page.goto('/#/settings/users');await page.goto('/#/settings/components');
 await page.reload();
 await expect(page.getByRole('progressbar',{name:'Downloading'})).toBeVisible();
 await expect(page.getByRole('button',{name:'Install components',exact:true})).toBeDisabled();
 await page.getByRole('button',{name:'Cancel installation',exact:true}).click();
 await expect.poll(()=>cancelled).toBe(true);
 await expect(page.getByText('Maintenance was cancelled.',{exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Install components',exact:true})).toBeEnabled();
 await page.unrouteAll({behavior:'wait'});
});

async function desktopFixture(page: Page, kind: 'remote' | 'managed-local', pending: boolean) {
 await page.addInitScript(({kind, pending}) => {
  let listener: ((status: any) => void) | undefined;
  const fixture = { upgrades: 0, downloads: 0, checks: 0, status: { comparison: 'available', latestVersion: '0.3.0', checking: false, ...(kind === 'managed-local' ? { local: { version: pending ? '0.1.0' : '0.2.0', targetVersion: '0.2.0', pending, busy: false } } : {}) } };
  Object.assign(window, { updateFixture: fixture, emitUpdate: () => listener?.(structuredClone(fixture.status)) });
  window.molrApp = {
   getInfo: async () => ({ apiVersion: 1, platform: 'linux', build: { version: '0.2.0', commit: 'a'.repeat(40) }, connection: { kind, name: 'Fixture', url: location.origin }, window: { maximized: false, fullscreen: false } }),
   window: { minimize: async () => {}, toggleMaximize: async () => {}, close: async () => {} },
   disconnect: async () => {}, onWindowStateChanged: () => () => {},
   oauth: { authorize: async () => ({ status: 'cancelled' }), cancel: async () => {} },
   notifications: { begin: async () => {}, end: async () => {}, setBadge: async () => true, requestPermission: async () => false, show: async () => {}, close: async () => {}, onClick: () => () => {} },
   updates: { status: async () => fixture.status as any, check: async () => { fixture.checks++; return fixture.status as any; }, openDownloads: async () => { fixture.downloads++; }, upgradeLocal: async () => { fixture.upgrades++; }, onChanged: callback => { listener = callback; return () => { listener = undefined; }; } },
  };
 }, {kind, pending});
 await login(page);
 const runtime: UpdateStatus = await (await page.request.get('/api/updates')).json();
 const frontend = await page.evaluate(async () => { const path = '/src/build-info.ts'; return (await import(path)).buildInfo; });
 const state: UpdateStatus = { ...runtime, current: { ...runtime.current, version: pending ? '0.1.0' : '0.2.0', commit: frontend.commit }, comparison: 'current' };
 await page.route(/\/api\/updates(?:\?.*)?$/, route => route.fulfill({ json: state }));
 await page.goto('/#/account/about');
 return state;
}
for (const kind of ['remote', 'managed-local'] as const) test(`${kind} consistency requires every component to match`, async ({ page }) => {
 const frontend = { version: '0.1.0', commit: 'a'.repeat(40) };
 await page.route('**/src/build-info.ts', route => route.fulfill({ contentType: 'application/javascript', body: `export const buildInfo = ${JSON.stringify(frontend)};` }));
 const state = await desktopFixture(page, kind, true);
 await page.getByRole('button', { name: 'View version details', exact: true }).click();
 const summary = page.locator('.version-consistency');
 await expect(summary).toContainText('Desktop and Runtime versions differ');
 await expect(summary.locator('[data-consistent]')).toHaveCount(0);
 await expect(summary.getByRole('status')).toHaveCount(1);
 // Updating Runtime alone leaves the loaded WebUI stale.
 state.current.version = '0.2.0';
 await reopenVersionDetails(page);
 await expect(summary).toContainText('WebUI and Runtime versions differ');
 await expect(summary.locator('[data-consistent]')).toHaveCount(0);
 // Reload the matching WebUI; all three installed components now agree.
 frontend.version = '0.2.0';
 await page.reload();
 await page.getByRole('button', { name: 'View version details', exact: true }).click();
 await expect(summary).toHaveText('All component versions match');
 await expect(summary.locator('[data-consistent] svg')).toHaveCount(1);
 await expect(page.locator('.version-trigger .version-tags').getByText('Version mismatch', { exact: true })).toHaveCount(0);
 state.current.commit = 'b'.repeat(40);
 await reopenVersionDetails(page);
 await expect(summary).toContainText('WebUI and Runtime commits differ');
 await expect(summary).toContainText('Desktop and Runtime commits differ');
 await expect(summary.locator('[data-consistent]')).toHaveCount(0);
 state.current.commit = '';
 await reopenVersionDetails(page);
 await expect(summary).toContainText('build information is incomplete');
 await expect(summary.locator('[data-consistent]')).toHaveCount(0);
 state.current.commit = 'a'.repeat(40);
 await reopenVersionDetails(page);
 await expect(summary).toHaveText('All component versions match');
 await page.route(/\/api\/updates(?:\?.*)?$/, route => route.abort('failed'));
 await reopenVersionDetails(page);
 await expect(summary).toHaveText('Cannot check consistency while Runtime is unreachable');
 await expect(summary.locator('[data-consistent]')).toHaveCount(0);
});
test('managed local update lives in the dialog, can be deferred and clears after completion', async ({ page }) => {
 const state = await desktopFixture(page, 'managed-local', true);
 const trigger=page.getByRole('button',{name:'View version details',exact:true});
 await expect(trigger).toContainText('Desktop version'); await expect(trigger).toContainText('0.2.0');
 await expect(page.locator('.version-trigger .version-tags').getByText('Version mismatch', {exact:true})).toBeVisible();
 await expect(page.locator('.version-trigger .version-tags').getByText('Update available', {exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Update and restart local service',exact:true})).toHaveCount(0);
 await page.locator('.version-trigger .version-tags').getByText('Version mismatch', {exact:true}).click();
 const dialog=page.getByRole('dialog',{name:'Version details',exact:true});
 await expect(dialog.getByRole('region',{name:'Desktop app',exact:true})).toContainText('0.2.0');
 await expect(dialog.getByRole('region',{name:'Local service · Runtime',exact:true})).toContainText('0.1.0');
 await expect(dialog.getByRole('region',{name:'WebUI',exact:true})).toContainText('Updated with the service');
 expect(await page.evaluate(() => (window as any).updateFixture.upgrades)).toBe(0);
 await page.getByRole('button',{name:'Close version details',exact:true}).click();
 await expect(dialog).toHaveCount(0);
 await page.goto('/#/account/general'); await page.goto('/#/account/about');
 expect(await page.evaluate(() => (window as any).updateFixture.upgrades)).toBe(0);
 await page.locator('.version-trigger .version-tags').getByText('Update available', {exact:true}).click();
 await page.getByRole('button', {name:'Update and restart local service', exact:true}).click();
 expect(await page.evaluate(() => (window as any).updateFixture.upgrades)).toBe(1);
 await page.evaluate(() => { const f=(window as any).updateFixture; f.status.local.error='Fixture upgrade failed'; (window as any).emitUpdate(); });
 await expect(page.getByText('Local service action did not complete', {exact:true})).toBeVisible();
 await expect(page.getByRole('button', {name:'Update and restart local service', exact:true})).toBeEnabled();
 state.current.version='0.2.0';
 await page.evaluate(() => { const f=(window as any).updateFixture; f.status.local={...f.status.local,version:'0.2.0',pending:false,error:undefined}; (window as any).emitUpdate(); });
 await reopenVersionDetails(page);
 await expect(page.getByRole('button', {name:'Update and restart local service',exact:true})).toHaveCount(0);
 await expect(dialog.getByRole('region',{name:'Local service · Runtime',exact:true})).toContainText('0.2.0');
 await page.getByRole('button',{name:'Close version details',exact:true}).click();
 await expect(dialog).toHaveCount(0);
 await expect(trigger).toContainText('Desktop version'); await expect(trigger).not.toContainText('Molr');
});
test('remote connection keeps desktop and service updates independent in the dialog', async ({ page }) => {
 const state=await desktopFixture(page, 'remote', false);
 state.current.version='0.4.0';
 await page.getByRole('button',{name:'View version details',exact:true}).click();
 await reopenVersionDetails(page);
 await expect(page.getByRole('region',{name:'Remote service · Runtime',exact:true})).toContainText('0.4.0');
 await expect(page.getByText('Desktop update available', {exact:true})).toBeVisible();
 await expect(page.locator('.release-version').first()).toContainText('v0.3.0');
 await expect(page.getByRole('button', {name:'Update and restart local service',exact:true})).toHaveCount(0);
 await page.getByRole('button',{name:'Download desktop update',exact:true}).click();
 expect(await page.evaluate(() => (window as any).updateFixture.downloads)).toBe(1);
 await page.getByRole('button',{name:'Close version details',exact:true}).click();
 await expect(page.getByRole('dialog')).toHaveCount(0);
 await expect(page.getByRole('button',{name:'View version details',exact:true})).toContainText('0.2.0');
});
test('checking updates respects independent cooldowns and does not let one source block the other', async ({ page }) => {
 const state=await desktopFixture(page,'remote',false); let serviceChecks=0;
 state.source_configured=true; state.supported=true; state.check.next_allowed_at=new Date(Date.now()+60000).toISOString();
 await page.route('**/api/updates/check',route=>{serviceChecks++;return route.fulfill({json:state});});
 await page.getByRole('button',{name:'View version details',exact:true}).click();
 await reopenVersionDetails(page);
 await expect(page.getByText(/Next check available/)).toHaveCount(0);
 await page.getByRole('button',{name:'Check for new versions now',exact:true}).click();
 await expect.poll(()=>page.evaluate(()=>(window as any).updateFixture.checks)).toBe(1);
 expect(serviceChecks).toBe(0);
 await page.evaluate(()=>{const f=(window as any).updateFixture;f.status.checking=true;(window as any).emitUpdate();});
 await expect(page.getByRole('button',{name:'Checking for new versions…',exact:true})).toBeDisabled();
 await page.evaluate(()=>{const f=(window as any).updateFixture;f.status.checking=false;f.status.nextAllowedAt=new Date(Date.now()+4500).toISOString();(window as any).emitUpdate();});
 const countdown=page.getByRole('button',{name:/^Check again in \d+s$/});
 await expect(countdown).toBeDisabled();
 const initial=Number((await countdown.innerText()).match(/\d+/)![0]);
 await expect.poll(async()=>Number((await countdown.innerText()).match(/\d+/)![0])).toBeLessThan(initial);
 await expect(page.getByRole('button',{name:'Check for new versions now',exact:true})).toBeEnabled({timeout:10000});
 expect(serviceChecks).toBe(0);
 expect(await page.evaluate(()=>(window as any).updateFixture.checks)).toBe(1);
 await page.getByRole('button',{name:'Check for new versions now',exact:true}).click();
 await expect.poll(()=>page.evaluate(()=>(window as any).updateFixture.checks)).toBe(2);
 expect(serviceChecks).toBe(0);
});
test('service upgrade progress returns after closing and reopening version details', async ({ page }) => {
 await page.addInitScript(() => { Object.defineProperty(crypto, 'randomUUID', { value: undefined, configurable: true }); });
 await login(page);
 const state:UpdateStatus=await (await page.request.get('/api/updates')).json();
 state.source_configured=true;state.supported=true;state.comparison='available';state.latest={version:'0.3.0',commit:'a'.repeat(40),published_at:'2026-10-20T00:00:00Z',notes_url:'https://example.com/releases',file:'fixture.tar.gz',sha256:'a'.repeat(64)};
 let installationRequests=0;
 await page.route(/\/api\/updates\/installation(?:\?.*)?$/,route=>{installationRequests++;return route.fulfill({json:null});});
 const task={id:'b'.repeat(32),operation:'update',target:'0.3.0',state:'running',stage:'downloading',downloaded:50,total:100,error_code:''};let started=false;
 await page.route(/\/api\/updates(?:\?.*)?$/,route=>route.fulfill({json:state}));
 await page.route(/\/api\/maintenance(?:\?.*)?$/,route=>route.fulfill({json:{can_update:true,...(started?{task}:{})}}));
 await page.route('**/api/updates/apply',route=>{expect(route.request().postDataJSON().version).toBe('0.3.0');expect(route.request().postDataJSON().idempotency_key).toMatch(/^[a-f0-9]{32}$/);started=true;return route.fulfill({status:202,json:task});});
 await page.route(`**/api/maintenance/tasks/${task.id}`,route=>route.fulfill({json:task}));
 await page.goto('/#/account/about');await page.getByRole('button',{name:'View version details',exact:true}).click();
 await expect(page.getByRole('button',{name:'Manual upgrade instructions',exact:true})).toHaveCount(0);
 expect(installationRequests).toBe(0);
 await page.getByRole('button',{name:'Update and restart service',exact:true}).click();
 await expect(page.getByRole('progressbar',{name:'Downloading'})).toHaveAttribute('aria-valuenow','50');
 await page.getByRole('button',{name:'Close version details',exact:true}).click();await expect(page.getByRole('dialog')).toHaveCount(0);
 await page.getByRole('button',{name:'View version details',exact:true}).click();
 await expect(page.getByRole('progressbar',{name:'Downloading'})).toHaveAttribute('aria-valuenow','50');
});

test('manual upgrade is exclusive and loads instructions only when opened', async ({ page }) => {
 await login(page);
 const state:UpdateStatus=await (await page.request.get('/api/updates')).json();
 state.source_configured=true;state.supported=true;state.comparison='available';state.latest={version:'0.3.0',commit:'a'.repeat(40),published_at:'2026-10-20T00:00:00Z',notes_url:'https://example.com/releases',file:'fixture.tar.gz',sha256:'a'.repeat(64)};
 let installationRequests=0, hasRecord=false;
 await page.route(/\/api\/updates(?:\?.*)?$/,route=>route.fulfill({json:state}));
 await page.route(/\/api\/maintenance(?:\?.*)?$/,route=>route.fulfill({json:{can_update:false}}));
 await page.route(/\/api\/updates\/installation(?:\?.*)?$/,route=>{installationRequests++;return route.fulfill({json:hasRecord?{name:'fixture',system:false,setup_path:'/example/bin/molr-setup',source:'https://example.com/releases/stable'}:null});});
 await page.goto('/#/account/about');await page.getByRole('button',{name:'View version details',exact:true}).click();
 await expect(page.getByRole('button',{name:'Manual upgrade instructions',exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Update and restart service',exact:true})).toHaveCount(0);
 expect(installationRequests).toBe(0);
 await page.getByRole('button',{name:'Manual upgrade instructions',exact:true}).click();
 await expect(page.getByRole('link',{name:'View manual upgrade steps',exact:true})).toBeVisible();
 await expect(page.locator('.update-guide pre')).toHaveCount(0);
 await expect(page.getByText('Back up your data before upgrading. The service will restart and running tasks will be interrupted.',{exact:true})).toHaveCount(1);
 expect(installationRequests).toBe(1);
 hasRecord=true;
 await page.reload();await page.getByRole('button',{name:'View version details',exact:true}).click();
 await page.getByRole('button',{name:'Manual upgrade instructions',exact:true}).click();
 await expect(page.locator('.update-guide pre')).toHaveText("'/example/bin/molr-setup' update --version '0.3.0'");
 await expect(page.getByRole('link',{name:'View manual upgrade steps',exact:true})).toHaveCount(0);
 await expect(page.getByRole('link',{name:'Deployment and backup guide',exact:true})).toBeVisible();
 await expect(page.getByText('Using the original installation account, run in the server terminal:',{exact:true})).toBeVisible();
 await expect(page.getByText('Back up your data before upgrading. The service will restart and running tasks will be interrupted.',{exact:true})).toHaveCount(1);
});
test('unavailable upgrade capability can be retried without offering manual upgrade', async ({ page }) => {
 await login(page);
 const state:UpdateStatus=await (await page.request.get('/api/updates')).json();
 state.source_configured=true;state.supported=true;state.comparison='available';state.latest={version:'0.3.0',commit:'a'.repeat(40),published_at:'2026-10-20T00:00:00Z',notes_url:'https://example.com/releases',file:'fixture.tar.gz',sha256:'a'.repeat(64)};
 let unavailable=true;
 await page.route(/\/api\/updates(?:\?.*)?$/,route=>route.fulfill({json:state}));
 await page.route(/\/api\/maintenance(?:\?.*)?$/,route=>unavailable?route.abort('failed'):route.fulfill({json:{can_update:true}}));
 await page.goto('/#/account/about');await page.getByRole('button',{name:'View version details',exact:true}).click();
 await expect(page.getByText('Unable to determine upgrade options',{exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Manual upgrade instructions',exact:true})).toHaveCount(0);
 unavailable=false;await page.getByRole('button',{name:'Retry',exact:true}).click();
 await expect(page.getByRole('button',{name:'Update and restart service',exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Manual upgrade instructions',exact:true})).toHaveCount(0);
});

test('members see update availability without administrator upgrade controls', async ({ page }) => {
 await login(page);
 const principal=await (await page.request.get('/api/auth/me')).json(); principal.User.Administrator=false;
 const state:UpdateStatus=await (await page.request.get('/api/updates')).json();
 state.source_configured=true;state.supported=true;state.comparison='available';state.latest={version:'0.3.0',commit:'a'.repeat(40),published_at:'2026-10-20T00:00:00Z',notes_url:'https://example.com/releases',file:'fixture.tar.gz',sha256:'a'.repeat(64)};
 let administrativeRequests=0;
 await page.route('**/api/auth/me',async route=>route.fulfill({response:await route.fetch(),json:principal}));
 await page.route(/\/api\/updates(?:\?.*)?$/,route=>route.fulfill({json:state}));
 await page.route(/\/api\/(maintenance|updates\/installation)$/,route=>{administrativeRequests++;return route.fulfill({json:{can_update:true}});});
 await page.goto('/#/account/about'); await page.reload();
 await page.locator('.version-trigger .version-tags').getByText('Update available', {exact:true}).click();
 await expect(page.getByText('Contact your administrator to upgrade.',{exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Update and restart service',exact:true})).toHaveCount(0);
 await expect(page.getByRole('button',{name:'Manual upgrade instructions',exact:true})).toHaveCount(0);
 expect(administrativeRequests).toBe(0);
});


test('container updates show the published image and Docker guide without native installer actions', async ({ page }) => {
 await login(page);
 const state: UpdateStatus = await (await page.request.get('/api/updates')).json();
 state.current = { ...state.current, distribution: 'container', container_image: 'ghcr.io/example/molr' };
 state.source_configured = true; state.supported = true; state.comparison = 'available';
 state.latest = { version: '0.3.0', commit: 'a'.repeat(40), published_at: '2026-10-24T00:00:00Z', notes_url: 'https://github.com/example/molr/releases/tag/v0.3.0', file: '', sha256: '', image: 'ghcr.io/example/molr:latest', digest: 'sha256:' + 'b'.repeat(64) };
 let nativeReads = 0;
 await page.route(/\/api\/updates(?:\?.*)?$/, route => route.fulfill({ json: state }));
 await page.route('**/api/updates/check', route => route.fulfill({ json: state }));
 await page.route(/\/api\/maintenance(?:\?.*)?$/, route => route.fulfill({ json: { can_update: false } }));
 await page.route(/\/api\/updates\/installation(?:\?.*)?$/, route => { nativeReads++; return route.fulfill({ json: null }); });
 await page.goto('/#/account/about');
 await page.getByRole('button', { name: 'View version details', exact: true }).click();
 await page.getByRole('button', { name: 'Manual upgrade instructions', exact: true }).click();
 await expect(page.locator('.update-guide pre')).toHaveText('ghcr.io/example/molr:latest');
 await expect(page.getByRole('link', { name: 'View manual upgrade steps', exact: true })).toHaveAttribute('href', 'https://molr.ai/en/docs/admin/deployment/docker/');
 await expect(page.getByRole('button', { name: 'Update and restart service', exact: true })).toHaveCount(0);
 expect(nativeReads).toBe(0);
});

test('service recommendations identify stable releases and never offer a downgrade or prerelease fallback', async ({ page }) => {
 await login(page);
 const state: UpdateStatus = await (await page.request.get('/api/updates')).json();
 state.source_configured = true; state.supported = true; state.auto_check = false;
 state.current = { ...state.current, distribution: 'container', container_image: 'ghcr.io/example/molr' };
 const stable = { version: '0.1.0', commit: 'a'.repeat(40), published_at: '2026-10-24T00:00:00Z', notes_url: 'https://example.com/releases/v0.1.0', file: '', sha256: '', image: 'ghcr.io/example/molr:latest', digest: 'sha256:' + 'b'.repeat(64) };
 await page.route(/\/api\/updates(?:\?.*)?$/, route => route.fulfill({ json: state }));
 await page.route('**/api/updates/check', route => route.fulfill({ json: state }));
 await page.route(/\/api\/maintenance(?:\?.*)?$/, route => route.fulfill({ json: { can_update: false } }));
 const cases: Array<{ version: string; comparison: UpdateStatus['comparison']; title: string }> = [
  { version: '0.1.0-0.nightly.20261024.145333.g8ef1ffd4ea76', comparison: 'available', title: 'Service update available' },
  { version: '0.1.0-rc.1', comparison: 'available', title: 'Service update available' },
  { version: '0.1.0', comparison: 'current', title: 'Service is up to date with the stable release' },
  { version: '0.1.1-0.nightly.20261025.0.gaaaaaaa', comparison: 'ahead', title: 'This build is newer than the latest stable release' },
  { version: '0.1.1-0.nightly.20261025.0.gaaaaaaa', comparison: 'unavailable', title: 'No stable service release for this platform' },
 ];
 for (const value of cases) {
  state.current.version = value.version; state.comparison = value.comparison;
  state.latest = value.comparison === 'unavailable' ? null : stable;
  await page.goto('/#/account/general');
  await page.goto('/#/account/about');
  await page.getByRole('button', { name: 'View version details', exact: true }).click();
  await expect(page.getByText(value.title, { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Manual upgrade instructions', exact: true })).toHaveCount(value.comparison === 'available' ? 1 : 0);
  if (value.comparison === 'available') await expect(page.locator('.release-version strong')).toHaveText('v0.1.0');
  else await expect(page.locator('.release-version')).toHaveCount(0);
  await expect(page.getByText('Unable to check for updates', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'Close version details', exact: true }).click();
 }
});
