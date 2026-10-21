import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

async function fixture(page: Page, os = 'linux', enabled = true) {
  await page.route('**/api/auth/me', route => route.fulfill({ headers: { 'X-Molr-CSRF-Cookie': 'molr_dev_csrf' }, json: {} }));
  await page.route('**/api/sandbox*', route => route.fulfill({ json: { Enabled: enabled, Capabilities: { OS: os, Shell: enabled } } }));
  await page.route('**/api/resources?*', route => route.fulfill({ json: { Items: [{ ID: 'compute', State: 'available', SandboxID: 'local', Name: 'Compute' }] } }));
  await page.route('**/api/grants?*', route => route.fulfill({ json: { Items: [{ ResourceID: 'compute', Scope: {}, Actions: ['execute'] }] } }));
  await page.route('**/api/assistants/terminal-fixture/shell-tasks*', route => route.fulfill({ json: { Items: [] } }));
}

for (const os of ['windows', 'freebsd']) test(`${os} Runtime explains unsupported terminals and cannot submit`, async ({ page }) => {
  await fixture(page, os);
  let creates = 0;
  page.on('request', request => { if (request.method() === 'POST' && request.url().endsWith('/terminals')) creates++; });
  await page.goto('/tests/fixtures/terminal-feedback.html');
  await expect(page.locator('.terminal-empty')).toContainText('This Runtime’s operating system does not support interactive terminals.');
  await expect(page.getByRole('button', { name: 'New session', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Open terminal', exact: true })).toHaveCount(0);
  expect(creates).toBe(0);
});

for (const os of ['linux', 'darwin']) test(`${os} Runtime permits creation independently of client platform`, async ({ page }) => {
  await fixture(page, os);
  await page.addInitScript(() => Object.defineProperty(navigator, 'platform', { value: 'Win32' }));
  await page.goto('/tests/fixtures/terminal-feedback.html');
  await expect(page.getByRole('button', { name: 'Open terminal', exact: true })).toBeEnabled();
});

test('disabled sandbox explains the capability state instead of missing grants', async ({ page }) => {
  await fixture(page, 'linux', false);
  await page.goto('/tests/fixtures/terminal-feedback.html');
  await expect(page.locator('.terminal-empty')).toContainText('Local sandbox command execution is unavailable.');
  await expect(page.getByRole('button', { name: 'New session', exact: true })).toBeDisabled();
});

test('creation errors use Toasts, retain unknown request IDs and display platform reasons', async ({ page }) => {
  await fixture(page);
  const requests: string[] = [];
  await page.route('**/api/assistants/terminal-fixture/terminals', route => {
    requests.push(route.request().postDataJSON().request_id);
    return route.fulfill(requests.length < 3 ? { status: 500, json: { code: 'internal' } } : { status: 412, json: { code: 'failed_precondition', reason: 'terminal_platform_unsupported' } });
  });
  await page.goto('/tests/fixtures/terminal-feedback.html');
  const open = page.getByRole('button', { name: 'Open terminal', exact: true }), toast = page.locator('.ui-toast--danger');
  await open.click();
  await expect(toast).toBeVisible();
  await expect(page.locator('.terminal-panel .ui-alert')).toHaveCount(0);
  await toast.getByRole('button', { name: 'Close', exact: true }).click();
  await open.click(); await expect.poll(() => requests.length).toBe(2); await expect(toast).toBeVisible();
  expect(requests[1]).toBe(requests[0]);
  await open.click();
  await expect(toast).toContainText('This Runtime’s operating system does not support interactive terminals.');
  await open.click(); await expect.poll(() => requests.length).toBe(4);
  expect(requests[3]).not.toBe(requests[2]);
  await page.getByRole('button', { name: 'Close terminal window', exact: true }).click();
  await expect(toast).toHaveCount(0);
});

test('capability lookup failure has a retry Toast and keeps creation disabled until recovery', async ({ page }) => {
  await fixture(page);
  let fail = true;
  await page.route('**/api/sandbox*', route => route.fulfill(fail ? { status: 500, json: { code: 'internal' } } : { json: { Enabled: true, Capabilities: { OS: 'linux', Shell: true } } }));
  await page.goto('/tests/fixtures/terminal-feedback.html');
  const toast = page.locator('.ui-toast--danger');
  await expect(toast).toBeVisible();
  await expect(page.getByRole('button', { name: 'New session', exact: true })).toBeDisabled();
  fail = false;
  await toast.getByRole('button', { name: 'Retry', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Open terminal', exact: true })).toBeEnabled();
  await expect(toast).toHaveCount(0);
});
