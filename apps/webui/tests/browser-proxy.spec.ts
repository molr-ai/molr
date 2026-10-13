import { test, expect, type Locator } from '@playwright/test';

async function finishMotion(dialog: Locator) {
 await dialog.evaluate(element => { for (const animation of element.getAnimations({subtree:true})) animation.finish(); });
}

async function dismissWithMotion(dialog: Locator, method: 'Cancel' | 'Close' | 'Escape' | 'backdrop') {
 await expect(dialog).toHaveAttribute('data-contained','true');
 await finishMotion(dialog);
 await dialog.evaluate(async (element, method) => {
  const closed=new Promise<void>(resolve=>{
   const observer=new MutationObserver(()=>{
    if (!element.isConnected || element.getAttribute('data-state') === 'closed') { observer.disconnect(); resolve(); }
   });
   observer.observe(document.body,{subtree:true,childList:true,attributes:true,attributeFilter:['data-state']});
  });
  if (method === 'Escape') element.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));
  else if (method === 'backdrop') {
   const backdrop=element.parentElement!.querySelector<HTMLElement>('.ui-modal-backdrop')!;
   backdrop.dispatchEvent(new PointerEvent('pointerdown',{bubbles:true})); backdrop.click();
  } else [...element.querySelectorAll('button')].find(button => (button.getAttribute('aria-label') ?? button.textContent)?.trim() === method)!.click();
  await closed;
  const slide=element.getAnimations().find(animation => animation.id === 'molr-drawer-slide');
  if (!element.isConnected || !slide) throw new Error('Drawer unmounted before its exit animation');
  slide.pause(); slide.currentTime=110;
 },method);
 await expect(dialog).toHaveAttribute('data-state','closed');
 await expect(dialog).toHaveAttribute('open');
 expect(await dialog.evaluate(element => new DOMMatrixReadOnly(getComputedStyle(element).transform).m42)).toBeGreaterThan(0);
 await finishMotion(dialog);
 await expect(dialog).toHaveCount(0);
}


