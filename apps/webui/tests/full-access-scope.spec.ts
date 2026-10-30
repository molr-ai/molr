import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
async function setup(page: Page) {
  const grants: any[] = [];
  await page.route('**/api/auth/me', r => r.fulfill({ headers: { 'X-Molr-CSRF-Cookie': 'molr_dev_csrf' }, json: {} }));
  await page.route('**/api/models', r => r.fulfill({ json: [] }));
  await page.route('**/api/sandboxes', r => r.fulfill({ json: [{ ID: 'local', Name: 'Local', Local: true, State: 'available', Info: { Enabled: true, Capabilities: { FullAccess: true, Shell: true } } }] }));
  await page.route('**/api/grants', r => { grants.push(r.request().postDataJSON()); return r.fulfill({ json: {} }); });
  await page.goto('/tests/fixtures/full-access-scope.html');
  return { grants, dialog: page.getByRole('dialog') };
}
test('Full Access requires explicit selection and preserves exact argument boundaries', async ({ page }) => {
  const { grants, dialog } = await setup(page);
  const save = dialog.getByRole('button', { name: 'Save', exact: true });
  await expect(save).toBeDisabled();
  await expect(dialog.getByRole('radio', { name: 'Unrestricted', exact: true })).not.toBeChecked();
  await dialog.getByRole('radio', { name: 'Specified programs', exact: true }).click();
  await expect(save).toBeDisabled();
  await dialog.getByLabel('Program path 1', { exact: true }).fill('/usr/bin/printf');
  await dialog.getByRole('button', { name: 'Add argument', exact: true }).click();
  await dialog.getByLabel('Argument 1', { exact: true }).fill(' a b ');
  await dialog.getByRole('button', { name: 'Add argument', exact: true }).click();
  await save.click();
  await expect.poll(() => grants.length).toBe(1);
  expect(grants[0].Scope).toMatchObject({ FullAccess: true, FullAccessScope: { mode: 'programs', programs: [{ executable: '/usr/bin/printf', arguments: 'exact', args: [' a b ', ''] }] } });
  await dialog.getByRole('radio', { name: 'Any arguments', exact: true }).click();
  await save.click();
  await expect.poll(() => grants.length).toBe(2);
  expect(grants[1].Scope.FullAccessScope.programs[0]).toMatchObject({ arguments: 'any', args: [] });
  await dialog.getByRole('button', { name: 'Remove program', exact: true }).click();
  await expect(save).toBeDisabled();
});
test('Unrestricted access is submitted only after explicit choice', async ({ page }) => {
  const { grants, dialog } = await setup(page);
  await dialog.getByRole('radio', { name: 'Unrestricted', exact: true }).click();
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect.poll(() => grants.length).toBe(1);
  expect(grants[0].Scope.FullAccessScope).toEqual({ mode: 'unrestricted' });
});

test('one use is optional and expiry defaults to a bounded date', async ({ page }) => {
 const {grants,dialog}=await setup(page);
 await dialog.getByRole('radio',{name:'Specified programs',exact:true}).click();
 await dialog.getByLabel('Program path 1',{exact:true}).fill('/usr/bin/uptime');
 const once=dialog.getByRole('switch',{name:'Use once',exact:true});
 await expect(once).not.toBeChecked();
 await once.check();
 await dialog.getByRole('button',{name:'Save',exact:true}).click();
 await expect.poll(()=>grants.length).toBe(1);
 expect(grants[0].Scope.Once).toBe(true);
 expect(Date.parse(grants[0].ExpiresAt)).toBeGreaterThan(Date.now());
});
