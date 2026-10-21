import { expect, test, type Page } from '@playwright/test';
import type { Model, Plan, PlanEvent } from '@molr/sdk';

async function login(page: Page) {
  await page.goto('/');
  if (!(await page.request.get('/api/auth/me')).ok()) {
    await page.getByLabel('Username', { exact: true }).fill('owner');
    await page.getByLabel('Password', { exact: true }).fill(process.env.MOLR_E2E_PASSWORD ?? 'webui-e2e-password-2026!');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  }
  await expect(page.getByRole('navigation', { name: 'Molr', exact: true })).toBeVisible();
}

function gate() {
  let release!: () => void;
  const promise = new Promise<void>(resolve => { release = resolve; });
  return { promise, release };
}

test('sandbox loading replaces the workspace and refresh retains its content', async ({ page }) => {
  await login(page);
  let response = gate(), requested = gate();
  await page.route(url => url.pathname === '/api/sandboxes', async route => {
    const pending = response.promise;
    requested.release();
    await pending;
    await route.continue();
  });
  const workspace = page.locator('.sandbox-workspace');
  try {
    await page.goto('/#/resources');
    await page.getByRole('tab', { name: 'Sandboxes', exact: true }).click();
    await requested.promise;
    await expect(workspace.locator('.loading')).toHaveCount(1);
    await expect(workspace.locator('.sandbox-layout, .sandbox-mobile-switch')).toHaveCount(0);
    response.release();
    await expect(workspace.locator('.sandbox-detail')).toBeVisible();
    await expect(workspace.locator('.loading')).toHaveCount(0);

    response = gate(); requested = gate();
    await page.getByRole('button', { name: 'Check again', exact: true }).click();
    await requested.promise;
    await expect(workspace.locator('.sandbox-detail')).toBeVisible();
    await expect(workspace.locator('.loading')).toHaveCount(0);
  } finally { response.release(); await page.unrouteAll({ behavior: 'wait' }); }
});

test('plan list has one initial skeleton and keeps rows while other queries and pagination load', async ({ page }) => {
  await login(page);
  const at = new Date(Date.now() + 3_600_000).toISOString();
  const event = (id: string): PlanEvent => {
    const ref = { Kind: 'todo', ID: id } as const;
    const plan: Plan = {
      Ref: ref, Title: id, AssistantID: '', Schedule: null, Ready: true, ExecutionCount: 0, HasSuccessors: false, Executions: [], Occurrences: [], BlockedBy: [],
      Todo: { ID: id, Title: id, Notes: '', AssistantID: '', DueAt: at, Completed: false, Version: 1 },
      Links: { Ref: ref, Dependencies: [], Handoff: 'notify', Paused: false, Version: 1, Dispatched: false, Runs: [] },
    };
    return { Key: `${id}@${at}`, At: at, Plan: plan };
  };
  const first = event('First loading fixture'), second = event('Second loading fixture');
  const plans = gate(), initial = gate(), nextPage = gate(), nextRequested = gate();
  await page.route(url => url.pathname === '/api/plans', async route => {
    await plans.promise;
    await route.fulfill({ json: [first.Plan, second.Plan] });
  });
  await page.route(url => url.pathname === '/api/plans/events', async route => {
    const next = new URL(route.request().url()).searchParams.has('cursor');
    if (next) nextRequested.release();
    await (next ? nextPage : initial).promise;
    const item=next ? second : first;
    await route.fulfill({ json: { Items: [{Key:item.Key,At:item.At,PlanKey:item.Plan.Ref.ID}], Plans:{[item.Plan.Ref.ID]:item.Plan}, NextCursor: next ? '' : 'next-page' } });
  });
  const workspace = page.locator('.plans-workspace');
  try {
    await page.goto('/#/plans');
    await page.getByRole('button', { name: 'List', exact: true }).click();
    await expect(workspace.locator('.loading')).toHaveCount(1);
    await expect(workspace.locator('.plans-list, .plans-empty')).toHaveCount(0);
    initial.release();
    await expect(workspace.locator('.plan-row')).toHaveCount(1);
    await expect(workspace.locator('.loading')).toHaveCount(0);
    plans.release();
    await page.locator('.plans-load-more').click();
    await nextRequested.promise;
    await expect(workspace.locator('.plan-row')).toHaveCount(1);
    await expect(workspace.locator('.loading')).toHaveCount(0);
    await expect(page.locator('.plans-load-more')).toBeDisabled();
    nextPage.release();
    await expect(workspace.locator('.plan-row')).toHaveCount(2);
    await expect(page.locator('.plans-load-more')).toHaveCount(0);
  } finally {
    plans.release(); initial.release(); nextPage.release();
    await page.unrouteAll({ behavior: 'wait' });
  }
});