test('administrator saves browser proxy settings, keeps and clears credentials', async ({ page }) => {
 await page.goto('/');
 await page.getByLabel('Username', {exact:true}).fill('owner');
 await page.getByLabel('Password', {exact:true}).fill('webui-e2e-password-2026!');
 await page.getByRole('button',{name:'Sign in',exact:true}).click();
 await expect(page.locator('.workspace')).toBeVisible();
 await page.goto('/#/settings/components');
 const trigger=page.getByRole('button',{name:'Browser settings',exact:true});
 const form=page.getByRole('dialog',{name:'Browser settings',exact:true});
 await expect(page.getByRole('list',{name:'Components list',exact:true})).toBeVisible();
 await expect(page.getByRole('combobox',{name:'Connection mode'})).toHaveCount(0);
 await trigger.click();
 // Cancel discards the draft and returns keyboard focus to the component row.
 await form.getByRole('combobox',{name:'Connection mode'}).click();
 await page.getByRole('option',{name:'Use proxy',exact:true}).click();
 await form.getByLabel('Proxy server',{exact:true}).fill('http://discarded.example.test:8080');
 const viewport=page.viewportSize()!;
 await page.setViewportSize({width:viewport.width>600?393:1440,height:900});
 await expect(form.getByLabel('Proxy server',{exact:true})).toHaveValue('http://discarded.example.test:8080');
 await page.setViewportSize(viewport);
 await expect(form.getByLabel('Proxy server',{exact:true})).toHaveValue('http://discarded.example.test:8080');
 if (viewport.width < 600) await dismissWithMotion(form,'Cancel');
 else await form.getByRole('button',{name:'Cancel',exact:true}).click();
 await expect(form).not.toBeVisible();
 await expect(trigger).toBeFocused();
 await trigger.click();
 await expect(form.getByRole('combobox',{name:'Connection mode'})).toContainText('Direct');

 await expect(form.getByRole('combobox',{name:'Connection mode'})).toBeVisible();
 await form.getByRole('combobox',{name:'Connection mode'}).click();
 await page.getByRole('option',{name:'Use proxy',exact:true}).click();
 await form.getByLabel('Proxy server',{exact:true}).fill('http://proxy.example.test:8080');
 await form.getByRole('combobox',{name:'Proxy authentication'}).click();
 await page.getByRole('option',{name:'Username and password',exact:true}).click();
 await form.getByLabel('Username',{exact:true}).fill('fixture-user');
 await form.getByLabel('Password',{exact:true}).fill('fixture-secret');
 await form.getByLabel('Proxy bypass addresses',{exact:true}).fill('*.example.test, 192.0.2.0/24');
 await form.getByRole('button',{name:'Save',exact:true}).click();
 await expect(form).not.toBeVisible();
 await page.reload();
 await expect(form).not.toBeVisible();
 await trigger.click();
 await expect(form.getByLabel('Proxy server',{exact:true})).toHaveValue('http://proxy.example.test:8080');
 await expect(form.getByLabel('Password',{exact:true})).toHaveValue('');
 const response=await page.request.get('/api/components/browser/settings');
 const stored=await response.json();
 expect(stored.PasswordConfigured).toBe(true);
 expect(JSON.stringify(stored)).not.toContain('fixture-secret');
 await form.getByLabel('Proxy bypass addresses',{exact:true}).fill('*.example.test');
 await form.getByRole('button',{name:'Save',exact:true}).click();
 await expect(form).not.toBeVisible();
 await expect.poll(async()=> (await (await page.request.get('/api/components/browser/settings')).json()).Bypass).toEqual(['*.example.test']);
 await trigger.click();
 await form.getByRole('combobox',{name:'Connection mode'}).click();
 await page.getByRole('option',{name:'Direct',exact:true}).click();
 await form.getByRole('button',{name:'Save',exact:true}).click();
 await expect.poll(async()=> (await (await page.request.get('/api/components/browser/settings')).json()).Mode).toBe('direct');
 expect((await (await page.request.get('/api/components/browser/settings')).json()).PasswordConfigured).toBe(false);
 await expect(form).not.toBeVisible();
 await trigger.click();
 await expect(form.getByRole('combobox',{name:'Connection mode'})).toBeVisible();
 await page.keyboard.press('Escape');
 await expect(form).not.toBeVisible();
 await expect(trigger).toBeFocused();
 if (viewport.width < 600) {
  for (const method of ['Close','Escape','backdrop'] as const) {
   await trigger.click();
   await expect(form.getByRole('combobox',{name:'Connection mode'})).toBeVisible();
   await dismissWithMotion(form,method);
   await expect(trigger).toBeFocused();
  }
  await page.emulateMedia({reducedMotion:'reduce'});
  await trigger.click();
  await expect(form.getByRole('combobox',{name:'Connection mode'})).toBeVisible();
  expect(await form.evaluate(element=>element.getAnimations().length)).toBe(0);
  await form.getByRole('button',{name:'Cancel',exact:true}).click();
  await expect(form).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await page.emulateMedia({reducedMotion:'no-preference'});
  await page.evaluate(async()=>{
   const path='/src/api/client.ts';const {api}=await import(path);
   const settings=await api.get('/api/settings');
   await api.mutate('/api/settings',{Settings:{...settings,ReducedMotion:true}},settings.Version);
  });
  await page.reload();
  await trigger.click();
  await expect(form.getByRole('combobox',{name:'Connection mode'})).toBeVisible();
  expect(await form.evaluate(element=>element.getAnimations().length)).toBe(0);
  await page.keyboard.press('Escape');
  await expect(form).toHaveCount(0);
  await expect(trigger).toBeFocused();
 }
 await page.evaluate(async()=>{const path='/src/api/client.ts';const {api}=await import(path);await api.post('/api/auth/logout');});
});
