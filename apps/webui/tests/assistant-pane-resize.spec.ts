import { test, expect, type Page } from '@playwright/test';

const base = `http://127.0.0.1:${process.env.MOLR_TEST_PORT ?? process.env.MOLR_E2E_PORT ?? 5193}`;
async function login(page: Page) {
  await page.goto('/');
  await page.getByLabel('Username', { exact: true }).fill('owner');
  await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('navigation', { name: 'Molr' })).toBeVisible();
}
async function post(page: Page, path: string, data: object) {
  const csrf = (await page.context().cookies()).find(c => c.name === `molr_dev_csrf_p${new URL(base).port}`)!.value;
  const response = await page.request.post(path, { headers: { Origin: base, 'X-CSRF-Token': csrf }, data: { Mutation: { IdempotencyKey: crypto.randomUUID(), ExpectedVersion: 0 }, ...data } });
  expect(response.ok(), await response.text()).toBe(true);
  return response.json();
}
async function hire(page: Page, name: string) {
  const endpoint = `http://127.0.0.1:${process.env.MOLR_E2E_MODEL_PORT ?? 18882}`;
  const provider = await post(page, '/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: endpoint, AllowedOrigins: [new URL(endpoint).origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] } });
  const models = await (await page.request.get('/api/models')).json();
  return post(page, '/api/assistants', { Name: name, ModelID: models.find((m: { ResourceID: string }) => m.ResourceID === provider.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } });
}

async function width(page: Page, selector: string) { return page.locator(selector).evaluate(e => e.getBoundingClientRect().width); }
async function drag(page: Page, label: string, dx: number) {
  const separator = page.getByRole('separator', { name: label, exact: true });
  const box = (await separator.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + 100);
  await page.mouse.down(); await page.mouse.move(box.x + box.width / 2 + dx, box.y + 100, { steps: 8 }); await page.mouse.up();
}

test('assistant panes resize, persist independently, and respect minimum widths across viewport changes', async ({ page }, info) => {
  await login(page);
  const assistant = await hire(page, `Resize ${info.project.name} ${crypto.randomUUID()}`);
  await page.goto(`/#/assistants/${assistant.ID}`);
  if (info.project.name === 'mobile') {
    await expect(page.locator('.chat-panel')).toBeVisible();
    await expect(page.locator('.pane-separator')).toHaveCount(0);
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    return;
  }
  await page.setViewportSize({ width: 1920, height: 1000 });
  const list = page.getByRole('separator', { name: 'Assistant list', exact: true });
  await expect(list).toBeVisible();
  await drag(page, 'Assistant list', 80);
  await expect.poll(() => width(page, '.workspace-sidebar')).toBe(336);
  await page.reload();
  await expect.poll(() => width(page, '.workspace-sidebar')).toBe(336);
  const open = async (kind: string) => {
    await page.goto(`/#/assistants/${assistant.ID}?panel=${kind}`);
    await expect(page.locator(`.chat-side-panel[data-kind="${kind}"][data-docked]`)).toBeVisible();
    await expect.poll(() => page.evaluate(() => document.getAnimations().filter(a => a.id.startsWith('molr-chat-panel')).length)).toBe(0);
  };
  await open('info');
  await drag(page, 'Resize side panel', -90);
  await expect.poll(() => width(page, '.chat-side-panel')).toBe(690);
  await open('tasks');
  await expect.poll(() => width(page, '.chat-side-panel')).toBe(480);
  await drag(page, 'Resize side panel', 60);
  await expect.poll(() => width(page, '.chat-side-panel')).toBe(420);
  await open('info');
  await expect.poll(() => width(page, '.chat-side-panel')).toBe(690);
  await page.reload();
  await expect.poll(() => width(page, '.chat-side-panel')).toBe(690);
  await open('tasks');
  await expect.poll(() => width(page, '.chat-side-panel')).toBe(420);
  await page.getByRole('separator', { name: 'Resize side panel' }).press('Home');
  await expect.poll(() => width(page, '.chat-side-panel')).toBe(320);
  await drag(page, 'Resize side panel', 250);
  await expect.poll(() => width(page, '.chat-side-panel')).toBe(320);
  await list.press('Home');
  await expect.poll(() => width(page, '.workspace-sidebar')).toBe(224);
  await drag(page, 'Assistant list', -180);
  await expect.poll(() => width(page, '.workspace-sidebar')).toBe(224);
  await list.press('End');
  await expect.poll(() => width(page, '.chat-panel')).toBe(360);
  await page.setViewportSize({ width: 1530, height: 900 });
  await expect.poll(() => width(page, '.chat-panel')).toBeGreaterThanOrEqual(360);
  await expect.poll(() => width(page, '.chat-side-panel')).toBeGreaterThanOrEqual(320);
  await page.setViewportSize({ width: 1100, height: 800 });
  await expect(page.locator('.chat-side-panel[data-docked]')).toHaveCount(0);
  await page.keyboard.press('Escape');
  await expect.poll(() => width(page, '.chat-panel')).toBeGreaterThanOrEqual(360);
  await page.setViewportSize({ width: 710, height: 800 });
  await expect.poll(() => width(page, '.workspace-sidebar')).toBeGreaterThanOrEqual(224);
  await expect.poll(() => width(page, '.chat-panel')).toBeGreaterThanOrEqual(360);
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('switching panels animates from the last resized width and handles interrupted and equal-width transitions', async ({ page }, info) => {
  test.skip(info.project.name !== 'desktop', 'Docked transitions require a wide workspace.');
  await page.setViewportSize({ width: 1920, height: 1000 });
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await login(page);
  const assistant = await hire(page, `Transitions ${crypto.randomUUID()}`);
  await page.goto(`/#/assistants/${assistant.ID}?panel=info`);
  const slot = page.locator('.chat-side-panel');
  await expect(slot).toHaveCSS('flex-basis', '600px');
  await expect.poll(() => slot.evaluate(el => el.getAnimations().length)).toBe(0);
  await drag(page, 'Resize side panel', -90);
  await expect(slot).toHaveCSS('flex-basis', '690px');
  expect(await slot.evaluate(el => el.getAnimations().length)).toBe(0);

  // Freeze only sidebar transitions so their first and intermediate frames can be inspected.
  await page.evaluate(() => {
    const animate = Element.prototype.animate;
    Element.prototype.animate = function(frames, options) {
      const animation = animate.call(this, frames, options);
      if (animation.id.startsWith('molr-chat-panel')) { animation.pause(); animation.currentTime = 0; }
      return animation;
    };
  });
  const switchTo = async (kind: string) => {
    await page.evaluate(hash => { location.hash = hash; }, `/assistants/${assistant.ID}?panel=${kind}`);
    await expect(slot).toHaveAttribute('data-kind', kind);
  };
  const endpoints = () => slot.evaluate(el => {
    const animation = el.getAnimations().find(a => a.id === 'molr-chat-panel-size');
    return (animation?.effect as KeyframeEffect)?.getKeyframes().map(frame => Number.parseFloat(String(frame.flexBasis)));
  });
  const finish = async () => {
    await slot.evaluate(el => { for (const animation of el.getAnimations({ subtree: true })) if (animation.id.startsWith('molr-chat-panel')) animation.finish(); });
    await expect.poll(() => slot.evaluate(el => el.getAnimations().length)).toBe(0);
  };
  await switchTo('tasks');
  await expect.poll(endpoints).toEqual([690, 480]);
  await slot.evaluate(el => { for (const animation of el.getAnimations({ subtree: true })) if (animation.id.startsWith('molr-chat-panel')) animation.currentTime = Number(animation.effect!.getTiming().duration) / 2; });
  const intermediate = await width(page, '.chat-side-panel');
  expect(intermediate).toBeGreaterThan(480);
  expect(intermediate).toBeLessThan(690);
  await switchTo('info');
  const reversed = await endpoints();
  expect(Math.abs(reversed[0] - intermediate)).toBeLessThan(1);
  expect(reversed[1]).toBe(690);
  await finish();

  await switchTo('tasks');
  await finish();
  await drag(page, 'Resize side panel', -210);
  await expect(slot).toHaveCSS('flex-basis', '690px');
  expect(await slot.evaluate(el => el.getAnimations().length)).toBe(0);
  await switchTo('info');
  await expect.poll(endpoints).toEqual([690, 690]);
  expect(await slot.evaluate(el => el.getAnimations().some(a => a.id === 'molr-chat-panel-content'))).toBe(true);
  await finish();

  await page.emulateMedia({ reducedMotion: 'reduce' });
  await switchTo('tasks');
  await expect(slot).toHaveCSS('flex-basis', '690px');
  expect(await slot.evaluate(el => el.getAnimations({ subtree: true }).filter(a => a.id.startsWith('molr-chat-panel')).length)).toBe(0);
});
