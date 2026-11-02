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
    const path = dialog.getByRole('textbox', { name: 'Directory path', exact: true });
    for (const [input, message] of [
      ['relative/path/', 'Enter an absolute directory path or ~/ path on the sandbox machine'],
      [`${directory}/missing/`, 'The directory does not exist or cannot be accessed'],
    ]) {
      await path.fill(input);
      await expect(path).not.toHaveAttribute('aria-invalid', 'true');
      await dialog.getByRole('button', { name: 'Save', exact: true }).click();
      await expect(dialog.getByText(message, { exact: false })).toBeVisible();
      await expect(path).toHaveValue(input);
      await expect(path).toHaveAttribute('aria-invalid', 'true');
      await expect(page.locator('.ui-toast--danger')).toHaveCount(0);
      await expect(dialog.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue(name);
    }
    await path.fill(`${directory}///`);
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
