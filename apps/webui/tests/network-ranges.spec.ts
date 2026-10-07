import { test, expect } from '@playwright/test';

const port = Number(process.env.MOLR_TEST_SECOND_PORT ?? Number(process.env.MOLR_TEST_PORT ?? process.env.MOLR_E2E_PORT ?? 5193) + 1);
const origin = `http://127.0.0.1:${port}`;

test('network connection displays multiple address and port ranges and persists a deployment-bound resource', async ({ page }, info) => {
  await page.goto(`${origin}/#/resources`);
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('second-owner');
  await page.getByLabel('Password', { exact: true }).fill('second-webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('tab', { name: 'Sandboxes', exact: true }).click();
  await page.getByRole('tab', { name: 'Network', exact: true }).click();
  await page.getByRole('button', { name: 'Add network', exact: true }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Add network', exact: true });
  const name = `Network ${info.project.name} ${Date.now()}`;
  await dialog.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
  await dialog.getByRole('combobox', { name: 'Network target', exact: true }).click();
  const option = page.getByRole('option', { name: /LAN ranges/ });
  await expect(option).toContainText('192.168.1.0/24, 10.20.0.0/16, fd12::/32');
  await expect(option).toContainText('80, 443, 8000-8099, 9000-9099');

  await option.click();
  await dialog.getByText('View detailed network scope', { exact: true }).click();
  await expect(dialog).toContainText('TCP / UDP');
  await expect(dialog).toContainText('192.168.1.10, fd12::dead/128');
  await expect(dialog).toContainText('8080-8089, 9005');
  await expect(dialog).toContainText('Exclusions apply only to this network.');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const access = page.getByRole('dialog', { name: `Manage access · ${name}`, exact: true });
  await expect(access).toBeVisible();
  await page.keyboard.press('Escape');
  const card = page.locator('.sandbox-resource').filter({ has: page.getByRole('heading', { name, exact: true }) });
  await card.getByText('View detailed network scope', { exact: true }).click();
  await expect(card).toContainText('192.168.1.10, fd12::dead/128');
  await expect(card).toContainText('8080-8089, 9005');
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  await page.reload();
  await page.getByRole('tab', { name: 'Sandboxes', exact: true }).click();
  await page.getByRole('tab', { name: 'Network', exact: true }).click();
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  const resources = await (await page.request.get(`${origin}/api/resources?kind=network`)).json();
  expect(resources.Items.find((r: { Name: string }) => r.Name === name)).toMatchObject({ Kind: 'network', Provider: 'sandbox-network', Actions: ['execute'] });
  const csrf = (await page.context().cookies()).find(cookie => cookie.name === `molr_dev_csrf_p${port}`)!.value;
  const headers = { Origin: origin, 'X-CSRF-Token': csrf };
  // Owner resource APIs select presets; they must not expand deployment policy.
  for (const binding of [{ SandboxTarget: 'unconfigured' }, { SandboxTarget: 'LAN ranges', Addresses: ['0.0.0.0/0'], Ports: ['1-65535'] }]) {
    const response = await page.request.post(`${origin}/api/resources`, { headers, data: { Name: 'Invalid range', Kind: 'network', Provider: 'sandbox-network', Binding: binding, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
    expect(response.status()).toBe(400);
  }
});


test('built-in network presets require an explicit connection and no network configuration', async ({ page }, info) => {
  await page.goto(`${origin}/#/resources`);
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('second-owner');
  await page.getByLabel('Password', { exact: true }).fill('second-webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('tab', { name: 'Sandboxes', exact: true }).click();
  await page.getByRole('tab', { name: 'Network', exact: true }).click();
  await page.getByRole('button', { name: 'Add network', exact: true }).first().click();
  const dialog=page.getByRole('dialog', {name:'Add network',exact:true});
  await dialog.getByRole('combobox', {name:'Network target',exact:true}).click();
  await page.getByRole('option', {name:/Internet access/}).click();
  await expect(dialog.getByRole('textbox',{name:'Name',exact:true})).toHaveValue('Internet access');
  await expect(dialog).toContainText('No proxy, interface or route configuration is required.');
  await expect(dialog).toContainText('all TCP / UDP ports');
  await expect(dialog.locator('details')).not.toHaveAttribute('open','');
  const name=`Internet preset ${info.project.name} ${Date.now()}`;
  await dialog.getByRole('textbox',{name:'Name',exact:true}).fill(name);
  await dialog.getByRole('button',{name:'Save',exact:true}).click();
  const access=page.getByRole('dialog',{name:`Manage access · ${name}`,exact:true});
  await expect(access).toBeVisible();
  const resources=await (await page.request.get(`${origin}/api/resources?kind=network`)).json();
  const resource=resources.Items.find((r:{Name:string})=>r.Name===name);
  expect(resource).toBeTruthy();
  const grants=await (await page.request.get(`${origin}/api/grants?resource_id=${resource.ID}`)).json();
  expect(grants.Items).toHaveLength(0);
});
