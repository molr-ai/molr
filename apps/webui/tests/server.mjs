import { mcpModel, mcpProvider } from './mcp-test-model.mjs';
import { pluginHTTPTestModel, pluginHTTPProvider } from './plugin-http-test-model.mjs';
import { createServer as createHTTPSServer } from 'node:https';
import { profileModel } from './profile-model.mjs';
import { workspaceCLIModel } from './workspace-cli-model.mjs';
// Isolated real Runtime + a deterministic external model adapter. No UI API mocks.
import { spawn } from 'node:child_process';
import { compressionModel } from './compression-model.mjs';
import { responseModel } from './response-model.mjs';
import { permissionModel } from './permission-model.mjs';
import { retryModel } from './retry-model.mjs';
import { expediteModel } from './expedite-model.mjs';
import { randomUUID } from 'node:crypto';
import { existsSync } from 'node:fs';
import { mkdtemp, mkdir, writeFile, rm, chmod, readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const root = fileURLToPath(new URL('../../../', import.meta.url));
const directory = await mkdtemp(join(tmpdir(), 'molr-webui-e2e-'));
const children = [];
const runtimeUnits = [];
function startRuntime(binary, config, suffix) {
  if (process.env.MOLR_E2E_SANDBOX_SERVICE !== '1') return spawn(binary, ['serve', '-config', config], { env, stdio: 'inherit' });
  const unit = `molr-webui-quota-test-${process.pid}-${suffix}`;
  runtimeUnits.push(unit);
  return spawn('/usr/bin/systemd-run', ['--user', '--pipe', '--wait', '--collect', `--unit=${unit}`, '-p', 'Type=exec', '-p', 'Delegate=yes', '-p', 'DelegateSubgroup=supervisor', '-p', 'KillMode=control-group', ...(env.SSL_CERT_FILE ? [`--setenv=SSL_CERT_FILE=${env.SSL_CERT_FILE}`] : []), binary, 'serve', '-config', config], { env, stdio: 'inherit' });
}
const uiPort = Number(process.env.MOLR_TEST_PORT ?? process.env.MOLR_E2E_PORT ?? 5193), runtimePort = Number(process.env.MOLR_TEST_RUNTIME_PORT ?? process.env.MOLR_E2E_RUNTIME_PORT ?? 18881), modelPort = Number(process.env.MOLR_E2E_MODEL_PORT ?? 18882);
const runtimeURL = `http://127.0.0.1:${runtimePort}`, uiURL = `http://127.0.0.1:${uiPort}`;
const env = { ...process.env, CGO_ENABLED: '0', GOTOOLCHAIN: 'local', GOCACHE: process.env.GOCACHE ?? '/tmp/molr-go-build-cache', GOMODCACHE: process.env.GOMODCACHE ?? '/tmp/molr-go-mod-cache', GOPATH: process.env.GOPATH ?? '/tmp/molr-go-path' };
async function command(binary, args, cwd, input) {
  const child = spawn(binary, args, { cwd, env, stdio: ['pipe', 'inherit', 'inherit'] }); child.stdin.end(input);
  await new Promise((resolve, reject) => { child.on('error', reject); child.on('exit', code => code === 0 ? resolve() : reject(new Error(`${binary} exited ${code}`))); });
}
const model = createServer(async (request, response) => {
  if (request.method === 'GET' && request.url === '/workspace-skill.md') { response.end('---\nname: workspace-cli\ndescription: Use the workspace CLI to check its version.\n---\nRun /work/.local/bin/workspace-cli --version with shell_execute.\n'); return; }
  if (request.method === 'GET' && request.url === '/workspace-cli.sh') { response.end('#!/bin/sh\nprintf \"fixture-cli-1\\n\"\n'); return; }
  if (request.method === 'GET' && request.url === '/quota-fixture') { response.end('quota-fixture\n'.repeat(1024)); return; }
  if (request.method === 'GET' && request.url === '/anthropic/v1/models') {
    if (request.headers['x-api-key'] !== 'test-provider-secret' || request.headers['anthropic-version'] !== '2023-06-01' || request.headers.authorization) { response.writeHead(401).end(); return; }
    response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ data: [{ id: 'claude-fixture', max_input_tokens: 200000, max_tokens: 8192 }], has_more: false })); return;
  }
  if (request.method === 'GET' && ['/v1/models', '/private/v1/models'].includes(request.url?.split('?')[0])) {
    if (request.url.startsWith('/private/') && !['Bearer test-provider-secret', 'Bearer rotated-provider-secret'].includes(request.headers.authorization)) { response.writeHead(401).end(); return; }
    response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ data: [{ id: 'discovered-one' }, { id: 'discovered-two' }] })); return;
  }
  if (request.method === 'GET' && request.url === '/openrouter/api/v1/models') {
    if (request.headers.authorization !== 'Bearer test-router-key') { response.writeHead(401).end(); return; }
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify({ data: [{ id: 'fixture/router', context_length: 131072, architecture: { input_modalities: ['text', 'image'], output_modalities: ['text'] }, supported_parameters: ['tools'], top_provider: { max_completion_tokens: 8192 }, pricing: { prompt: '0', completion: '0' } }] })); return;
  }
  let raw = ''; for await (const part of request) raw += part;
  try {
    const input = JSON.parse(raw); if (profileModel(input, response)) return; if (mcpModel(input, response)) return; if (await compressionModel(input, response)) return; if (workspaceCLIModel(input, response)) return; if (pluginHTTPTestModel(input, response)) return; if (responseModel(input, response)) return; if (permissionModel(input, response)) return; if (retryModel(input, response)) return; if (input.model?.startsWith('Expedite ')) { await expediteModel(input, response); return; } const messages = input.messages ?? [];
    const eventIndex = messages.findLastIndex(message => typeof message.content === 'string' && message.content.startsWith('Runtime event: '));
    const event = eventIndex >= 0 ? JSON.parse(messages[eventIndex].content.split('\n')[0].slice('Runtime event: '.length)) : {};
    const recent = messages.slice(eventIndex + 1), toolResults = recent.filter(message => message.role === 'tool');
    const message = { role: 'assistant', content: 'Internal reasoning is not a chat message.' }; let finish = 'stop';
    if (event.conversation_id && input.model?.startsWith('Multi-tool trajectory ') && toolResults.length === 0) {
      const calls = [
        ['conversation_read', { conversation_id: event.conversation_id, limit: 1 }],
        ['conversation_read', { conversation_id: event.conversation_id, limit: 2 }],
        ['conversation_publish', { conversation_id: event.conversation_id, text: 'Reply after multiple tool calls.' }],
      ];
      message.tool_calls = calls.map(([name, args]) => ({ id: randomUUID(), type: 'function', function: { name, arguments: JSON.stringify(args) } }));
      finish = 'tool_calls';
    } else if (event.conversation_id && toolResults.length < 2) {
      const name = toolResults.length ? 'conversation_publish' : 'conversation_read';
      const reply = input.model?.startsWith('Rich text ') ? '## Runtime rich text reply\n\n**Published by the agent** with $E=mc^2$.\n\n> Keep the original source.\n\n| Stage | Minutes |\n| --- | ---: |\n| Walk | 90 |\n| Rest | 30 |\n\n$$\nT = \\frac{6}{4} + 0.5 = 2\n$$\n\n```python\ntotal = 6 / 4 + 0.5\n```' : 'Runtime delivered this reply through conversation_publish.';
      const args = toolResults.length ? { conversation_id: event.conversation_id, text: reply } : { conversation_id: event.conversation_id, limit: 5 };
      message.tool_calls = [{ id: randomUUID(), type: 'function', function: { name, arguments: JSON.stringify(args) } }]; finish = 'tool_calls';
    }
    if (input.model?.startsWith('Turn metrics ')) {
      if (toolResults.length === 1) message.tool_calls.push({ id: randomUUID(), type: 'function', function: { name: 'conversation_publish', arguments: JSON.stringify({ conversation_id: event.conversation_id, text: 'Another message from the same turn.' }) } });
      // Leave a visible message while the final model call is still running.
      if (toolResults.length >= 2) await new Promise(resolve => setTimeout(resolve, 6500));
      const id = randomUUID(); response.setHeader('Content-Type', 'text/event-stream');
      const send = value => response.write('data: ' + JSON.stringify({ id, ...value }) + '\n\n');
      send({ choices: [{ index: 0, delta: { role: 'assistant', content: '' } }] });
      await new Promise(resolve => setTimeout(resolve, 80));
      send({ choices: [{ index: 0, delta: { content: 'Internal ' } }] });
      await new Promise(resolve => setTimeout(resolve, 120));
      send({ choices: [{ index: 0, delta: { content: 'completion.', ...(message.tool_calls ? { tool_calls: message.tool_calls.map((call, index) => ({ ...call, index })) } : {}) }, finish_reason: finish }] });
      send({ choices: [], usage: { prompt_tokens: 30, completion_tokens: 10, prompt_tokens_details: { cached_tokens: 12 } } });
      response.end('data: [DONE]\n\n'); return;
    }
    if (input.model === 'fixture/router') {
      if (request.headers.authorization !== 'Bearer test-router-key' || input.max_tokens !== 8192 || 'store' in input || 'max_completion_tokens' in input) throw new Error('Wrong OpenRouter request');
      for (const previous of messages.filter(item => item.role === 'assistant')) {
        if (previous.reasoning_details?.[0]?.text !== 'Private continuation.' || previous.reasoning_details?.[0]?.signature !== 'fixture-signature' || previous.reasoning_details?.[1]?.data !== 'opaque-fixture') throw new Error('Lost OpenRouter continuation');
      }
      const id = randomUUID(); response.setHeader('Content-Type', 'text/event-stream');
      const send = value => response.write('data: ' + JSON.stringify({ id, ...value }) + '\n\n');
      send({ choices: [{ index: 0, delta: { reasoning_details: [{ type: 'reasoning.text', text: 'Private ', index: 0 }] } }] });
      send({ choices: [{ index: 0, delta: { reasoning_details: [{ type: 'reasoning.text', text: 'continuation.', signature: 'fixture-signature', index: 0 }] } }] });
      send({ choices: [{ index: 0, delta: { reasoning_details: [{ type: 'reasoning.encrypted', data: 'opaque-fixture', index: 1 }] } }] });
      send({ choices: [{ index: 0, delta: { ...message, ...(message.tool_calls ? { tool_calls: message.tool_calls.map((call, index) => ({ ...call, index })) } : {}) }, finish_reason: finish }] });
      send({ choices: [], usage: { prompt_tokens: 30, completion_tokens: 10 } });
      response.end('data: [DONE]\n\n'); return;
    }
    response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ id: randomUUID(), choices: [{ message, finish_reason: finish }], usage: { prompt_tokens: 30, completion_tokens: 10 } }));
  } catch { response.writeHead(400).end(); }
});
await new Promise(resolve => model.listen(modelPort, '127.0.0.1', resolve));
let pluginProvider;
if (process.env.MOLR_E2E_PLUGIN_HTTP === '1' || process.env.MOLR_E2E_MCP === '1') {
  const cert = join(directory, 'plugin-cert.pem'), key = join(directory, 'plugin-key.pem');
  await command('openssl', ['req', '-quiet', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', key, '-out', cert, '-days', '2', '-subj', '/CN=localhost', '-addext', 'subjectAltName=IP:127.0.0.1,DNS:localhost'], directory);
  env.SSL_CERT_FILE = cert;
  pluginProvider = createHTTPSServer({ cert: await readFile(cert), key: await readFile(key) }, process.env.MOLR_E2E_MCP === '1' ? mcpProvider : pluginHTTPProvider);
  await new Promise(resolve => pluginProvider.listen(Number(process.env.MOLR_E2E_PLUGIN_PORT ?? 18886), '127.0.0.1', resolve));
}

await mkdir(join(directory, 'data'));
const config = { debug: { enabled: true }, network: { allowed_private_ips: ['127.0.0.1'] }, data_dir: join(directory, 'data'), auth: { login_attempts_per_minute: 100 }, http: { listen: `127.0.0.1:${runtimePort}`, public_url: runtimeURL, tls_mode: 'local', requests_per_minute: 3000, allowed_origins: [uiURL] } };
if (process.env.MOLR_E2E_GPU_DEVICES) config.sandbox = { host_tools: true, gpu: { enabled: true, devices: JSON.parse(process.env.MOLR_E2E_GPU_DEVICES) } };
await writeFile(join(directory, 'server.json'), JSON.stringify(config), { mode: 0o600 });
const binary = join(directory, 'molr');
await command(process.env.GO_BINARY ?? (existsSync('/snap/go/current/bin/go') ? '/snap/go/current/bin/go' : 'go'), ['build', '-o', binary, './cmd/molr'], resolve(root, 'runtime'));
await chmod(binary, 0o755);
await command(binary, ['init', '-config', join(directory, 'server.json'), '-username', 'owner', '-password-stdin'], directory, 'webui-e2e-password-2026!\n');
children.push(startRuntime(binary, join(directory, 'server.json'), 'primary'));
children.push(spawn(process.execPath, [resolve(root, 'node_modules/vite/bin/vite.js'), ...(process.env.MOLR_E2E_PREVIEW === '1' ? ['preview'] : []), '--host', '0.0.0.0', '--port', String(uiPort)], { cwd: resolve(root, 'apps/webui'), env: { ...env, MOLR_RUNTIME_URL: runtimeURL }, stdio: 'inherit' }));
// A second independent Runtime reproduces same-host, different-port login
// collisions in one browser context. It shares neither accounts nor data.
const secondUIPort = Number(process.env.MOLR_TEST_SECOND_PORT ?? uiPort + 1), secondRuntimePort = Number(process.env.MOLR_TEST_SECOND_RUNTIME_PORT ?? runtimePort + 2);
const secondRuntimeURL = `http://127.0.0.1:${secondRuntimePort}`;
const secondDirectory = join(directory, 'second');
await mkdir(join(secondDirectory, 'data'), { recursive: true });
const secondConfig = join(secondDirectory, 'server.json');
// CIDR/port network fixtures belong to the Linux backend; native macOS uses its network toggle.
await writeFile(secondConfig, JSON.stringify({ ...config, data_dir: join(secondDirectory, 'data'), sandbox: process.platform === 'linux' ? { networks: [
  { name: 'LAN ranges', protocols: ['tcp', 'udp'], addresses: ['192.168.1.0/24', '10.20.0.0/16', 'fd12::/32'], ports: ['80', '443', '8000-8099', '9000-9099'], exclude_addresses: ['192.168.1.10', 'fd12::dead/128'], exclude_ports: ['8080-8089', '9005'] },
  { name: 'Internet', protocols: ['tcp'], addresses: ['0.0.0.0/0', '::/0'], ports: ['80', '443'], exclude_addresses: ['10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', 'fc00::/7', '127.0.0.0/8', '::1/128', '100.64.0.0/10'] },
] } : undefined, http: { ...config.http, listen: `127.0.0.1:${secondRuntimePort}`, public_url: secondRuntimeURL, allowed_origins: [`http://127.0.0.1:${secondUIPort}`] } }), { mode: 0o600 });
await command(binary, ['init', '-config', secondConfig, '-username', 'second-owner', '-password-stdin'], secondDirectory, 'second-webui-e2e-password-2026!\n');
children.push(startRuntime(binary, secondConfig, 'secondary'));
children.push(spawn(process.execPath, [resolve(root, 'node_modules/vite/bin/vite.js'), ...(process.env.MOLR_E2E_PREVIEW === '1' ? ['preview'] : []), '--host', '0.0.0.0', '--port', String(secondUIPort)], { cwd: resolve(root, 'apps/webui'), env: { ...env, MOLR_RUNTIME_URL: secondRuntimeURL }, stdio: 'inherit' }));
let closing = false;
async function close() { if (closing) return; closing = true; for (const unit of runtimeUnits) await command('/usr/bin/systemctl', ['--user', 'stop', unit], directory); for (const child of children) child.kill('SIGTERM'); model.close(); pluginProvider?.close(); await Promise.all(children.map(child => child.exitCode !== null || child.signalCode !== null ? Promise.resolve() : new Promise(resolve => child.once('exit', resolve)))); await rm(directory, { recursive: true, force: true }); process.exit(0); }
process.on('SIGINT', close); process.on('SIGTERM', close);
for (const child of children) child.on('exit', () => { if (!closing) void close(); });
