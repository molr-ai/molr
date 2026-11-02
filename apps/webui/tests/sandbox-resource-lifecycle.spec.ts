import { test, expect, type Page } from '@playwright/test';
import { mkdtemp, realpath, readFile, writeFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';

async function login(page: Page) {
  await page.goto('/#/resources');
  await page.getByRole('textbox', { name: 'Username', exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('tab', { name: 'Sandboxes', exact: true }).click();
}
async function post(page: Page, path: string, data: object, version = 0) {
  const base = new URL(page.url()).origin, port = new URL(base).port;
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  return page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: version } } });
}
async function json(response: Awaited<ReturnType<typeof post>>) { expect(response.ok(), await response.text()).toBe(true); return response.json(); }

for (const kind of ['storage', 'network'] as const) test(`${kind} disconnect, reconnect and guarded deletion preserve independent resources`, async ({ page }, info) => {
  const directory = await realpath(await mkdtemp(join(tmpdir(), 'molr-resource-lifecycle-')));
  await writeFile(join(directory, 'keep.txt'), 'keep directory contents');
  try {
    await login(page);
    const name = `${kind} lifecycle ${info.project.name} ${Date.now()}`;
    const make = (suffix: string) => post(page, '/api/sandboxes/resource', { SandboxID: 'local', Kind: kind, SourceID: kind === 'network' ? 'preset:internet' : '', Path: kind === 'storage' ? directory : '', Name: name + suffix }).then(json);
    let resource = await make('');
    const sibling = await make(' sibling');
    const provider = await json(await post(page, '/api/resources', { Kind: 'model', Provider: 'openai-compatible', Name: name + ' provider', Binding: { Endpoint: `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? '18882'}/v1/chat/completions`, AllowedOrigins: [`http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? '18882'}`], Models: [{ Name: 'local-test', ContextLimit: 8192, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] } }));
    const models = await (await page.request.get('/api/models')).json();
    const model = models.find((m: { ResourceID: string }) => m.ResourceID === provider.ID);
    const assistant = await json(await post(page, '/api/assistants', { Name: name + ' assistant', ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } }));
    const grant = await json(await post(page, '/api/grants', { AssistantID: assistant.ID, ResourceID: resource.ID, Actions: kind === 'storage' ? ['read'] : ['execute'], Scope: {}, ...(kind === 'network' ? { Constraints: { network: { bytes: 1048576, connections: 4, period: 'day', timezone: 'UTC' } } } : {}) }));
    const blocked = await post(page, '/api/resources/delete', { ID: resource.ID }, resource.Version);
    expect(blocked.ok()).toBe(false);
    const blockedError = await blocked.json();
    expect(blockedError.reason).toBe('resource_not_disconnected');
    expect(blockedError.message).toBe('Disconnect this resource before deleting it.');
    await page.reload();
    await page.getByRole('tab', { name: 'Sandboxes', exact: true }).click();
    await page.getByRole('tab', { name: kind === 'storage' ? 'Storage' : 'Network', exact: true }).click();
    const main = page.locator('.sandbox-detail');
    const row = main.locator(`[data-resource-id="${resource.ID}"]`);
    await row.getByRole('button', { name: `More · ${name}`, exact: true }).click();
    await expect(page.getByRole('menuitem', { name: 'Delete resource', exact: true })).toHaveCount(0);
    await page.getByRole('menuitem', { name: 'Disconnect resource', exact: true }).click();
    await page.getByRole('dialog', { name: 'Disconnect resource', exact: true }).getByRole('button', { name: 'Confirm', exact: true }).click();
    await expect(row).toHaveCount(0);
    await expect(main.locator(`[data-resource-id="${sibling.ID}"]`)).toBeVisible();
    await main.getByRole('button', { name: /Disconnected resources ·/ }).click();
    const drawer = page.getByRole('dialog', { name: 'Disconnected resources', exact: true });
    const disconnected = drawer.locator(`[data-resource-id="${resource.ID}"]`);
    await expect(disconnected).toBeVisible();
    const rowBounds = await disconnected.boundingBox(), bodyBounds = await disconnected.locator('.sandbox-resource-body').boundingBox();
    expect(bodyBounds!.width).toBeGreaterThan(rowBounds!.width * 0.9);
    await disconnected.getByRole('button', { name: `More · ${name}`, exact: true }).click();
    const deleteItem = page.getByRole('menuitem', { name: 'Delete resource', exact: true });
    const reason = 'Active grants remain. Their account owners must revoke them before deletion.';
    await expect(deleteItem).toBeDisabled();
    await expect(disconnected.locator('.sandbox-resource-footer > .muted')).toHaveCount(0);
    const tip = page.getByRole('tooltip', { name: reason, exact: true });
    if (info.project.name === 'mobile') await deleteItem.tap({ force: true });
    else await deleteItem.hover();
    await expect(tip).toBeVisible();
    await expect(page.getByRole('dialog', { name: 'Delete resource', exact: true })).toHaveCount(0);
    await page.keyboard.press('End');
    await expect(deleteItem).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(tip).not.toBeVisible();
    await expect(page.getByRole('menu')).toBeVisible();
    await page.keyboard.press('Enter');
    await expect(tip).toBeVisible();
    await expect(page.getByRole('dialog', { name: 'Delete resource', exact: true })).toHaveCount(0);
    await page.keyboard.press('Escape');
    await page.keyboard.press('Escape');
    await expect(page.getByRole('menu')).not.toBeVisible();
    await expect(tip).toHaveCount(0);
    await disconnected.getByRole('button', { name: 'Reconnect', exact: true }).click();
    const confirm = page.getByRole('dialog', { name: 'Reconnect', exact: true });
    await expect(confirm).toContainText('without resetting expiry or quota usage');
    await confirm.getByRole('button', { name: 'Confirm', exact: true }).click();
    await expect(disconnected).toHaveCount(0);
    await drawer.getByRole('button', { name: /Close/ }).first().click();
    await expect(row).toBeVisible();
    resource = await (await page.request.get(`/api/resources/${resource.ID}`)).json();
    const grants = await (await page.request.get(`/api/grants?resource_id=${resource.ID}`)).json();
    expect((grants.Items ?? grants).find((g: { ID: string }) => g.ID === grant.ID)).toEqual(grant);
    await json(await post(page, '/api/resources/disconnect', { ID: resource.ID }, resource.Version));
    resource = await (await page.request.get(`/api/resources/${resource.ID}`)).json();
    const denied = await post(page, '/api/resources/delete', { ID: resource.ID }, resource.Version);
    const deniedError = await denied.json();
    expect(deniedError.reason).toBe('resource_has_grants');
    expect(deniedError.message).toBe('This resource still has active permissions. Revoke them before deleting it.');
    await json(await post(page, '/api/grants/revoke', { GrantID: grant.ID }, grant.Version));
    await page.reload();
    await page.getByRole('tab', { name: 'Sandboxes', exact: true }).click();
    await page.getByRole('tab', { name: kind === 'storage' ? 'Storage' : 'Network', exact: true }).click();
    await main.getByRole('button', { name: /Disconnected resources ·/ }).click();
    await disconnected.getByRole('button', { name: `More · ${name}`, exact: true }).click();
    await page.getByRole('menuitem', { name: 'Delete resource', exact: true }).click();
    await page.getByRole('dialog', { name: 'Delete resource', exact: true }).getByRole('button', { name: 'Confirm', exact: true }).click();
    await expect(disconnected).toHaveCount(0);
    expect((await page.request.get(`/api/resources/${resource.ID}`)).status()).toBe(404);
    expect((await page.request.get(`/api/resources/${sibling.ID}`)).status()).toBe(200);
    expect(await readFile(join(directory, 'keep.txt'), 'utf8')).toBe('keep directory contents');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  } finally { await rm(directory, { recursive: true, force: true }); }
});
