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
 await expect(page.getByText('No update source configured', { exact: true })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Check for updates', exact: true })).toBeDisabled();
 await expect(page.getByRole('region', { name: 'Installed builds' })).toContainText('Runtime');
});
test('about reactivation checks releases and keeps build mismatch separate from release status', async ({ page }) => {
 await login(page);
 const runtime: UpdateStatus = await (await page.request.get('/api/updates')).json();
 const frontend = await page.evaluate(async () => { const path = '/src/build-info.ts'; return (await import(path)).buildInfo; });
 const state: UpdateStatus = { ...runtime, auto_check: true, source_configured: true, supported: true, current: { ...runtime.current, version: frontend.version, commit: frontend.commit }, comparison: 'current', latest: { version: frontend.version, commit: frontend.commit, published_at: '2026-10-11T00:00:00Z', notes_url: 'https://example.com/releases', file: 'fixture.tar.gz', sha256: 'a'.repeat(64) } };
 let checks = 0;
 await page.route('**/api/updates', route => route.fulfill({ json: state }));
 await page.route('**/api/updates/check', route => { checks++; return route.fulfill({ json: state }); });
 await page.goto('/#/account/about');
 await expect(page.getByText('WebUI and Runtime builds match', { exact: true })).toBeVisible();
 await expect.poll(() => checks).toBeGreaterThan(0);
 const initialChecks = checks;
 await page.waitForTimeout(800);
 expect(checks).toBe(initialChecks); // Checking must not invalidate its own effect and loop.
 const before = checks;
 await page.goto('/#/account/general');
 await page.goto('/#/account/about');
 await expect.poll(() => checks).toBeGreaterThan(before);
 state.current = { ...state.current, commit: 'b'.repeat(40) };
 await page.getByRole('button', { name: 'Recheck build consistency' }).click();
 await expect(page.getByRole('region', { name: 'Installed builds' })).toContainText('WebUI and Runtime commits differ');
 await expect(page.getByText('Up to date', { exact: true })).toBeVisible();
 state.check.error_code = 'network_error';
 state.comparison = 'available'; state.latest!.version = '0.2.0';
 await page.getByRole('button', { name: 'Recheck build consistency' }).click();
 await expect(page.getByText('Version 0.2.0 is available', { exact: true })).toBeVisible();
 if (await page.locator('.nav-account-actions').isVisible()) await expect(page.locator('.nav-account-actions .update-badge')).toBeVisible();
 await expect(page.getByText('Unable to check for updates', { exact: true })).toBeVisible();
 await page.route('**/api/updates', route => route.abort('failed'));
 await page.getByRole('button', { name: 'Recheck build consistency' }).click();
 await expect(page.getByText('Cannot check consistency while Runtime is unreachable', { exact: true })).toBeVisible();
 await expect(page.getByText('Version 0.2.0 is available', { exact: true })).toBeVisible();
});

test('component maintenance shows progress, survives panel navigation and exposes cancellation', async ({ page }) => {
 await login(page);
 const task = { id: 'a'.repeat(32), operation: 'component_install', component: 'browser', target: 'fixture', state: 'running', stage: 'downloading', downloaded: 50, total: 100, error_code: '' };
 let started = false, cancelled = false;
 await page.route('**/api/components', route => route.fulfill({ json: [{ id: 'browser', state: 'missing', can_install: true, auto_install: started, download_bytes: 100, disk_bytes: 200, task: started ? task : undefined }] }));
 await page.route('**/api/components/browser/install', route => { const q=route.request().postDataJSON(); expect(q.idempotency_key).toMatch(/^[a-f0-9]{32}$/); started=true; return route.fulfill({status:202,json:task}); });
 await page.route(`**/api/maintenance/tasks/${task.id}`, route => route.fulfill({ json: task }));
 await page.route(`**/api/maintenance/tasks/${task.id}/cancel`, route => { cancelled=true;task.state='cancelled';task.error_code='maintenance_cancelled';return route.fulfill({json:task}); });
 await page.goto('/#/account/about');
 await expect(page.getByRole('region', { name: 'Installed builds' })).toBeVisible();
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
