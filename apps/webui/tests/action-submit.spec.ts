import { test, expect } from '@playwright/test';

test('group form serializes submissions before the busy render commits', async ({ page }, info) => {
  await page.goto('/#/settings/groups');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('button', { name: 'Add group', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Add group', exact: true });
  const name = `Serialized submit ${info.project.name}-${Date.now()}`;
  await dialog.getByRole('textbox').first().fill(name);
  let requests = 0, release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/admin/groups', async route => {
    if (route.request().method() !== 'POST') { await route.continue(); return; }
    requests++; await gate;
    await route.continue();
  });
  try {
    await dialog.locator('form').evaluate((form: HTMLFormElement) => { form.requestSubmit(); form.requestSubmit(); });
    await expect(dialog.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
    await expect.poll(() => requests).toBeGreaterThan(0);
    release(); await expect(dialog).not.toBeVisible();
    expect(requests).toBe(1);
    const groups = await (await page.request.get('/api/admin/groups')).json();
    expect(groups.filter((group: { Name: string }) => group.Name === name)).toHaveLength(1);
  } finally { release(); }
});

test('failed submissions release the action lock and allow a later save', async ({ page }, info) => {
  await page.goto('/#/settings/groups');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('button', { name: 'Add group', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Add group', exact: true });
  const name = `Retry submit ${info.project.name}-${Date.now()}`;
  await dialog.getByRole('textbox').first().fill(name);
  let requests = 0;
  await page.route('**/api/admin/groups', async route => {
    if (route.request().method() !== 'POST') { await route.continue(); return; }
    if (++requests === 1) await route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ code: 'forbidden' }) });
    else await route.continue();
  });
  await dialog.locator('form').evaluate((form: HTMLFormElement) => { form.requestSubmit(); form.requestSubmit(); });
  await expect(dialog.getByRole('alert')).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Save', exact: true })).toBeEnabled();
  expect(requests).toBe(1);
  await expect(dialog.getByRole('textbox').first()).toHaveValue(name);
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).not.toBeVisible(); expect(requests).toBe(2);
  const groups = await (await page.request.get('/api/admin/groups')).json();
  expect(groups.filter((group: { Name: string }) => group.Name === name)).toHaveLength(1);
});

test('canceling identity verification releases its parent action and nested actions remain independent', async ({ page }, info) => {
  await page.goto('/#/account/security');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('button', { name: 'Create login link', exact: true }).click();
  const parent = page.getByRole('dialog', { name: 'Create login link', exact: true });
  await parent.getByLabel('Name', { exact: true }).fill(`Canceled action ${info.project.name}`);
  await parent.getByRole('button', { name: 'Save', exact: true }).click();
  const verification = page.getByRole('dialog', { name: 'Verify identity', exact: true });
  await verification.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(verification).not.toBeVisible();
  await expect(parent.getByRole('button', { name: 'Save', exact: true })).toBeEnabled();
  await expect(parent.getByRole('alert')).toHaveCount(0);
  await parent.getByRole('button', { name: 'Save', exact: true }).click();
  await verification.getByLabel('Runtime sign-in password', { exact: true }).fill('webui-e2e-password-2026!');
  // The parent's action is pending while this separate hook performs verification.
  await verification.getByRole('button', { name: 'Verify', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Login link created', exact: true })).toBeVisible();
});
