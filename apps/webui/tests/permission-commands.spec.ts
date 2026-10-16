import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';

const port = process.env.MOLR_TEST_PORT ?? '5193', base = `http://127.0.0.1:${port}`;
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { ...data, Mutation: { IdempotencyKey: crypto.randomUUID() } } });
  expect(response.status(), await response.text()).toBe(200); return response.json();
}

for (const command of ['/permission approve', '/permission reject', '/权限审批 同意', '/权限审批 拒绝']) {
  test(`${command} only resolves the pending proposal and preserves attachments`, async ({ page }, info) => {
    await page.goto('/');
    await page.getByLabel('Username', { exact: true }).fill('owner');
    await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
    const name = `Permission ${command === '/权限审批 同意' ? 'grouped' : 'cpu'} commands ${info.project.name} ${Date.now()}`, origin = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
    const resource = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 65536, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0 } }] } });
    if (command === '/权限审批 同意') await post(page, '/api/sandboxes/resource', { SandboxID: 'local', Kind: 'network', SourceID: 'preset:host', Name: 'Grouped approval network' });
    const models = await (await page.request.get('/api/models')).json();
    const assistant = await post(page, '/api/assistants', { Name: name, ModelID: models.find((m: any) => m.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
    await page.goto(`/#/assistants/${assistant.ID}`);
    const input = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
    const send = page.getByRole('button', { name: 'Send', exact: true });
    await input.fill('/');
    await expect(page.getByRole('option')).toHaveCount(1);
    await input.fill(command); await send.click();
    await expect(page.locator('.composer-notice')).toHaveText('There is no pending permission request.');
    await expect(page.locator('.ui-chat-message--user')).toHaveCount(0);
    await input.fill('Please run the calculation.'); await send.click();
    await expect(page.locator('.permission-request-card')).toBeVisible({ timeout: 60_000 });
    await input.fill('/'); await expect(page.getByRole('option')).toHaveCount(3);
    const requestPath = `/api/permission-requests?assistant_id=${assistant.ID}`;
    const [proposal] = await (await page.request.get(requestPath)).json();
    // A newer request arriving before submission must not silently change the target.
    await page.route(`**${requestPath}`, route => route.fulfill({ json: [{ ...proposal, ID: 'another-request' }, proposal] }));
    await input.fill(command); await send.click();
    await expect(page.locator('.composer-notice')).toContainText('Pending requests changed');
    await expect(page.locator('.permission-request-card')).toContainText('Awaiting permission');
    await page.unroute(`**${requestPath}`); await page.reload();
    await expect(page.locator('.permission-request-card')).toContainText('Awaiting permission');
    await input.fill('/permission invalid'); await send.click();
    await expect(page.locator('.composer-notice')).toContainText('Usage: /permission approve|reject [id|all].');
    await page.locator('.message-composer input[type=file]').setInputFiles({ name: 'retained.txt', mimeType: 'text/plain', buffer: Buffer.from('Keep this attachment.') });
    await expect(page.locator('.composer-attachments')).toContainText('retained.txt');
    await input.fill(command.startsWith('/权限') ? '/权限审批 ' : '/permission ');
    await expect(page.getByRole('option')).toHaveCount(2);
    await page.getByRole('option').filter({ has: page.locator('.select__option-label', { hasText: command }) }).click();
    await expect(input).toHaveValue(command);
    if (command === '/permission approve') await input.fill(`${command} all`);
    if (command === '/权限审批 同意') await input.fill(`${command} ${proposal.ID}`);
    await input.press('Enter');
    await expect(input).toHaveValue('');
    const approved = command.endsWith('approve') || command.endsWith('同意');
    await expect(page.locator('.permission-request-card')).toContainText(approved ? 'Approved' : 'Rejected');
    const [resolved] = await (await page.request.get(requestPath)).json();
    expect(resolved.State).toBe(approved ? 'approved' : 'rejected');
    if (approved) {
      expect(resolved.Granted.ResourceID).toBe(proposal.Input.resource_id);
      expect(Date.parse(resolved.Granted.ExpiresAt)).toBe(Date.parse(proposal.Input.expires_at));
      expect(resolved.Granted.Constraints.compute).toEqual(proposal.Input.constraints.compute);
      if (command === '/权限审批 同意') {
        expect(resolved.GrantedAll).toHaveLength(2);
        expect(resolved.GrantedAll[1].ResourceID).toBe(proposal.Input.additional_requests[0].resource_id);
        expect(resolved.GrantedAll[1].Constraints.network).toEqual(proposal.Input.additional_requests[0].constraints.network);
      }
    }
    await expect(page.locator('.composer-attachments')).toContainText('retained.txt');
    await expect(page.locator('.ui-chat-message--user')).toHaveCount(1);
    await input.fill(command); await send.click();
    await expect(page.locator('.composer-notice')).toHaveText('There is no pending permission request.');
    await expect(page.locator('.ui-chat-message--user')).toHaveCount(1);
  });
}
