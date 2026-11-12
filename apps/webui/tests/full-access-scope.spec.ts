import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
async function setup(page: Page) {
  const grants: any[] = [];
  await page.route('**/api/auth/me', r => r.fulfill({ headers: { 'X-Molr-CSRF-Cookie': 'molr_dev_csrf' }, json: {} }));
  await page.route('**/api/models', r => r.fulfill({ json: [] }));
  await page.route('**/api/sandboxes', r => r.fulfill({ json: [{ ID: 'local', Name: 'Local', Local: true, State: 'available', Info: { Enabled: true, Capabilities: { FullAccess: true, Shell: true } } }] }));
  await page.route('**/api/sandboxes/*/programs?*', r => r.fulfill({ json: { Programs: [], CheckedAt: new Date().toISOString() } }));
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

test('discovered programs fill exact paths without granting arbitrary arguments; idle does not rescan', async ({ page }) => {
  const { grants, dialog } = await setup(page);
  const calls: string[] = [];
  const executable = '/opt/example agents/codex';
  await page.route('**/api/sandboxes/local/programs?*', route => {
    calls.push(route.request().url());
    return route.fulfill({ json: { Programs: [{ Name: 'Codex', Command: 'codex', Category: 'agent', Executable: executable, ResolvedPath: executable, Source: 'path', Available: true, Reason: '' }, { Name: 'Windows launcher', Command: 'claude', Category: 'agent', Executable: 'C:\\Tools\\claude.cmd', ResolvedPath: 'C:\\Tools\\claude.cmd', Source: 'path', Available: false, Reason: 'launcher_unsupported' }], CheckedAt: new Date().toISOString() } });
  });
  await dialog.getByRole('radio', { name: 'Specified programs', exact: true }).click();
  expect(calls).toHaveLength(0);
  const pathBounds = (await dialog.getByLabel('Program path 1', { exact: true }).boundingBox())!;
  const pickerBounds = (await dialog.getByRole('button', { name: 'Choose a discovered program' }).boundingBox())!;
  expect(pickerBounds.x).toBeGreaterThanOrEqual(pathBounds.x + pathBounds.width);
  expect(Math.abs(pickerBounds.y + pickerBounds.height / 2 - pathBounds.y - pathBounds.height / 2)).toBeLessThan(1);
  expect(Math.abs(pickerBounds.height - pathBounds.height)).toBeLessThan(1);
  await expect(dialog.getByRole('button', { name: 'Choose a discovered program' })).toHaveText('');
  await expect(dialog.getByRole('button', { name: 'Choose a discovered program' }).locator('svg')).toHaveCount(1);
  await dialog.getByRole('button', { name: 'Choose a discovered program' }).hover();
  const tooltip = page.getByRole('tooltip', { name: 'Choose a discovered program', exact: true });
  await expect(tooltip).toBeVisible();
  await expect(tooltip).toHaveAttribute('data-side', 'top');
  const tipBounds = (await tooltip.boundingBox())!;
  const currentButton = (await dialog.getByRole('button', { name: 'Choose a discovered program' }).boundingBox())!;
  const centeredLeft = currentButton.x + (currentButton.width - tipBounds.width) / 2;
  const expectedLeft = Math.max(8, Math.min(centeredLeft, page.viewportSize()!.width - tipBounds.width - 8));
  expect(Math.abs(tipBounds.x - expectedLeft)).toBeLessThan(1);
  expect(tipBounds.y + tipBounds.height).toBeLessThan(currentButton.y);
  await dialog.getByRole('button', { name: 'Choose a discovered program' }).click();
  const picker = page.getByRole('dialog', { name: 'Choose a host program' });
  await expect(picker.getByRole('button', { name: `Select Codex · ${executable}`, exact: true })).toBeEnabled();
  await expect(picker.getByRole('button', { name: /Select Windows launcher/ })).toBeDisabled();
  await picker.locator('summary').filter({ hasText: 'Codex' }).click();
  for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) {
    for (const theme of ['light', 'dark']) {
      await page.evaluate(({ palette, theme }) => { document.documentElement.dataset.palette = palette; document.documentElement.dataset.theme = theme; }, { palette, theme });
      expect(await picker.locator('.host-programs').evaluate(node => node.scrollWidth <= node.clientWidth + 1), `${palette}/${theme}`).toBe(true);
    }
  }
  await page.clock.install();
  await page.clock.fastForward(600_000);
  expect(calls).toHaveLength(1);
  await picker.getByRole('button', { name: 'Scan again', exact: true }).click();
  await expect.poll(() => calls.length).toBe(2);
  expect(new URL(calls[1]).searchParams.get('refresh')).toBe('true');
  await picker.getByRole('button', { name: `Select Codex · ${executable}`, exact: true }).click();
  await expect(dialog.getByLabel('Program path 1', { exact: true })).toHaveValue(executable);
  await expect(dialog.getByRole('radio', { name: 'Exact arguments', exact: true })).toBeChecked();
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect.poll(() => grants.length).toBe(1);
  expect(grants[0].Scope.FullAccessScope).toEqual({ mode: 'programs', programs: [{ executable, arguments: 'exact', args: [] }] });
  await dialog.getByLabel('Program path 1', { exact: true }).fill('/opt/keep-first');
  await dialog.getByRole('button', { name: 'Add program', exact: true }).click();
  const second = dialog.locator('.full-access-program-input').nth(1).locator('..');
  await second.getByRole('button', { name: 'Add argument', exact: true }).click();
  await second.getByLabel('Argument 1', { exact: true }).fill('--version');
  await second.getByRole('button', { name: 'Choose a discovered program' }).click();
  await picker.getByRole('button', { name: `Select Codex · ${executable}`, exact: true }).click();
  await expect(dialog.getByLabel('Program path 1', { exact: true })).toHaveValue('/opt/keep-first');
  await expect(dialog.getByLabel('Program path 2', { exact: true })).toHaveValue(executable);
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect.poll(() => grants.length).toBe(2);
  expect(grants[1].Scope.FullAccessScope.programs[1]).toEqual({ executable, arguments: 'exact', args: ['--version'] });
});

