import { test, expect, type Locator } from '@playwright/test';

async function closeAndCheck(dialog: Locator, button = 'Cancel', removed = true) {
 const animated = (await dialog.getAttribute('class'))?.includes('ui-drawer');
 if (animated) {
  await dialog.evaluate(element => { for (const animation of element.getAnimations()) animation.finish(); });
  await dialog.evaluate(async (element, name) => {
   const contents = element.querySelector('.ui-dialog__body')?.firstElementChild;
   const changed = new Promise<void>(resolve => {
    const observer = new MutationObserver(() => {
     if (!element.isConnected || element.getAttribute('data-state') === 'closed') { observer.disconnect(); resolve(); }
    });
    observer.observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ['data-state'] });
   });
   const buttons = name === 'Close' ? element.querySelectorAll<HTMLButtonElement>(':scope > header button') : element.querySelectorAll('button');
   [...buttons].find(button => (button.getAttribute('aria-label') ?? button.textContent)?.trim() === name)!.click();
   await changed;
   const slide = element.getAnimations().find(animation => animation.id === 'molr-drawer-slide');
   if (contents && !contents.isConnected) throw new Error('Business dialog cleared its contents before exit');
   if (!element.isConnected || !slide) throw new Error('Business dialog lost its exit animation');
   slide.pause(); slide.currentTime = 110;
  }, button);
  await expect(dialog).toHaveAttribute('open');
  await expect(dialog.locator(':scope > .ui-dialog__body')).not.toBeEmpty();
  await dialog.evaluate(element => {
   for (const animation of element.getAnimations()) animation.finish();
   for (const animation of element.parentElement?.querySelector('.ui-modal-backdrop')?.getAnimations() ?? []) animation.finish();
  });
 } else await dialog.getByRole('button', { name: button, exact: true }).click();
 if (removed) await expect(dialog).toHaveCount(0);
 else await expect(dialog).not.toBeVisible();
}

