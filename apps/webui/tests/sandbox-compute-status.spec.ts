import { test, expect } from '@playwright/test';

test('compute status separates disabled, controlled execution, full access and offline snapshots', async ({ page }) => {
  await page.route(url => url.pathname === '/api/assistants', route => route.fulfill({ json: { Items: [{ ID: 'compute-status-assistant', Name: 'Compute status assistant', State: 'active', Version: 1 }], HasMore: false } }));
  let state = 'available', enabled = true, shell = false, fullAccess = true, reason = 'sandbox_helper_unavailable';
  await page.route(url => url.pathname === '/api/sandboxes', async route => {
    const response = await route.fetch();
    const nodes = await response.json();
    for (const node of nodes) {
      node.State = state;
      node.Info.Enabled = enabled;
      Object.assign(node.Info.Capabilities, { Shell: shell, FullAccess: fullAccess, Reason: reason });
    }
    await route.fulfill({ response, json: nodes });
  });
  await page.goto('/#/resources');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('tab', { name: /^(Sandboxes|沙盒)$/ }).click();
  await page.getByRole('tab', { name: /^(Compute|计算)$/ }).click();
  const overview = page.locator('.sandbox-capabilities'), summary = page.locator('.sandbox-compute-summary');
  const refresh = () => page.getByRole('button', { name: /^(Check again|重新检查)$/ }).click();
  await expect(overview).toContainText('Controlled compute unavailable');
  await expect(overview).toContainText('Full access available');
  await expect(summary).toContainText('storage, compute and network');
  await expect(page.locator('.sandbox-resource-heading')).toContainText('Unavailable');
  await page.getByRole('button', { name: 'Grant assistant access', exact: true }).click();
  await expect(page.getByRole('dialog').getByRole('button', { name: 'Grant assistant access', exact: true })).toBeDisabled();
  await page.keyboard.press('Escape');
  await page.getByRole('tab', { name: /^(Full access|完全访问)$/ }).click();
  await page.getByRole('button', { name: 'Grant assistant access', exact: true }).click();
  await expect(page.getByRole('dialog').getByRole('button', { name: 'Grant assistant access', exact: true })).toBeEnabled();
  await page.keyboard.press('Escape');
  await page.getByRole('tab', { name: /^(Compute|计算)$/ }).click();
  fullAccess = false;
  await refresh();
  await expect(summary).not.toContainText('storage, compute and network');
  await expect(overview).not.toContainText('Full access available');
  enabled = false; reason = 'sandbox_disabled';
  await refresh();
  await expect(summary).toContainText('Command execution disabled');
  await expect(summary).toContainText('Enable command execution');
  await expect(page.locator('.sandbox-section')).not.toContainText('Expose directories or networks');
  enabled = true; shell = true; reason = 'available'; fullAccess = true;
  await refresh();
  await expect(overview).toContainText('Controlled compute available');
  await expect(summary).toContainText('Requestable compute capacity');
  state = 'offline';
  await refresh();
  await expect(summary).toContainText('Compute node offline');
  await expect(summary).not.toContainText('Isolation checks passed');
  await expect(overview).not.toContainText('Full access available');
  state = 'disconnected';
  await refresh();
  await expect(summary).toContainText('Compute node disconnected');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test('empty compute uses execution guidance and storage/network have specific deployment guidance', async ({ page }) => {
  let shell = false;
  await page.route(url => url.pathname === '/api/resources', async route => {
    const response = await route.fetch(), data = await response.json();
    await route.fulfill({ response, json: Array.isArray(data) ? [] : { ...data, Items: [] } });
  });
  await page.route(url => url.pathname === '/api/sandboxes', async route => {
    const response = await route.fetch(), nodes = await response.json();
    for (const node of nodes) {
      node.State = 'available';
      Object.assign(node.Info, { Enabled: true, Directories: [], Networks: [], AllowDirectoryPaths: false });
      Object.assign(node.Info.Capabilities, { Shell: shell, FullAccess: true, Reason: shell ? 'available' : 'sandbox_helper_unavailable' });
    }
    await route.fulfill({ response, json: nodes });
  });
  await page.goto('/#/resources');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('tab', { name: /^(Sandboxes|沙盒)$/ }).click();
  await page.getByRole('tab', { name: /^(Compute|计算)$/ }).click();
  await expect(page.locator('.sandbox-section')).toContainText('Controlled compute unavailable');
  await expect(page.locator('.sandbox-section')).not.toContainText('No networks exposed');
  await expect(page.getByRole('button', { name: 'Connect compute', exact: true })).toHaveCount(0);
  shell = true;
  await page.getByRole('button', { name: /^(Check again|重新检查)$/ }).click();
  await expect(page.locator('.sandbox-empty')).toContainText('Compute not connected');
  await expect(page.getByRole('button', { name: 'Connect compute', exact: true })).toBeEnabled();
  await page.getByRole('tab', { name: /^(Storage|存储)$/ }).click();
  await expect(page.locator('.sandbox-empty')).toContainText('No directories exposed');
  await page.getByRole('tab', { name: /^(Network|网络)$/ }).click();
  await expect(page.locator('.sandbox-empty')).toContainText('No networks exposed');
});
