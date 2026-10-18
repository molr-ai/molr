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