test('business dialogs retain content across settings, resources, plans, memory and chat exits', async ({ page }, info) => {
 test.setTimeout(90_000);
 const errors: string[] = []; page.on('pageerror', error => errors.push(error.message));
 await page.goto('/');
 await page.getByLabel('Username', { exact: true }).fill('owner');
 await page.getByLabel('Password', { exact: true }).fill('webui-e2e-password-2026!');
 await page.getByRole('button', { name: 'Sign in', exact: true }).click();
 await expect(page.locator('.workspace')).toBeVisible();
 // The shared test account may have enabled reduced motion in another test.
 await page.evaluate(async () => { const path = '/@id/@molr/sdk'; const { api } = await import(path); const settings = await api.get('/api/settings'); await api.mutate('/api/settings', { Settings: { ...settings, ReducedMotion: false } }, { idempotencyKey: crypto.randomUUID(), expectedVersion: settings.Version }); });
 await page.reload();
 const dialog = (name: string) => page.getByRole('dialog', { name, exact: true, includeHidden: true });
 for (const [route, name, field] of [
  ['settings/users', 'Add user', 'Username'],
  ['account/security', 'Change password', 'New password'],
  ['plans', 'New plan', 'What needs doing?'],
  ['account/memory', 'New folder', 'Folder name'],
 ] as const) {
  await page.goto('/#/' + route);
  const trigger = page.getByRole('button', { name, exact: true }).first(); await trigger.click();
  await dialog(name).getByLabel(field, { exact: true }).fill('Discarded draft');
  await closeAndCheck(dialog(name)); await expect(trigger).toBeFocused();
 }
 await page.goto('/#/resources');
 await page.getByRole('tab', { name: 'Sandboxes', exact: true }).click();
 await page.getByRole('button', { name: 'Connect sandbox', exact: true }).click();
 await dialog('Connect sandbox').getByLabel('Service URL', { exact: true }).fill('https://sandbox.example.test');
 await closeAndCheck(dialog('Connect sandbox'));
 await page.getByRole('tab', { name: 'Tools', exact: true }).click();
 await page.locator('.tools-catalog-row').first().click();
 const tool = page.locator('.tool-catalog-detail');
 await expect(tool.locator('.tool-catalog-content')).toBeVisible();
 await closeAndCheck(tool, 'Close', false);
 await expect(tool.locator('.tool-catalog-content')).toHaveCount(0);
 // Verification is a separate nested modal; cancelling it must preserve its parent.
 await page.goto('/#/account/security');
 await page.getByRole('button', { name: 'Change password', exact: true }).click();
 await dialog('Change password').getByLabel('New password', { exact: true }).fill('unused-new-password-fixture');
 await dialog('Change password').getByRole('button', { name: 'Save', exact: true }).click();
 await expect(dialog('Verify identity').getByLabel('Runtime sign-in password', { exact: true })).toBeVisible();
 await closeAndCheck(dialog('Verify identity'));
 await expect(dialog('Change password').getByLabel('New password', { exact: true })).toHaveValue('unused-new-password-fixture');
 await closeAndCheck(dialog('Change password'));
 const assistant = await page.evaluate(async name => {
  const path = '/@id/@molr/sdk'; const { api } = await import(path);
  const origin = 'https://models.example.test';
  const resource = await api.mutate('/api/resources', { Name: name, Kind: 'model', Provider: 'openai-compatible', Binding: { Endpoint: origin, AllowedOrigins: [origin], Models: [{ Name: name, ContextLimit: 32768, SupportsTools: true, Pricing: { Currency: 'USD', InputMicrosPerMillion: 0, OutputMicrosPerMillion: 0, CachedInputMicrosPerMillion: 0 } }] } }, { idempotencyKey: crypto.randomUUID() });
  const models = await api.get('/api/models');
  return api.mutate('/api/assistants', { Name: name, ModelID: models.find((model: { ResourceID: string }) => model.ResourceID === resource.ID).ID, Budget: { Limit: { Currency: 'USD', Micros: 0 }, Period: 'daily' } }, { idempotencyKey: crypto.randomUUID() });
 }, `Modal lifecycle ${info.project.name}`);
 // A narrow desktop also uses the modal side panel; docked layout has separate regression coverage.
 await page.setViewportSize({ width: info.project.name === 'mobile' ? 393 : 1100, height: 900 });
 await page.goto(`/#/assistants/${assistant.ID}`);
 const infoTrigger = page.getByRole('button', { name: `View information for ${assistant.Name}`, exact: true });
 await infoTrigger.click();
 const panel = page.locator('.assistant-info');
 await expect(panel.getByRole('textbox', { name: 'Assistant name', exact: true })).toBeVisible();
 await closeAndCheck(panel, 'Close');
 await expect(infoTrigger).toBeFocused();
 await page.locator('.chat-header').getByRole('button', { name: 'Plans', exact: true }).click();
 await expect(page.locator('.assistant-plans .plans-workspace')).toBeVisible();
 await closeAndCheck(page.locator('.assistant-plans'), 'Close');
 if (info.project.name === 'desktop') {
  await page.setViewportSize({ width: 1800, height: 1100 });
  await infoTrigger.click();
  const slot = page.locator('.chat-side-panel');
  await expect(panel).toHaveAttribute('data-docked', 'true');
  await expect(panel.getByRole('textbox', { name: 'Assistant name', exact: true })).toBeVisible();
  await slot.evaluate(element => { for (const animation of element.getAnimations({ subtree: true })) animation.finish(); });
  await panel.evaluate(async element => {
   const slot = element.closest('.chat-side-panel')!;
   const closing = new Promise<void>(resolve => {
    const observer = new MutationObserver(() => { if (slot.getAttribute('data-state') === 'closing') { observer.disconnect(); resolve(); } });
    observer.observe(slot, { attributes: true, attributeFilter: ['data-state'] });
   });
   element.querySelector<HTMLButtonElement>(':scope > header button')!.click(); await closing;
   for (const animation of slot.getAnimations({ subtree: true })) { animation.pause(); animation.currentTime = 110; }
  });
  const composer = page.getByRole('textbox', { name: 'Message your assistant', exact: true });
  await composer.fill('Keep focus during the docked exit');
  await slot.evaluate(element => { for (const animation of element.getAnimations({ subtree: true })) animation.finish(); });
  await expect(slot).toHaveCount(0); await expect(composer).toBeFocused();
 }
 expect(errors).toEqual([]);
 await page.evaluate(async () => { const path = '/@id/@molr/sdk'; const { api } = await import(path); await api.post('/api/auth/logout'); });
});
