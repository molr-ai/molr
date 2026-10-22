// Real Electron IPC and installer subprocesses, with synthetic installer receipts.
// This verifies update orchestration, not native Runtime installation correctness.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { _electron as electron, expect } from '@playwright/test';
import { waitForVisibleWindow } from './window-controls.mjs';
if (process.platform === 'win32') throw new Error('The subprocess fixture requires a Unix runner.');
const root=resolve(import.meta.dirname,'../../..'), directory=mkdtempSync(join(tmpdir(),'molr-update-flow-'));
const payload=join(directory,'payload'), stateFile=join(payload,'state.json'); mkdirSync(join(payload,'bin'),{recursive:true});
const server=createServer((_req,res)=>{res.setHeader('Content-Type','text/html');res.end('<!doctype html><script>window.molrApp.getInfo()</script><h1>Local fixture</h1>');});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
const url=`http://127.0.0.1:${server.address().port}/`;
const write=value=>writeFileSync(stateFile,JSON.stringify(value));
write({version:'0.1.0',upgrades:0,fail:false});
writeFileSync(join(payload,'release.json'),JSON.stringify({version:'0.2.0',commit:'a'.repeat(40),os:process.platform,arch:process.arch==='arm64'?'arm64':'amd64'}));
writeFileSync(join(payload,'bin/molr-setup'),`#!${process.execPath}
const fs=require('node:fs'),path=require('node:path');
const file=path.join(__dirname,'../state.json'),state=JSON.parse(fs.readFileSync(file));
if(process.argv[3]==='upgrade') {state.upgrades++;fs.writeFileSync(file,JSON.stringify(state));if(state.fail){process.stderr.write('Fixture upgrade failed');process.exit(1);}state.version='0.2.0';fs.writeFileSync(file,JSON.stringify(state));}
console.log(JSON.stringify({type:'desktop',installed:true,state:'running',version:state.version,url:${JSON.stringify(url)},missing:[]}));
`,{mode:0o755});
let app;
const env={...process.env,MOLR_DESKTOP_DATA:join(directory,'data'),MOLR_DESKTOP_RUNTIME:payload}; delete env.ELECTRON_RUN_AS_NODE; delete env.MOLR_DESKTOP_DEV_URL;
async function launch(){app=await electron.launch({args:[join(root,'apps/desktop'),...(process.env.MOLR_DESKTOP_TEST_HEADLESS==='1'?['--ozone-platform=headless']:[])],env,chromiumSandbox:true});app.context().setDefaultTimeout(10000);return waitForVisibleWindow(app,await app.firstWindow());}
async function remote(code){return app.evaluate(async({webContents},{url,code})=>webContents.getAllWebContents().find(c=>c.getURL() === url).executeJavaScript(code),{url,code});}
async function ready(manager){await expect.poll(()=>manager.evaluate(()=>window.molrDesktop.state().then(s=>s.view))).toBe('runtime');}
try {
 let manager=await launch();
 await expect(manager.getByRole('article',{name:'这台电脑'}).locator('.desktop-version-notice')).toHaveText('可更新');
 let state=await manager.evaluate(()=>window.molrDesktop.state());
 const id=state.connections.find(c=>c.kind==='managed-local').id;
 await manager.evaluate(id=>window.molrDesktop.connect(id),id); await ready(manager);
 assert.equal((await remote('window.molrApp.getInfo()')).connection.kind,'managed-local');
 assert.equal((await remote('window.molrApp.updates.status()')).local.pending,true);
 assert.equal(JSON.parse(readFileSync(stateFile)).upgrades,0);
 // Deferring persists across process restarts without touching the service.
 await app.close(); manager=await launch(); await ready(manager);
 assert.equal(JSON.parse(readFileSync(stateFile)).upgrades,0);
 await app.evaluate(({dialog})=>{dialog.showMessageBox=async()=>({response:0,checkboxChecked:false});});
 await remote('window.molrApp.updates.upgradeLocal()');
 await expect.poll(()=>manager.evaluate(()=>window.molrDesktop.state().then(s=>s.busy))).toBeUndefined();
 assert.equal(JSON.parse(readFileSync(stateFile)).upgrades,0);
 assert.equal((await remote('window.molrApp.updates.status()')).local.pending,true);
 // Installer failure preserves the pending state and exposes a retry.
 write({version:'0.1.0',upgrades:0,fail:true});
 await app.evaluate(({dialog})=>{dialog.showMessageBox=async()=>({response:1,checkboxChecked:false});});
 await remote('window.molrApp.updates.upgradeLocal()');
 await expect.poll(()=>manager.evaluate(()=>window.molrDesktop.state().then(s=>s.local.error))).toContain('Fixture upgrade failed');
 await manager.getByRole('button',{name:'这台电脑的操作',exact:true}).click();
 await manager.getByRole('menuitem',{name:'管理本机服务'}).click();
 await expect(manager.getByRole('button',{name:'更新本机服务',exact:true})).toBeEnabled();
 state=await manager.evaluate(()=>window.molrDesktop.state()); assert.equal(state.local.updatePending,true); assert.equal(state.local.version,'0.1.0');
 write({version:'0.1.0',upgrades:1,fail:false});
 await manager.evaluate(id=>window.molrDesktop.connect(id),id); await ready(manager);
 await remote('window.molrApp.updates.upgradeLocal()');
 await expect.poll(()=>manager.evaluate(()=>window.molrDesktop.state().then(s=>s.local.version))).toBe('0.2.0'); await ready(manager);
 assert.equal((await remote('window.molrApp.updates.status()')).local.pending,false);
 assert.equal(JSON.parse(readFileSync(stateFile)).upgrades,2);
 // A newer local installation never offers a downgrade to the bundled payload.
 write({version:'0.3.0',upgrades:2,fail:false});
 assert.equal((await remote('window.molrApp.updates.status()')).local.pending,false);
 await remote('window.molrApp.updates.upgradeLocal()');
 await expect.poll(()=>manager.evaluate(()=>window.molrDesktop.state().then(s=>s.local.error))).toContain('没有可安装');
 assert.equal(JSON.parse(readFileSync(stateFile)).upgrades,2);
 console.log('Desktop update flow passed: defer, restart, cancel, failure, retry, reconnect, no downgrade.');
} finally {if(app) await app.close();server.closeAllConnections();await new Promise(r=>server.close(r));rmSync(directory,{recursive:true,force:true});}
