// Real Electron + real WebUI/Runtime acceptance. Use an isolated initialized
// Runtime and a fresh, test-only login link; never consume a delivery link.
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, rmSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { _electron as electron, chromium, expect } from '@playwright/test';
const root = resolve(import.meta.dirname, '../../..');
const url = process.env.MOLR_DESKTOP_WEBUI_URL;
const linkFile = process.env.MOLR_DESKTOP_TEST_LINK;
if (!url || !linkFile) throw new Error('Set MOLR_DESKTOP_WEBUI_URL and MOLR_DESKTOP_TEST_LINK (JSON with value from a test login-link issue).');
const token = JSON.parse(readFileSync(linkFile, 'utf8')).value;
const data = mkdtempSync(join(tmpdir(), 'molr-app-mode-'));
const screenshots = join(root, '.cache/acceptance/app-mode'); mkdirSync(screenshots, { recursive: true });
const env = { ...process.env, MOLR_DESKTOP_DATA: data }; delete env.ELECTRON_RUN_AS_NODE; delete env.MOLR_DESKTOP_RUNTIME; delete env.MOLR_DESKTOP_DEV_URL;
let app, browser;
try {
  app = await electron.launch({ args: [join(root, 'apps/desktop')], env, chromiumSandbox: true });
  const manager = await app.firstWindow();
  await manager.getByRole('heading', { name: '连接你的 Runtime' }).waitFor();
  await expect(manager.locator('.desktop-identity .logo__wordmark')).toBeVisible();
  assert.equal(await manager.locator('.desktop-identity strong').count(), 0);
  await manager.screenshot({path:join(screenshots,'manager.png')});
  await manager.evaluate(async url => { await window.molrDesktop.save({ name: 'App acceptance', url, allowHTTP: true }); const c=(await window.molrDesktop.state()).connections[0]; await window.molrDesktop.connect(c.id); }, url);
  await expect.poll(() => manager.evaluate(async () => (await window.molrDesktop.state()).view), { timeout: 30000 }).toBe('runtime');
  const runtime = app.context().pages().find(page => page.url().startsWith(url));
  assert(runtime, 'Runtime WebContentsView is attached as a Playwright page');
  await runtime.getByRole('banner', { name: /Desktop window|桌面窗口/ }).waitFor();
  assert.equal(await runtime.evaluate(() => typeof window.molrDesktop), 'undefined');
  await runtime.evaluate(() => { localStorage.setItem('molr-webui-locale','zh'); }); await runtime.reload();
  await runtime.getByRole('button', { name: '断开连接', exact: true }).waitFor();
  assert.equal(await manager.locator('.desktop-toolbar').count(), 0);
  await expect(runtime.locator('.app-titlebar .logo__wordmark')).toBeVisible();
  assert.equal(await runtime.locator('.app-titlebar strong').count(), 0);
  assert(await runtime.locator('.app-frame').evaluate(el => getComputedStyle(el).backgroundImage.includes('radial-gradient')));
  await expect(runtime.locator('.login-page')).toHaveCSS('background-image', 'none');
  await expect(runtime.locator('.app-titlebar')).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  // Login view already has controls; the real login-link flow authenticates.
  await runtime.screenshot({path:join(screenshots,'login.png')});
  await runtime.goto(`${url}/#/login?token=${encodeURIComponent(token)}`);
  await runtime.getByRole('button', { name: '确认登录', exact:true }).click();
  await runtime.locator('.workspace').waitFor({ timeout:30000 });
  await runtime.evaluate(() => { location.hash = '#/assistants'; });
  await expect(runtime.locator('.app-titlebar-brand .connection-logo')).toBeVisible();
  assert.equal(await runtime.locator('.workspace-nav .workspace-logo').count(), 0);
  await expect(runtime.locator('.app-titlebar')).toHaveCSS('-webkit-app-region','drag');
  await expect(runtime.locator('.app-titlebar-actions')).toHaveCSS('-webkit-app-region','no-drag');
  assert(await runtime.evaluate(() => Math.abs(document.querySelector('.workspace').getBoundingClientRect().bottom-innerHeight)<2));
  await runtime.locator('.assistant-workspace').waitFor();
  await runtime.locator('.nav-search').click();
  await runtime.locator('dialog[open]').waitFor();
  await expect(runtime.locator('.ui-modal-backdrop')).toHaveCSS('opacity', '1');
  assert.equal(await runtime.locator('dialog[open]').evaluate(el => el.matches(':modal')), false, 'App dialogs share the whole-window scope');
  assert(await runtime.locator('.app-titlebar').evaluate(el => !!el.closest('[inert]')), 'Titlebar is inert while a modal is open');
  const backdrop = await runtime.locator('.ui-modal-backdrop').boundingBox();
  assert(backdrop && backdrop.y === 0 && backdrop.height === await runtime.evaluate(() => innerHeight), 'Backdrop covers titlebar and content');
  await runtime.screenshot({path:join(screenshots,'dialog.png')});
  // The titlebar is ordinary application content: a click there dismisses the
  // modal through its backdrop rather than invoking an underlying window button.
  await runtime.mouse.click(400, 24);
  await runtime.locator('dialog[open]').waitFor({ state:'hidden' });
  assert.equal(await runtime.locator('.app-titlebar').evaluate(el => !!el.closest('[inert]')), false);
  await runtime.getByRole('button', { name:/^(最大化|Maximize)$/, exact:true }).click();
  await expect.poll(() => app.evaluate(({BrowserWindow})=>BrowserWindow.getAllWindows()[0].isMaximized())).toBe(true);
  await runtime.getByRole('button', { name:/^(还原窗口|Restore window)$/,exact:true }).click();
  await expect.poll(() => app.evaluate(({BrowserWindow})=>BrowserWindow.getAllWindows()[0].isMaximized())).toBe(false);
  await runtime.getByRole('button', { name:/^(最小化|Minimize)$/,exact:true }).click();
  await expect.poll(() => app.evaluate(({BrowserWindow})=>BrowserWindow.getAllWindows()[0].isMinimized())).toBe(true);
  await app.evaluate(({BrowserWindow})=>{const w=BrowserWindow.getAllWindows()[0];w.restore();w.show();});
  for(const theme of ['light','dark']) for(const palette of ['forest','ocean','iris','sand','rose','monochrome']) {
    // Exercise shared appearance preferences with the authenticated workspace.
    await runtime.evaluate(({theme,palette})=>{
      for (const [key,value] of [['molr-theme',theme],['molr-webui-palette',palette]]) { localStorage.setItem(key,value); window.dispatchEvent(new StorageEvent('storage',{key,newValue:value})); }
    },{theme,palette});
    await expect(runtime.locator('html')).toHaveAttribute('data-theme',theme);
    await expect(runtime.locator('html')).toHaveAttribute('data-palette',palette);
    for(const [size,width,height] of [['wide',1180,820],['narrow',640,720]]) {
      await app.evaluate(({BrowserWindow},{width,height})=>BrowserWindow.getAllWindows()[0].setSize(width,height),{width,height});
      await expect.poll(()=>runtime.evaluate(()=>innerWidth)).toBe(width);
      await expect.poll(()=>runtime.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
      assert(await runtime.locator('.app-titlebar').evaluate(el => {
        const bar=el.getBoundingClientRect(), center=bar.top+bar.height/2;
        return [...el.querySelectorAll('.app-titlebar-actions button, .app-titlebar-identity > span')].every(item=>{
          const rect=item.getBoundingClientRect(); return Math.abs(rect.top+rect.height/2-center)<1;
        });
      }), 'Titlebar controls and text are vertically centered');
      await runtime.screenshot({path:join(screenshots,`${palette}-${theme}-${size}.png`)});
      await runtime.evaluate(()=>window.dispatchEvent(new Event('molr:search')));
      await runtime.locator('dialog[open]').waitFor();
      await expect(runtime.locator('.ui-modal-backdrop')).toHaveCSS('opacity', '1');
      assert(await runtime.locator('.app-titlebar').evaluate(el=>!!el.closest('[inert]')));
      assert.equal((await runtime.locator('.ui-modal-backdrop').boundingBox()).y,0);
      if(size==='narrow') await expect(runtime.locator('dialog[open]')).toHaveClass(/ui-drawer/);
      await runtime.screenshot({path:join(screenshots,`${palette}-${theme}-${size}-modal.png`)});
      await runtime.locator('dialog[open]').press('Escape');
      await runtime.locator('dialog[open]').waitFor({state:'hidden'});
    }
  }
  await runtime.getByRole('button', {name:/^(断开连接|Disconnect)$/,exact:true}).click({ noWaitAfter: true }).catch(error => { if (!runtime.isClosed()) throw error; });
  await manager.getByRole('heading',{name:'连接你的 Runtime'}).waitFor();
  await manager.evaluate(async()=>window.molrDesktop.connect((await window.molrDesktop.state()).connections[0].id));
  await expect.poll(()=>manager.evaluate(async()=>(await window.molrDesktop.state()).view)).toBe('runtime');
  const reconnected=app.context().pages().find(page=>page.url().startsWith(url));
  await reconnected.locator('.workspace').waitFor();
  assert.equal(await reconnected.evaluate(async()=>(await fetch('/api/auth/me')).status),200,'Disconnect preserves authenticated session');
  // A transport failure during authentication must not remove App controls.
  await reconnected.route('**/api/auth/me',route=>route.abort('failed')); await reconnected.reload();
  await reconnected.getByRole('button',{name:/^(断开连接|Disconnect)$/,exact:true}).waitFor();
  await reconnected.locator('.session-page').waitFor();
  await reconnected.screenshot({path:join(screenshots,'session-error.png')});
  await app.evaluate(({webContents},url)=>webContents.getAllWebContents().find(c=>c.getURL().startsWith(url)).forcefullyCrashRenderer(),url);
  await manager.getByRole('heading',{name:'暂时无法连接'}).waitFor();
  await manager.getByRole('button',{name:'返回连接列表'}).click();
  // A normal browser has no App titlebar and retains the full viewport.
  browser=await chromium.launch({executablePath:process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH,chromiumSandbox:true});
  const web=await browser.newPage(); await web.goto(url); await web.locator('.login-page').waitFor();
  assert.equal(await web.locator('.app-titlebar').count(),0);
  assert.equal(await web.evaluate(()=>typeof window.molrApp),'undefined');
  await web.screenshot({path:join(screenshots,'browser.png')});
  const closed = app.waitForEvent('close');
  await manager.getByRole('button', {name:'关闭窗口',exact:true}).click({noWaitAfter:true}).catch(error=>{ if(!manager.isClosed()) throw error; });
  await closed; app = undefined;
  console.log('App mode passed: full wordmarks, continuous login background, aligned titlebar, whole-window dialog/drawer overlays, native controls, disconnect/session retention, crash recovery, browser mode and 48 appearance/size screenshots.');
} finally { await browser?.close(); await app?.close(); rmSync(data,{recursive:true,force:true}); }
