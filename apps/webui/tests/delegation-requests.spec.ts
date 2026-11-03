import { test, expect, type Page } from '@playwright/test';
const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}
for (const kind of ['hiring', 'communication']) for (const decision of ['approve', 'reject']) test(`${kind} request ${decision} uses existing approval and resume`, async ({ page }, info) => {
  test.setTimeout(100_000);
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
  const name = `Permission delegation ${kind} ${decision} ${info.project.name} ${Date.now()}`;
  const origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 65536, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
  const model = (await (await page.request.get('/api/models')).json()).find((m: any) => m.ResourceID === resource.ID);
  const assistant = await post(page, '/api/assistants', { Name: name, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 10_000_000 }, Period: 'daily' } });
  const peer = kind === 'communication' ? await post(page, '/api/assistants', { Name: `Reviewer ${info.project.name} ${decision}`, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 10_000_000 }, Period: 'daily' } }) : null;
  await page.goto(`/#/assistants/${assistant.ID}`);
  await page.locator('.chat-assistant-info').click();
  await page.getByRole('tab', { name: 'Permissions', exact: true }).click();
  await expect(page.locator('.assistant-info .permission-requests')).toHaveCount(0);
  await page.locator('.assistant-info > .ui-dialog__header').getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.locator('.assistant-info')).toHaveCount(0);
  const composer = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
  await composer.fill('Request the authorization needed to delegate this report.');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  const card = page.getByRole('article', { name: kind === 'hiring' ? 'Request temporary hiring' : 'Request assistant communication', exact: true });
  await expect(card).toContainText('Awaiting permission', { timeout: 30_000 });
  const extension = page.getByRole('button', { name: '1 pending permission request', exact: true });
  await expect(extension).toContainText('1 pending permission request');
  await expect(page.locator('.assistant-notifications')).toHaveCount(0);
  await expect(card).toContainText('Validity');
  await expect(card).toContainText('No expiry');
  await expect.poll(async () => (await (await page.request.get(`/api/assistants/${assistant.ID}/status`)).json()).WaitingReason).toBe('permission_pending');
  const before = await (await page.request.get(`/api/assistants/${assistant.ID}`)).json();
  expect(before.Hiring?.Enabled ?? false).toBe(false); expect(before.PeerCommunication?.Enabled ?? false).toBe(false);
  await page.reload();
  await expect(card).toContainText('Awaiting permission');
  if (decision === 'reject') {
    await extension.click();
    await expect(page.getByRole('tab', { name: 'Permissions', exact: true })).toHaveAttribute('aria-selected', 'true');
    await page.locator('.assistant-info .permission-requests').getByRole('button', { name: 'Reject', exact: true }).click();
    await page.getByRole('dialog', { name: 'Reject permission request', exact: true }).getByRole('button', { name: 'Confirm', exact: true }).click();
  } else {
    if (kind === 'communication') {
      await composer.fill('/permission approve');
      await page.getByRole('button', { name: 'Send', exact: true }).click();
      await expect(page.locator('.composer-notice')).toBeVisible();
      await composer.fill('');
    }
    await composer.fill('Draft preserved during approval');
    await extension.click();
    await expect(page.getByRole('tab', { name: 'Permissions', exact: true })).toHaveAttribute('aria-selected', 'true');
    await page.locator('.assistant-info .permission-requests').getByRole('button', { name: 'Review permission', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Review permission', exact: true });
    await expect(dialog.getByRole('group', { name: 'Validity', exact: true })).toBeVisible();
    await expect(dialog.getByRole('button', { name: 'Forever', exact: true })).toHaveAttribute('aria-pressed', 'true');
    await dialog.getByText('Current authorization', { exact: true }).click();
    await expect(dialog.getByText('Not allowed', { exact: true })).toBeVisible();
    if (kind === 'hiring') {
      await dialog.getByLabel('Maximum active assistants').fill('3');
      await dialog.getByRole('spinbutton', { name: 'Shared total budget (USD)' }).fill('2');
      await dialog.getByRole('button', { name: '7 days', exact: true }).click();
    } else {
      await expect(dialog.getByRole('button', { name: 'Approve permission', exact: true })).toBeDisabled();
      await dialog.getByRole('combobox', { name: 'Allowed assistants', exact: true }).click();
      await page.getByRole('option', { name: peer.Name, exact: true }).click();
      await page.keyboard.press('Escape');
      await expect(dialog.getByRole('link', { name: 'View assistant', exact: true })).toBeVisible();
    }
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
    await dialog.getByRole('button', { name: 'Approve permission', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(composer).toHaveValue('Draft preserved during approval');
  }
  await page.locator('.assistant-info > .ui-dialog__header').getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.locator('.assistant-info')).toHaveCount(0);
  await expect(card).toContainText(decision === 'approve' ? 'Approved' : 'Rejected');
  await expect(page.getByRole('log', { name: 'Assistant conversation' }).getByText(decision === 'approve' ? 'Delegation approved; current authorization checked before continuing.' : 'Delegation declined; no additional authorization was granted.', { exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(extension).toHaveCount(0);
  const requests = await (await page.request.get(`/api/permission-requests?assistant_id=${assistant.ID}`)).json();
  expect(requests).toHaveLength(1); expect(requests[0].State).toBe(decision === 'approve' ? 'approved' : 'rejected');
  const after = await (await page.request.get(`/api/assistants/${assistant.ID}`)).json();
  if (decision === 'reject') { expect(after.Hiring?.Enabled ?? false).toBe(false); expect(after.PeerCommunication?.Enabled ?? false).toBe(false); }
  else if (kind === 'hiring') { expect(after.Hiring.MaxAssistants).toBe(3); expect(after.Hiring.Budget.Micros).toBe(2000000); expect(requests[0].Input.hiring.max_assistants).toBe(2); expect(requests[0].ApprovedDelegation.hiring.max_assistants).toBe(3); }
  else { expect(after.PeerCommunication.AssistantIDs).toEqual([peer.ID]); expect(after.PeerCommunication.ExpiresAt).toBeNull(); expect((await (await page.request.get(`/api/assistants/${peer.ID}`)).json()).PeerCommunication?.Enabled ?? false).toBe(false); }
  await page.locator('.chat-assistant-info').click();
  await page.getByRole('tab', { name: 'Permissions', exact: true }).click();
  await expect(page.locator('.assistant-info').getByText('No pending permission requests', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Authorization history', exact: true }).click();
  await expect(page.locator('.assistant-info .permission-requests')).toContainText(decision === 'approve' ? 'Approved' : 'Rejected');
  await expect(page.locator('.assistant-info').getByRole('region', { name: 'Task delegation', exact: true })).toHaveCount(0);
  await page.locator('.assistant-info').getByRole('button', { name: 'Back', exact: true }).click();
  await expect(page.locator('.assistant-info').getByRole('button', { name: 'Authorization history', exact: true })).toBeFocused();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
