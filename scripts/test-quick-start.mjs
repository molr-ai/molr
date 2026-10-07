// Real embedded WebUI + isolated Runtime + deterministic model, no mocked Molr APIs.
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { cpSync, mkdirSync, mkdtempSync, writeFileSync, chmodSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { resolve, dirname } from 'node:path';
import { createServer } from 'node:http';
import { chromium } from '@playwright/test';
const root = resolve(import.meta.dirname, '..');
const directory = mkdtempSync(`${tmpdir()}/molr-setup-test-`);
const unit = `molr-setup-test-${process.pid}`;
const binarySource = process.env.MOLR_TEST_BINARY || resolve(root, '.cache/release/linux-amd64/molr/bin/molr-server');
cpSync(dirname(binarySource), `${directory}/bin`, { recursive: true });
chmodSync(`${directory}/bin/molr-server`, 0o755);
const binary = `${directory}/bin/molr-server`, configPath = `${directory}/server.json`;
const host = process.env.MOLR_TEST_HOST || '127.0.0.1', port = Number(process.env.MOLR_TEST_PORT || 18878);
const origin = `http://${host}:${port}`;
const config = { data_dir: `${directory}/data`, sandbox: { host_tools: true }, http: { listen: `0.0.0.0:${port}`, public_url: origin, tls_mode: 'lan', requests_per_minute: 3000 }, auth: { login_attempts_per_minute: 100 } };
writeFileSync(configPath, JSON.stringify(config), { mode: 0o600 });
const screenshots = resolve(root, '.cache/acceptance'); mkdirSync(screenshots, { recursive: true });
const requests = [];
const model = createServer(async (req, res) => {
 requests.push(`${req.method} ${req.url}`);
 res.setHeader('Content-Type', 'application/json');
 if (req.url === '/api/tags') return res.end(JSON.stringify({ models: [{ name: 'quick-start-fixture' }] }));
 if (req.url === '/v1/models') return res.end(JSON.stringify({ data: [{ id: 'quick-start-fixture', max_model_len: 32768 }] }));
 if (req.url !== '/v1/chat/completions' || req.method !== 'POST') return res.writeHead(404).end();
 let raw = ''; for await (const part of req) raw += part;
 try {
  const input = JSON.parse(raw), messages = input.messages || [];
  const eventIndex = messages.findLastIndex(m => typeof m.content === 'string' && m.content.startsWith('Runtime event: '));
  const event = eventIndex >= 0 ? JSON.parse(messages[eventIndex].content.split('\n')[0].slice('Runtime event: '.length)) : {};
  const done = messages.slice(eventIndex + 1).some(m => m.role === 'tool');
  const message = { role: 'assistant', content: '' };
  if (!done) message.tool_calls = [{ id: `publish-${Date.now()}`, type: 'function', function: { name: 'conversation_publish', arguments: JSON.stringify({ conversation_id: event.conversation_id, text: 'Molr setup fixture greeting.' }) } }];
  res.end(JSON.stringify({ id: 'setup-fixture', choices: [{ message, finish_reason: done ? 'stop' : 'tool_calls' }], usage: { prompt_tokens: 20, completion_tokens: 10 } }));
 } catch { res.writeHead(400).end(); }
});
let browser;
try {
 await new Promise((resolve, reject) => { model.once('error', reject); model.listen(11434, '127.0.0.1', resolve); });
 execFileSync(binary, ['bootstrap', '-config', configPath], { stdio: 'pipe' });
 execFileSync('systemd-run', ['--user', '--unit', unit, '-p', 'Type=exec', '-p', 'Delegate=yes', '-p', 'DelegateSubgroup=supervisor', '-p', 'KillMode=control-group', binary, 'serve', '-config', configPath], { stdio: 'pipe' });
 let ready = false;
 for (let i = 0; i < 40; i++) { try { const response = await fetch(`${origin}/`); if (response.ok) { ready = true; break; } } catch {} await new Promise(resolve => setTimeout(resolve, 250)); }
 assert(ready, 'Runtime did not become ready');
 const health = JSON.parse(execFileSync(binary, ['doctor', '-config', configPath], { encoding: 'utf8' }));
 assert.equal(health.Sandbox.Capabilities.Reason, 'available');
 const link = execFileSync(binary, ['access', '-config', configPath], { encoding: 'utf8' }).trim();
 browser = await chromium.launch({ headless: true, executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || undefined });
 const context = await browser.newContext({ locale: 'en-US', timezoneId: 'Asia/Shanghai', viewport: { width: 1440, height: 1000 } });
 const page = await context.newPage(), errors = []; page.setDefaultTimeout(20000); page.on('pageerror', error => errors.push(error.message));
 await page.goto(link); console.log('login page loaded');
 await page.getByRole('button', { name: 'Confirm sign-in', exact: true }).click();
 await page.getByRole('heading', { name: 'Get started with Molr' }).waitFor(); console.log('setup page loaded');
 const card = page.locator('.local-provider-result').filter({ hasText: '11434' }); await card.getByRole('button', { name: 'Use', exact: true }).waitFor();
 assert(!requests.some(r => r.startsWith('POST')), 'Discovery performed inference');
 for (const theme of ['light', 'dark']) for (const palette of ['forest', 'ocean', 'iris', 'sand', 'rose', 'monochrome']) {
  await page.evaluate(({ theme, palette }) => { document.documentElement.dataset.theme = theme; document.documentElement.dataset.palette = palette; }, { theme, palette });
  await page.screenshot({ path: `${screenshots}/setup-${palette}-${theme}.png`, fullPage: true });
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Horizontal overflow');
 }
 await page.setViewportSize({ width: 390, height: 844 });
 await page.screenshot({ path: `${screenshots}/setup-mobile.png`, fullPage: true });
 assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Mobile overflow');
 await card.getByRole('button', { name: 'Use', exact: true }).click();
 await page.getByRole('button', { name: 'Connect provider', exact: true }).click(); console.log('provider connected');
 await page.getByLabel('Input price', { exact: true }).fill('0');
 await page.getByLabel('Output price', { exact: true }).fill('0');
 // Refresh after provider save must resume without another provider connection.
 await page.reload();
 await page.getByLabel('Input price', { exact: true }).fill('0');
 await page.getByLabel('Output price', { exact: true }).fill('0');
 await page.getByRole('button', { name: 'Start using Molr', exact: true }).click();
 await page.getByText('Molr setup fixture greeting.', { exact: true }).waitFor({ timeout: 30000 });
 await page.reload();
 await page.getByText('Molr setup fixture greeting.', { exact: true }).waitFor();
 const state = await page.evaluate(async () => (await fetch('/api/setup')).json());
 assert.equal(state.Required, false); assert(state.AssistantID);
 const assistants = await page.evaluate(async () => (await fetch('/api/assistants')).json());
 assert.equal((assistants.Items || assistants).filter(a => a.Name === 'Molr').length, 1);
 const again = await page.evaluate(async () => { const csrf = document.cookie.split('; ').find(c => c.startsWith('molr_lan_csrf_')).split('=').slice(1).join('='); return (await fetch('/api/setup', { method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:'{}' })).json(); });
 assert.equal(again.AssistantID, state.AssistantID);
 assert.deepEqual(errors, []);
 console.log(JSON.stringify({ result: 'passed', LAN: host, sandbox: health.Sandbox.Capabilities, discoveryRequests: requests.filter(r => r.startsWith('GET')).length, inferenceRequests: requests.filter(r => r.startsWith('POST')).length, screenshots }));
} catch (error) {
 if (browser) { const page = browser.contexts()[0]?.pages()[0]; if (page) { await page.screenshot({ path:`${screenshots}/setup-failure.png`, fullPage:true }); console.error((await page.locator('body').innerText()).slice(0, 4000)); } }
 throw error;
} finally {
 await browser?.close(); model.closeAllConnections(); await new Promise(resolve => model.close(resolve));
 try { execFileSync('systemctl', ['--user','stop',unit], { stdio:'pipe' }); } catch {}
 try { execFileSync('systemctl', ['--user','reset-failed',unit], { stdio:'pipe' }); } catch {}
 rmSync(directory,{ recursive:true,force:true });
}
