import assert from 'node:assert/strict';
import { _electron as electron, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { createServer } from 'node:http';
import { mkdirSync, mkdtempSync, readFileSync, realpathSync, writeFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';

assert.equal(process.platform, 'darwin', 'Run packaged language acceptance on macOS');
const systemLanguage = process.env.MOLR_LANGUAGE_SYSTEM;
const root = realpathSync(process.cwd());
const evidence = join(root, '.cache/language-test'); mkdirSync(evidence, {recursive:true});
const data = mkdtempSync(join(evidence, 'clean-install-'));
const executable = realpathSync(process.env.MOLR_LANGUAGE_APP);
const installer = realpathSync(join(executable, '../../Resources/runtime/bin/molr-setup'));
const profile = join(data, 'desktop-profile');
const events = [], pending = [];
const record = (kind, details) => { events.push({at:new Date().toISOString(), kind, ...details}); writeFileSync(join(data,'events.json'), JSON.stringify(events,null,2), {mode:0o600}); };
let app, phase = 'launch', manager, runtimePage;
function step(value) { phase=value; console.log('Language test:', value); }
const provider = createServer(async (req,res) => {
  const chunks=[];for await (const c of req) chunks.push(c);
  res.setHeader('Content-Type','application/json');
  if(req.url?.endsWith('/models')) return res.end(JSON.stringify({data:[{id:'language-fixture',max_model_len:32768}]}));
  const body = JSON.parse(Buffer.concat(chunks).toString() || '{}');
  const message={role:'assistant',content:'Language test fixture.'};
  if(body.stream) {res.setHeader('Content-Type','text/event-stream');res.end('data: '+JSON.stringify({choices:[{index:0,delta:message,finish_reason:'stop'}],usage:{prompt_tokens:1,completion_tokens:1}})+'\n\ndata: [DONE]\n\n');}
  else res.end(JSON.stringify({choices:[{index:0,message,finish_reason:'stop'}],usage:{prompt_tokens:1,completion_tokens:1}}));
});
await new Promise(resolve=>provider.listen(0,'127.0.0.1',resolve));
const providerOrigin=`http://127.0.0.1:${provider.address().port}`;
const watchdog=setTimeout(()=>{console.error('Timeout:',phase);app?.process().kill('SIGKILL');},240000).unref();
async function snapshot(label,page) {
  const result=await page.evaluate(()=>({language:navigator.language,languages:[...navigator.languages],timezone:Intl.DateTimeFormat().resolvedOptions().timeZone,htmlLang:document.documentElement.lang,languageCookies:document.cookie.split(';').map(c=>c.trim()).filter(c=>c.startsWith('molr_locale_')),headings:[...document.querySelectorAll('h1')].map(x=>x.textContent),startup:window.__languageDiagnosis}));
  record(label,result);return result;
}
try {
  const env={...process.env};for(const key of ['ELECTRON_RUN_AS_NODE','MOLR_DESKTOP_DATA','MOLR_DESKTOP_RUNTIME','MOLR_DESKTOP_DEV_URL','LANGUAGE'])delete env[key];
  app=await electron.launch({executablePath:executable,args:[`--user-data-dir=${profile}`, ...(systemLanguage ? ['-AppleLanguages', `(${systemLanguage})`] : [])],env,chromiumSandbox:true,timeout:60000});
  app.context().setDefaultTimeout(20000);
  const identity=await app.evaluate(({app,Menu})=>({name:app.getName(),version:app.getVersion(),packaged:app.isPackaged,preferredSystemLanguages:app.getPreferredSystemLanguages(),locale:app.getLocale(),systemLocale:app.getSystemLocale(),userData:app.getPath('userData'),menus:Menu.getApplicationMenu()?.items.map(i=>i.label)}));
  assert.equal(identity.userData,profile);record('native-identity',identity);
  assert.equal(identity.name, 'Molr');
  assert.equal(identity.packaged, true);
  const language = (systemLanguage || identity.preferredSystemLanguages[0]).toLowerCase().startsWith('zh') ? 'zh' : 'en';
  assert.deepEqual(identity.menus, language === 'zh' ? ['Molr', '连接', '编辑', '显示'] : ['Molr', 'Connections', 'Edit', 'View']);
  assert.equal(identity.locale.startsWith(language), true);
  record('bundle-runtime-identity',JSON.parse(readFileSync(join(executable,'../../Resources/runtime/release.json'))));
  // Observe only; do not override navigator, preferences, cookies or responses.
  await app.context().addInitScript(()=>{
    if(window.top!==window)return;
    window.__languageDiagnosis={language:navigator.language,languages:[...navigator.languages],languageCookies:document.cookie.split(';').map(c=>c.trim()).filter(c=>c.startsWith('molr_locale_')),langChanges:[]};
    new MutationObserver(()=>window.__languageDiagnosis.langChanges.push({at:performance.now(),lang:document.documentElement.lang})).observe(document,{subtree:true,attributes:true,attributeFilter:['lang']});
  });
  app.context().on('page',page=>{
    page.on('request',request=>{
      const path=new URL(request.url()).pathname;
      if(!['/api/setup','/api/settings'].includes(path))return;
      const body=request.method()==='POST'?request.postDataJSON():null;
      record('page-request',{path,method:request.method(),...(body?{Language:body.Language??body.Settings?.Language,Timezone:body.Timezone??body.Settings?.Timezone}:{})});
    });
    page.on('response',response=>{
      const path=new URL(response.url()).pathname;if(!['/api/setup','/api/settings'].includes(path))return;
      pending.push(response.json().then(body=>record('page-response',{path,status:response.status(),Required:body.Required,Language:body.Language,Timezone:body.Timezone,Version:body.Version})).catch(()=>{}));
    });
  });
  manager=await app.firstWindow();await expect(manager.getByRole('heading',{name:'你想在哪里使用 Molr？',exact:true})).toBeVisible();
  await snapshot('manager-before-install',manager);
  record('fresh-storage',{languageCookies:(await app.context().cookies()).filter(c=>c.name.startsWith('molr_locale_')).map(c=>({name:c.name,value:c.value}))});
  step('install new local Runtime through desktop UI');
  await manager.getByRole('button',{name:'安装到这台电脑',exact:true}).click();
  await manager.getByLabel('密码', {exact:true}).fill('language-fixture-password');
  await manager.getByLabel('确认密码', {exact:true}).fill('language-fixture-password');
  await manager.getByRole('button',{name:'安装并启动',exact:true}).click();
  await expect(manager.getByRole('heading',{name:'选择连接',exact:true})).toBeVisible({timeout:120000});
  await manager.getByRole('button',{name:'连接',exact:true}).click();
  await expect.poll(()=>manager.evaluate(async()=>(await window.molrDesktop.state()).view),{timeout:120000}).toBe('runtime');
  runtimePage=app.context().pages().find(p=>p.url().startsWith('http://127.0.0.1:'));assert(runtimePage);
  step('initial login link and setup gate');
  await snapshot('runtime-before-auth',runtimePage);
  await expect(runtimePage.getByRole('button',{name:/^(确认登录|Confirm sign-in)$/})).toHaveCount(0);
  await expect(runtimePage.locator('.setup-page')).toBeVisible({timeout:30000});
  const initial = await snapshot('setup-before-any-settings-read',runtimePage);
  assert.equal(initial.htmlLang.split('-')[0], language);
  assert.deepEqual(initial.languageCookies, []);
  assert.equal(initial.language.startsWith(language), true);
  const initialSettings=await runtimePage.evaluate(async()=>{const r=await fetch('/api/settings');if(!r.ok)throw new Error('Settings read HTTP '+r.status);return r.json();});
  record('diagnostic-settings-read-before-setup',{Language:initialSettings.Language,Timezone:initialSettings.Timezone,Version:initialSettings.Version});
  const sandbox=await runtimePage.evaluate(async()=>{const r=await fetch('/api/sandbox');if(!r.ok)throw new Error('Sandbox read HTTP '+r.status);return r.json();});
  record('sandbox',{Enabled:sandbox.Enabled,Capabilities:sandbox.Capabilities});
  assert.equal(sandbox.Enabled,true);assert.equal(sandbox.Capabilities.Reason,'available');assert.equal(sandbox.Capabilities.NetworkBackend,'seatbelt-toggle');
  for (const key of ['Shell', 'FileIsolation', 'NetworkIsolation']) assert.equal(sandbox.Capabilities[key], true, key);
  for (const key of ['ProcessIsolation', 'ResourceLimits']) assert.equal(sandbox.Capabilities[key], false, key);
  await runtimePage.screenshot({path:join(data,'setup.png')});
  // Register only a synthetic model; account/setup/language paths remain real.
  await runtimePage.evaluate(async origin=>{
    const current=await fetch('/api/auth/me');const cookieName=current.headers.get('X-Molr-CSRF-Cookie');
    const csrf=document.cookie.split(';').map(c=>c.trim()).find(c=>c.startsWith(cookieName+'='))?.slice(cookieName.length+1);
    const r=await fetch('/api/resources',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':decodeURIComponent(csrf)},body:JSON.stringify({Name:'Language fixture',Kind:'model',Provider:'openai-compatible',Binding:{Endpoint:origin+'/v1/chat/completions',AllowedOrigins:[origin],Models:[{Name:'language-fixture',ContextLimit:32768,SupportsTools:true,Pricing:{Currency:'USD',InputMicrosPerMillion:0,OutputMicrosPerMillion:0,CachedInputMicrosPerMillion:0}}]},Mutation:{IdempotencyKey:crypto.randomUUID()}})});
    if(!r.ok)throw new Error('Fixture registration HTTP '+r.status+' '+await r.text());
  },providerOrigin);
  await runtimePage.reload();await expect(runtimePage.getByRole('button',{name:/^(开始使用|Start using Molr)$/})).toBeVisible();
  await snapshot('setup-immediately-before-submit',runtimePage);
  step('submit first-time configuration');
  await Promise.all([runtimePage.waitForResponse(r=>new URL(r.url()).pathname==='/api/setup'&&r.request().method()==='POST'&&r.status()===200),runtimePage.getByRole('button',{name:/^(开始使用|Start using Molr)$/}).click()]);
  await expect(runtimePage.locator('.workspace')).toBeVisible();
  await snapshot('workspace-after-setup',runtimePage);
  const saved=await runtimePage.evaluate(async()=>{const r=await fetch('/api/settings');if(!r.ok)throw new Error('Settings read HTTP '+r.status);return r.json();});record('diagnostic-saved-settings',{Language:saved.Language,Timezone:saved.Timezone,Version:saved.Version});
  assert.equal(saved.Language, language);
  assert.equal(saved.Timezone, initial.timezone);
  const submitted = events.find(e => e.kind === 'page-request' && e.path === '/api/setup' && e.method === 'POST');
  assert.equal(submitted?.Language, language);
  assert.equal(submitted?.Timezone, initial.timezone);
  await runtimePage.reload();await expect(runtimePage.locator('.workspace')).toBeVisible();assert.equal((await snapshot('workspace-after-reload',runtimePage)).htmlLang.split('-')[0], language);
  await runtimePage.screenshot({path:join(data,'workspace.png')});
  step('verify explicit language choices and saved override');
  await runtimePage.evaluate(() => { location.hash = '#/account/general'; });
  await runtimePage.getByRole('combobox', {name: language === 'zh' ? '语言' : 'Language', exact: true}).click();
  await expect(runtimePage.getByRole('option')).toHaveText(['简体中文', 'English']);
  const changedLanguage = language === 'zh' ? 'en' : 'zh';
  await runtimePage.getByRole('option', {name: changedLanguage === 'zh' ? '简体中文' : 'English', exact: true}).click();
  await expect.poll(() => runtimePage.evaluate(async () => (await (await fetch('/api/settings')).json()).Language)).toBe(changedLanguage);
  await runtimePage.reload();
  await expect(runtimePage.getByRole('combobox', {name: changedLanguage === 'zh' ? '语言' : 'Language', exact: true})).toBeVisible();
  assert.equal((await snapshot('saved-override-after-reload', runtimePage)).htmlLang.split('-')[0], changedLanguage);
  await Promise.allSettled(pending);
  record('complete',{harnessSourceCommit:execFileSync('git',['rev-parse','HEAD'],{encoding:'utf8'}).trim()});
  console.log('Language test passed:',data);
} catch(error) {
  record('failure',{phase,message:error.message});console.error('Failed during',phase,error.message);
  if(runtimePage&&!runtimePage.isClosed())await snapshot('failure-page',runtimePage).catch(()=>{});
  if(manager&&!manager.isClosed()) {
    const s=await manager.evaluate(async()=>await window.molrDesktop.state()).catch(()=>null);
    if(s)record('manager-failure',{view:s.view,error:s.error,localError:s.local?.error});
  }
  process.exitCode=1;
} finally {
  clearTimeout(watchdog);
  if(app){const timer=setTimeout(()=>app.process().kill('SIGKILL'),5000);await app.close().catch(()=>{});clearTimeout(timer);}
  const identityFile=join(profile,'runtime/instance.json');
  if(existsSync(identityFile)) {
    const name=JSON.parse(readFileSync(identityFile)).name;
    try {execFileSync(installer,['desktop','uninstall','--name',name,'--program-root',join(profile,'runtime/program'),'--json','--non-interactive','--no-browser','--yes'],{stdio:['ignore','pipe','pipe'],timeout:60000});record('cleanup',{serviceUninstalled:true});}catch(e){record('cleanup',{serviceUninstalled:false,message:e.message.slice(0,500)});process.exitCode=1;}
  }
  provider.closeAllConnections();await new Promise(resolve=>provider.close(resolve));
}