for (const hasModel of [false, true]) {
  test(`setup loading excludes forms, then shows ${hasModel ? 'model configuration' : 'provider connection'}`, async ({ page }) => {
    await login(page);
    await page.route(url => url.pathname === '/api/setup', route => route.fulfill({ json: { Required: true, AssistantID: '' } }));
    await page.route(url => url.pathname === '/api/resources/discover-local', route => route.fulfill({ json: [] }));
    const model: Model = { ID: 'loading-model', ResourceID: 'loading-provider', Name: 'Loading fixture model', SupportsTools: true, ContextLimit: 32768, Pricing: null };
    let response = gate(), requested = gate();
    await page.route(url => url.pathname === '/api/models', async route => {
      const pending = response.promise;
      requested.release();
      await pending;
      await route.fulfill({ json: hasModel ? [model] : [] });
    });
    const setup = page.locator('.setup-card');
    try {
      await page.reload();
      await requested.promise;
      await expect(setup.locator('.loading')).toHaveCount(1);
      await expect(setup.locator('.setup-provider, .setup-finish')).toHaveCount(0);
      response.release();
      const form = setup.locator(hasModel ? '.setup-finish' : '.setup-provider');
      await expect(form).toBeVisible();
      await expect(setup.locator('.loading')).toHaveCount(0);
      response = gate(); requested = gate();
      await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
      await requested.promise;
      await expect(form).toBeVisible();
      await expect(setup.locator('.loading')).toHaveCount(0);
    } finally { response.release(); await page.unrouteAll({ behavior: 'wait' }); }
  });
}

test('tool and MCP catalogs keep results and empty states out of their initial skeleton', async ({ page }) => {
  await login(page);
  const tools = gate(), mcp = gate();
  await page.route(url => url.pathname === '/api/tools', async route => { await tools.promise; await route.continue(); });
  await page.route(url => url.pathname === '/api/mcp/connections', async route => { await mcp.promise; await route.fulfill({ json: [] }); });
  try {
    await page.goto('/#/resources');
    await page.getByRole('tab', { name: 'Tools', exact: true }).click();
    const catalog = page.locator('.tools-catalog');
    await expect(catalog.locator('.loading')).toHaveCount(1);
    await expect(catalog.locator('.tools-catalog-summary, .tools-catalog-group, .tools-catalog-empty')).toHaveCount(0);
    tools.release();
    await expect(catalog.locator('.tools-catalog-group').first()).toBeVisible();
    await expect(catalog.locator('.loading')).toHaveCount(0);
    await page.getByRole('tab', { name: 'MCP tools', exact: true }).click();
    await expect(catalog.locator('.loading')).toHaveCount(1);
    await expect(catalog.locator('.tools-catalog-summary, .tools-catalog-empty')).toHaveCount(0);
    mcp.release();
    await expect(catalog.locator('.tools-catalog-empty')).toBeVisible();
    await expect(catalog.locator('.loading')).toHaveCount(0);
  } finally { tools.release(); mcp.release(); await page.unrouteAll({ behavior: 'wait' }); }
});
