import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { LocalRuntime } from '../dist/main/runtime/local.js';

// Exercise actual pipes with characters split across writes. The fixture uses a
// Unix executable script; native Windows installer coverage lives separately.
test('installer pipes preserve UTF-8 across chunk boundaries', { skip: process.platform === 'win32' }, async () => {
  const root = mkdtempSync(join(tmpdir(), 'molr-installer-output-'));
  try {
    mkdirSync(join(root, 'bin'));
    writeFileSync(join(root, 'release.json'), JSON.stringify({
      os: process.platform, arch: process.arch === 'arm64' ? 'arm64' : 'amd64', version: '0.1.0-local',
    }));
    writeFileSync(join(root, 'bin/molr-setup'), `#!/usr/bin/env node
const action = process.argv[3];
const failed = action === 'install';
const text = failed ? '错误：任务 XML 无法切换编码。' : JSON.stringify({type:'desktop',installed:true,state:'running',version:'中文路径 🦦'});
const output = failed ? process.stderr : process.stdout;
(async () => {
  for (const byte of Buffer.from(text)) {
    await new Promise(resolve => output.write(Buffer.from([byte]), resolve));
    await new Promise(resolve => setTimeout(resolve, 5));
  }
  process.exitCode = failed ? 1 : 0;
})();
`, { mode: 0o755 });
    const runtime = new LocalRuntime(root, join(root, 'manager'));
    assert.equal((await runtime.status()).version, '中文路径 🦦');
    await assert.rejects(runtime.action('install'), { message: '错误：任务 XML 无法切换编码。' });
    const log = readFileSync(runtime.logFile, 'utf8');
    assert(log.includes('错误：任务 XML 无法切换编码。'));
    assert(!log.includes('\uFFFD'));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test('administrator credentials use stdin and never enter arguments or logs', { skip: process.platform === 'win32' }, async () => {
  const root = mkdtempSync(join(tmpdir(), 'molr-installer-credentials-'));
  try {
    mkdirSync(join(root, 'bin'));
    writeFileSync(join(root, 'release.json'), JSON.stringify({ os: process.platform, arch: process.arch === 'arm64' ? 'arm64' : 'amd64', version: '0.1.0-local' }));
    writeFileSync(join(root, 'bin/molr-setup'), `#!/usr/bin/env node
const action = process.argv[3];
let input = ''; process.stdin.setEncoding('utf8');
process.stdin.on('data', data => input += data);
process.stdin.on('end', () => {
  if (action === 'install' && (input !== 'pipe-fixture-password' || process.argv.includes(input) || process.argv[process.argv.indexOf('--init-owner') + 1] !== 'my-admin')) {
    process.stderr.write('Credential transport failed'); process.exitCode = 1; return;
  }
  if (action === 'plan' && (input || process.argv.includes('--init-owner'))) { process.exitCode = 1; return; }
  process.stdout.write(JSON.stringify({type:'desktop',installed:action==='install',state:action==='install'?'running':'planned'}));
});
`, { mode: 0o755 });
    const runtime = new LocalRuntime(root, join(root, 'manager'));
    const result = await runtime.action('install', 'permissive', { username: 'my-admin', password: 'pipe-fixture-password' });
    assert.equal(result.installed, true);
    await assert.rejects(runtime.action('install', 'permissive', { username: 'my-admin', password: 'short' }));
    await assert.rejects(runtime.action('start', undefined, { username: 'my-admin', password: 'pipe-fixture-password' }));
  } finally { rmSync(root, { recursive: true, force: true }); }
});

test('a newer installed service cannot be downgraded through desktop actions', { skip: process.platform === 'win32' }, async () => {
  const root = mkdtempSync(join(tmpdir(), 'molr-version-guard-'));
  try {
    mkdirSync(join(root, 'bin'));
    writeFileSync(join(root, 'release.json'), JSON.stringify({ os: process.platform, arch: process.arch === 'arm64' ? 'arm64' : 'amd64', version: '0.2.0' }));
    writeFileSync(join(root, 'bin/molr-setup'), `#!/usr/bin/env node
if (process.argv[3] !== 'status') { process.stderr.write('Unexpected installer mutation'); process.exitCode = 1; }
else process.stdout.write(JSON.stringify({type:'desktop',installed:true,state:'running',version:'0.3.0'}));
`, { mode: 0o755 });
    const runtime = new LocalRuntime(root, join(root, 'manager'));
    await assert.rejects(runtime.action('upgrade'), { message: '本机服务版本较新，请更新桌面应用。' });
    assert.equal((await runtime.status()).version, '0.3.0');
  } finally { rmSync(root, { recursive: true, force: true }); }
});
