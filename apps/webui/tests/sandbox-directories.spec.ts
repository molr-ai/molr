import { test, expect } from '@playwright/test';
import { mkdtemp, realpath, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

test('connect an unrestricted local directory and keep assistant authorization separate', async ({ page }, info) => {
  const directory = await realpath(await mkdtemp(join(tmpdir(), 'molr-directory-ui-')));
  try {
    await page.goto('/#/resources');
    await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
    await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await page.getByRole('tab', { name: 'Sandboxes', exact: true }).click();
    await page.getByRole('tab', { name: 'Storage', exact: true }).click();
    const grants = await (await page.request.get('/api/grants')).json();
    await page.getByRole('button', { name: 'Connect directory', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Connect directory', exact: true });
    const name = `Directory ${info.project.name} ${Date.now()}`;
    await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
    await expect(dialog.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
    await dialog.getByRole('textbox', { name: 'Directory path', exact: true }).fill(directory);
    await dialog.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByRole('dialog', { name: `Manage access · ${name}`, exact: true })).toBeVisible();
    await page.keyboard.press('Escape');
    const row = page.locator('.sandbox-resource').filter({ has: page.getByRole('heading', { name, exact: true }) });
    await expect(row).toContainText(directory);
    await expect(row).toContainText('No assistants authorized');
    expect(await (await page.request.get('/api/grants')).json()).toEqual(grants);
    const resources = await (await page.request.get('/api/sandboxes/local/resources')).json();
    expect(resources.some((r: { Path: string }) => r.Path === directory)).toBe(true);
  } finally { await rm(directory, { recursive: true, force: true }); }
});
