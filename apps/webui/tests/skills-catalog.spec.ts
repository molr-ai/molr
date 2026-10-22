import { expect, test } from '@playwright/test';

test('skills catalog preserves ownership, combines filters and reads instructions in a responsive drawer', async ({ page }, info) => {
  test.setTimeout(120_000);
  await page.goto('/#/resources');
  if (!(await page.request.get('/api/auth/me')).ok()) {
    await page.getByLabel('Username', { exact: true }).fill('owner');
    await page.getByLabel('Password', { exact: true }).fill(process.env.MOLR_E2E_PASSWORD ?? 'webui-e2e-password-2026!');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  }
  const origin = new URL(page.url()).origin;
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${new URL(origin).port}`)!.value;
  const post = async (path: string, data: object) => {
    const response = await page.request.post(path, { headers: { Origin: origin, 'X-CSRF-Token': csrf }, data });
    expect(response.ok(), await response.text()).toBe(true); return response.json();
  };
  const sandbox = await (await page.request.get('/api/sandbox')).json();
  expect(sandbox.Enabled).toBe(true); expect(sandbox.Capabilities.Reason).toBe('available');
  const suffix = `${info.project.name}-${Date.now()}`;
  const endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const provider = await post('/api/resources', { Mutation: { IdempotencyKey: crypto.randomUUID() }, Name: `Skills ${suffix}`, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [endpoint], Models: [{ Name: 'Skills fixture', ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 1 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  const model = models.find((m: { ResourceID: string }) => m.ResourceID === provider.ID);
  const assistants = [];
  const text = `---\nname: Report review\ndescription: Check the report and preserve source references.\n---\n# Review\n\n<script>alert('plain text')</script>\n${'Long instructions '.repeat(150)}\n${'unbroken-path-'.repeat(100)}`;
  for (const name of [`Research ${suffix}`, `Writing ${suffix}`]) {
    const assistant = await post('/api/assistants', { Mutation: { IdempotencyKey: crypto.randomUUID() }, Name: name, ModelID: model.ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
    assistants.push(assistant);
    const workspace = `/api/assistants/${assistant.ID}/workspace`;
    await post(workspace, { Action: 'mkdir', Path: '.agents/skills/report-review' });
    await post(workspace, { Action: 'write', Path: '.agents/skills/report-review/SKILL.md', Text: text });
  }
  await page.goto('/#/resources');
  await page.getByRole('tab', { name: 'Skills', exact: true }).click();
  const catalog = page.locator('.skills-catalog'), rows = catalog.locator('.tools-catalog-row');
  const search = page.getByRole('searchbox', { name: 'Search skills, workflows or assistants' });
  await search.fill(`Report review ${suffix}`);
  await expect(rows).toHaveCount(2);
  await expect(rows.filter({ hasText: assistants[0].Name })).toHaveCount(1);
  await expect(rows.filter({ hasText: assistants[1].Name })).toHaveCount(1);
  const filters = page.getByRole('group', { name: 'Skill sources' });
  await filters.getByRole('button', { name: /^Built-in/ }).click();
  await expect(rows).toHaveCount(0);
  await expect(catalog).toContainText('No matching results');
  await filters.getByRole('button', { name: /^Assistant specific/ }).click();
  await expect(rows).toHaveCount(2);
  await page.getByRole('combobox', { name: 'Assistant scope' }).click();
  await page.getByRole('option', { name: assistants[0].Name, exact: true }).click();
  await expect(rows).toHaveCount(1);
  await rows.first().focus(); await page.keyboard.press('Enter');
  const detail = page.getByRole('dialog', { name: 'Report review', exact: true });
  await expect(detail).toBeVisible();
  await expect(detail.locator('.tool-catalog-model-description')).toHaveText(text);
  expect(await detail.locator('script').count()).toBe(0);
  await expect(detail).toContainText(assistants[0].Name);
  expect(await detail.locator('.ui-dialog__body').evaluate(e => e.scrollWidth <= e.clientWidth)).toBe(true);
  await page.keyboard.press('Escape');
  await expect(detail).toBeHidden(); await expect(rows.first()).toBeFocused();
  await expect(search).toHaveValue(`Report review ${suffix}`);
  await search.fill('no matching skill');
  await catalog.getByRole('button', { name: 'Clear filters', exact: true }).click();
  await expect(search).toHaveValue('');
  await search.fill('molr-docs');
  await expect(rows).toHaveCount(1); await rows.click();
  const builtin = page.getByRole('dialog', { name: 'molr-docs', exact: true });
  await expect(builtin).toContainText('Read-only');
  await expect(builtin.locator('.tool-catalog-model-description')).toContainText('name: molr-docs');
  await page.keyboard.press('Escape'); await expect(builtin).toBeHidden(); await search.fill('');
  const all = await (await page.request.get('/api/skills')).json();
  await expect(rows).toHaveCount(all.Items.length);
  // All palette/mode combinations retain readable semantic feedback and fit the viewport.
  for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) {
    for (const theme of ['light', 'dark']) {
      await page.evaluate(({ palette, theme }) => { document.documentElement.dataset.palette = palette; document.documentElement.dataset.theme = theme; }, { palette, theme });
      await expect(rows.first()).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      expect(await catalog.evaluate(e => e.scrollWidth <= e.clientWidth)).toBe(true);
      await page.screenshot({ animations: 'disabled', path: info.outputPath(`skills-${palette}-${theme}.png`) });
    }
  }
});