test('switching nodes clears selected programs and discovers on the selected node', async ({ page }) => {
  const { grants } = await setup(page);
  await page.route('**/api/sandboxes', route => route.fulfill({ json: ['local', 'remote'].map(ID => ({ ID, Name: ID === 'local' ? 'Local' : 'Remote node', Local: ID === 'local', State: 'available', Info: { Enabled: true, Capabilities: { FullAccess: true, Shell: true } } })) }));
  const nodes: string[] = [];
  await page.route('**/api/sandboxes/*/programs?*', route => {
    const node = new URL(route.request().url()).pathname.split('/')[3]; nodes.push(node);
    return route.fulfill({ json: { Programs: [{ Name: 'Codex', Command: 'codex', Category: 'agent', Executable: `/${node}/bin/codex`, ResolvedPath: `/${node}/bin/codex`, Source: 'path', Available: true }], CheckedAt: new Date().toISOString() } });
  });
  await page.goto('/tests/fixtures/full-access-scope.html?nodes=1');
  const dialog = page.getByRole('dialog').first();
  await dialog.getByRole('radio', { name: 'Specified programs', exact: true }).click();
  await dialog.getByRole('button', { name: 'Choose a discovered program' }).click();
  await page.getByRole('button', { name: 'Select Codex · /local/bin/codex', exact: true }).click();
  await dialog.getByRole('combobox', { name: 'Sandbox', exact: true }).click();
  await page.getByRole('option', { name: 'Remote node', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  await expect(dialog.getByLabel('Program path 1', { exact: true })).toHaveCount(0);
  await dialog.getByRole('radio', { name: 'Specified programs', exact: true }).click();
  await dialog.getByRole('button', { name: 'Choose a discovered program' }).click();
  await page.getByRole('button', { name: 'Select Codex · /remote/bin/codex', exact: true }).click();
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect.poll(() => grants.length).toBe(1);
  expect(grants[0].ResourceID).toBe('remote-compute');
  expect(grants[0].Scope.FullAccessScope.programs[0].executable).toBe('/remote/bin/codex');
  expect(nodes).toEqual(['local', 'remote']);
});

test('host command lookup debounces typing, skips composition and cancels when closed', async ({ page }) => {
  const { dialog } = await setup(page);
  const commands: string[] = [];
  await page.route('**/api/sandboxes/local/programs?*', route => {
    commands.push(new URL(route.request().url()).searchParams.get('command') ?? '');
    return route.fulfill({ json: { Programs: [], CheckedAt: new Date().toISOString() } });
  });
  await dialog.getByRole('radio', { name: 'Specified programs', exact: true }).click();
  await dialog.getByRole('button', { name: 'Choose a discovered program' }).click();
  const picker = page.getByRole('dialog', { name: 'Choose a host program' });
  const input = picker.getByLabel('Find by command name');
  await expect(picker.getByRole('button', { name: 'Scan again', exact: true })).toBeEnabled();
  await expect(picker.getByRole('button', { name: 'Find', exact: true })).toHaveCount(0);
  const now = new Date();
  await page.clock.install({ time: now });
  await page.clock.pauseAt(now);
  await input.fill('co');
  await page.clock.runFor(200);
  await input.fill('codex');
  await page.clock.runFor(249);
  expect(commands).toEqual(['']);
  await page.clock.runFor(1);
  await expect.poll(() => commands).toEqual(['', 'codex']);
  await page.clock.runFor(60_000);
  expect(commands).toEqual(['', 'codex']);
  await input.dispatchEvent('compositionstart');
  await input.fill('kimi');
  await page.clock.runFor(1000);
  expect(commands).toEqual(['', 'codex']);
  await input.dispatchEvent('compositionend');
  await page.clock.runFor(250);
  await expect.poll(() => commands).toEqual(['', 'codex', 'kimi']);
  await input.fill('../invalid');
  await page.clock.runFor(1000);
  expect(commands).toEqual(['', 'codex', 'kimi']);
  await input.fill('');
  await expect.poll(() => commands).toEqual(['', 'codex', 'kimi', '']);
  await input.fill('docker');
  await page.keyboard.press('Escape');
  await page.clock.runFor(10_000);
  await expect(picker).not.toBeVisible();
  expect(commands).toEqual(['', 'codex', 'kimi', '']);
});

test('discovery errors preserve manual path entry while command lookup is automatic', async ({ page }) => {
  const { dialog } = await setup(page);
  const calls: string[] = [];
  await page.route('**/api/sandboxes/local/programs?*', route => {
    calls.push(route.request().url());
    return route.fulfill({ status: 503, json: { code: 'unavailable' } });
  });
  await dialog.getByRole('radio', { name: 'Specified programs', exact: true }).click();
  await dialog.getByLabel('Program path 1', { exact: true }).fill('/opt/manual/agent');
  await dialog.getByRole('button', { name: 'Choose a discovered program' }).click();
  const picker = page.getByRole('dialog', { name: 'Choose a host program' });
  await expect.poll(() => calls.length).toBe(1);
  await picker.getByLabel('Find by command name').fill('custom-tool');
  await expect(picker.getByRole('button', { name: 'Find', exact: true })).toHaveCount(0);
  await expect.poll(() => calls.length).toBe(2);
  expect(new URL(calls[1]).searchParams.get('command')).toBe('custom-tool');
  await page.keyboard.press('Escape');
  await expect(picker).not.toBeVisible();
  await expect(dialog.getByLabel('Program path 1', { exact: true })).toHaveValue('/opt/manual/agent');
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
