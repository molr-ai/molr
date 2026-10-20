import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { resolve } from 'node:path';
const base = process.env.MOLR_MANAGER_PREVIEW ?? 'http://127.0.0.1:5420/tests/fixtures/manager-preview.html';
const screenshots = resolve(import.meta.dirname, '../../..', '.cache/acceptance/manager'); mkdirSync(screenshots, { recursive: true });
const browser = await chromium.launch({ chromiumSandbox: true, executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || chromium.executablePath() });
try {
 const page = await browser.newPage({ viewport: {width:1180,height:820} });
 await page.goto(`${base}?scenario=empty`);
 await expect(page.getByRole('button', {name:'桌面设置',exact:true})).toHaveCount(0);
 await expect(page.locator('.desktop-identity > span')).toHaveCount(0);
 await expect(page.getByRole('heading', {name:'你想在哪里使用 Molr？'})).toBeVisible();
 await page.getByRole('button',{name:'安装到这台电脑'}).click();
 await page.screenshot({path:resolve(screenshots,'administrator-form.png'),animations:'disabled'});
 await page.getByLabel('密码',{exact:true}).fill('preview-password-2026');
 await page.getByLabel('确认密码',{exact:true}).fill('different-password');
 await page.getByRole('button',{name:'安装并启动'}).click();
 await expect(page.getByText('两次输入的密码不一致。')).toBeVisible();
 await page.getByLabel('确认密码',{exact:true}).fill('preview-password-2026');
 await page.getByRole('button',{name:'安装并启动'}).click();
 await expect(page.getByRole('heading',{name:'选择连接'})).toBeVisible();
 await expect(page.getByRole('article',{name:'这台电脑'})).toHaveCount(1);
 await page.getByRole('button',{name:'添加连接',exact:true}).click();
 await expect(page.getByRole('heading',{name:'连接远程服务'})).toBeVisible();
 await expect(page.getByRole('button',{name:'安装到这台电脑'})).toHaveCount(0);
 await page.getByLabel('服务地址').fill('https://second.example.com');
 await page.getByRole('button',{name:'保存连接'}).click();
 await expect(page.getByRole('article',{name:'second.example.com'})).toBeVisible();
 await page.getByRole('button',{name:'这台电脑的操作',exact:true}).click();
 await expect(page.getByRole('menuitem',{name:'移除连接'})).toHaveCount(0);
 await page.getByRole('menuitem',{name:'管理本机服务'}).click();
 await expect(page.getByRole('button',{name:'卸载本机服务',exact:true})).toHaveClass(/button--danger/);
 await page.screenshot({path:resolve(screenshots,'local-management.png'),animations:'disabled'});
 await page.getByRole('button',{name:'停止服务',exact:true}).click();
 await page.getByRole('button',{name:'返回连接列表'}).click();
 await expect(page.getByRole('button',{name:'启动并连接'})).toBeVisible();
 await page.getByRole('button',{name:'这台电脑的操作',exact:true}).click();
 await page.getByRole('menuitem',{name:'管理本机服务'}).click();
 await page.getByRole('button',{name:'卸载本机服务',exact:true}).click();
 await expect(page.getByRole('article',{name:'这台电脑'})).toHaveCount(0);
 for (const scenario of ['update', 'service-newer', 'different', 'unknown', 'list']) {
   await page.goto(`${base}?scenario=${scenario}`);
   const card = page.getByRole('article', {name:'这台电脑'});
   await expect(card.getByRole('button', {name:'连接', exact:true})).toBeVisible();
   await expect(page.getByRole('article', {name:'工作服务器'}).locator('.desktop-version-notice')).toHaveCount(0);
   if (scenario === 'unknown' || scenario === 'list') { await expect(card.locator('.desktop-version-notice')).toHaveCount(0); continue; }
   await expect(card.locator('.desktop-card-description .desktop-version-notice.badge')).toHaveText(scenario === 'update' ? '可更新' : '版本不一致');
   await expect(card.getByRole('button', {name:/^(可更新|版本不一致)$/})).toHaveCount(0);
   await card.getByRole('button', {name:'这台电脑的操作',exact:true}).click();
   await page.getByRole('menuitem',{name:'管理本机服务'}).click();
   await expect(page.getByRole('heading',{name:'管理本机服务'})).toBeVisible();
   await expect(page.locator('.desktop-version-details > div').filter({has:page.getByText('桌面应用附带',{exact:true})}).locator('dd')).toHaveText('0.2.0');
   if (scenario === 'update') {
     await expect(page.getByText('可从 0.1.0 更新至 0.2.0。', {exact:false})).toBeVisible();
     await page.getByRole('button', {name:'更新本机服务',exact:true}).click();
     await expect(page.getByRole('button', {name:'更新本机服务',exact:true})).toHaveCount(0);
     await page.getByRole('button',{name:'返回连接列表'}).click();
     await expect(card.locator('.desktop-version-notice')).toHaveCount(0);
   } else {
     await expect(page.getByRole('button', {name:'更新本机服务',exact:true})).toHaveCount(0);
     await expect(page.getByRole('button', {name:'查看桌面应用版本',exact:true})).toHaveCount(0);
   }
 }
 for (const scenario of ['list','empty','stopped','remote','update','service-newer']) for (const theme of ['light','dark']) for (const palette of ['forest','ocean','iris','sand','rose','monochrome']) {
   await page.goto(`${base}?scenario=${scenario}`);
   await page.evaluate(({theme,palette}) => {localStorage.setItem('molr-theme',theme);localStorage.setItem('molr-desktop-palette',palette);},{theme,palette});
   await page.reload(); await expect(page.locator('.desktop-intro h1')).toBeVisible();
   for (const width of [1180,620]) {
     await page.setViewportSize({width,height:820});
     assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
     await page.screenshot({path:resolve(screenshots,`${scenario}-${palette}-${theme}-${width}.png`),animations:'disabled'});
   }
 }
 // Inspect the changed controls and management layout across all appearances.
 for (const theme of ['light','dark']) for (const palette of ['forest','ocean','iris','sand','rose','monochrome']) {
   await page.goto(`${base}?scenario=update`);
   await page.evaluate(({theme,palette}) => {localStorage.setItem('molr-theme',theme);localStorage.setItem('molr-desktop-palette',palette);},{theme,palette});
   await page.reload();
   for (const width of [1180,390]) {
     await page.setViewportSize({width,height:1000});
     const add = page.getByRole('button',{name:'添加连接',exact:true});
     await add.hover();
     await page.screenshot({path:resolve(screenshots,`add-hover-${palette}-${theme}-${width}.png`),animations:'disabled'});
     await page.getByRole('button',{name:'这台电脑的操作',exact:true}).click();
     await page.getByRole('menuitem',{name:'管理本机服务'}).click();
     await page.getByRole('button',{name:'返回连接列表'}).hover();
     assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
     await page.screenshot({path:resolve(screenshots,`management-${palette}-${theme}-${width}.png`),fullPage:true,animations:'disabled'});
     await page.getByRole('button',{name:'返回连接列表'}).click();
   }
 }
 await page.goto(`${base}?scenario=list&platform=darwin`);
 await expect(page.locator('.desktop-toolbar')).toHaveCSS('padding-left','88px');
 await expect(page.locator('.desktop-window-controls')).toHaveCount(0);
 await page.screenshot({path:resolve(screenshots,'macos-safe-area.png'),animations:'disabled'});
 await page.goto(`${base}?scenario=list&platform=darwin&fullscreen=1`);
 await expect(page.locator('.desktop-toolbar')).toHaveCSS('padding-left','16px');
 console.log('Manager UI passed: empty flow, installation, password confirmation, cards, remote-only add, maintenance, and six palettes in both modes/widths.');
} finally {await browser.close();}
