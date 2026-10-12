// Native package acceptance. Requires systemd user delegation and sandbox dependencies.
import assert from 'node:assert/strict';
import { execFileSync, spawn } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, chmodSync, statSync, existsSync } from 'node:fs';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { createServer } from 'node:net';
import { createHash } from 'node:crypto';
const archive = resolve(process.env.MOLR_TEST_ARCHIVE || process.argv[2] || '');
const version = JSON.parse(execFileSync('tar', ['-xOf', archive, 'molr/release.json'], { encoding: 'utf8' })).version;
const directory = mkdtempSync(`${tmpdir()}/molr-release-test-`), name = `molr-release-test-${process.pid}`;
const probe = createServer(); await new Promise(r => probe.listen(0, '127.0.0.1', r));
const port = probe.address().port; await new Promise(r => probe.close(r));
const env = { ...process.env, XDG_CONFIG_HOME: `${directory}/config`, XDG_DATA_HOME: `${directory}/data`, XDG_CACHE_HOME: `${directory}/cache` };
const run = (cmd, args) => execFileSync(cmd, args, { env, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] });
try {
  chmodSync(directory, 0o775);
  const args = ['scripts/install.sh', '--archive', archive, '--name', name, '--program-root', `${directory}/program`, '--port', String(port), '--public-host', '127.0.0.1', '--no-browser', ...(process.env.MOLR_TEST_PREPARE === '1' ? [] : ['--skip-dependencies'])];
  const preview = run('sh', [...args, '--non-interactive', '--dry-run']);
  assert(preview.includes('Dry run complete'));
  assert(!existsSync(`${directory}/program`));
  assert.equal(statSync(directory).mode & 0o777, 0o775);
  const first = run('python3', ['scripts/test-install-interactive.py', 'sh', ...args, '--interactive']);
  assert(first.includes(`Result: verified`), 'Interactive installation must verify Runtime and sandbox');
  assert.equal(statSync(directory).mode & 0o777, 0o755);
  // Locate by the actual receipt, rather than assuming the installer's instance layout.
  const find = (path, filename) => run('find', [path, '-name', filename, '-type', 'f']).trim().split('\n').filter(Boolean);
  const configs = find(`${directory}/config`, 'server.json'); assert.equal(configs.length, 1);
  const binaries = find(`${directory}/program`, 'molr'); assert.equal(binaries.length, 1);
  const binary = binaries[0], config = configs[0];
  const doctor = () => {
    try {
      return JSON.parse(run(binary, ['doctor', '-config', config]));
    } catch (error) {
      // Doctor reports capabilities on stdout even when isolation is unavailable.
      // Print only its reason code, never installer output or authentication data.
      try {
        const reason = JSON.parse(error.stdout).Sandbox?.Capabilities?.Reason;
        if (typeof reason === 'string' && /^[a-z_]{1,80}$/.test(reason)) console.error(`Sandbox capability reason: ${reason}`);
      } catch {}
      const http = String(error.stderr || '').match(/\/api\/[a-z/]+: HTTP [0-9]{3}/);
      if (http) console.error(`Doctor request failed: ${http[0]}`);
      throw error;
    }
  };
  const health = doctor();
  assert.equal(health.Sandbox.Enabled, true);
  assert.equal(health.Sandbox.Capabilities.Reason, 'available');
  for (const field of ['Shell', 'ProcessIsolation', 'FileIsolation', 'NetworkIsolation', 'ResourceLimits']) assert.equal(health.Sandbox.Capabilities[field], true, field);
  assert(run(binary, ['version']).includes(version));
  assert((await fetch(`http://127.0.0.1:${port}/`)).ok);
  // Reinstall the same final archive: the data and owner must remain intact.
  const before = readFileSync(config);
  const second = run('sh', [...args, '--non-interactive', '--repair', '--directory-permissions', 'fix']);
  assert.deepEqual(readFileSync(config), before);
  assert(second.includes(`Result: verified`), 'Non-interactive reinstallation must verify Runtime and sandbox');
  const setup = resolve(binary, '../molr-setup');
  assert(run(setup, ['--non-interactive']).includes('State: installed'));
  const metadata = JSON.parse(run('tar', ['-xOf', archive, 'molr/release.json']));
  const bytes = readFileSync(archive), digest = createHash('sha256').update(bytes).digest('hex');
  // A separate process serves the mirror while the synchronous installer runs.
  const mirror = spawn(process.execPath, ['--input-type=module', '-e', `
    import { createServer } from 'node:http';
    import { readFileSync } from 'node:fs';
    const bytes = readFileSync(process.argv[1]);
    const build = JSON.parse(process.argv[2]);
    const server = createServer((req, res) => {
      if (req.url === '/package.tar.gz') { res.end(bytes); return; }
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ schema: 1, version: build.version, commit: build.commit, channel: 'preview', platforms: [{ os: build.os, arch: build.arch, file: 'package.tar.gz', url: 'http://127.0.0.1:'+server.address().port+'/package.tar.gz', sha256: process.argv[3] }] }));
    });
    server.listen(0, '127.0.0.1', () => process.send(server.address().port));
  `, archive, JSON.stringify(metadata), digest], { stdio: ['ignore', 'ignore', 'pipe', 'ipc'] });
  try {
    const mirrorPort = await new Promise((resolve, reject) => { mirror.once('message', resolve); mirror.once('error', reject); mirror.once('exit', code => reject(new Error('mirror exited '+code))); });
    const updated = run(setup, ['update', '--repair', '--source', `http://127.0.0.1:${mirrorPort}`, '--channel', 'preview', '--non-interactive', '--directory-permissions', 'fix', '--skip-dependencies', '--no-browser']);
    assert(updated.includes('Starting verified package installer'));
    assert(updated.includes('Result: verified'));
    assert.deepEqual(readFileSync(config), before);
  } finally { mirror.kill('SIGTERM'); }
  // Four authentications total: initial install, doctor, repair and update.
  // Verify retained-data reinstallation without starting or authenticating again.
  run(setup, ['uninstall', '--non-interactive']);
  assert(existsSync(config)); assert(!existsSync(binary));
  const retained = run('sh', [...args, '--non-interactive', '--no-start', '--directory-permissions', 'fix']);
  assert(retained.includes('Result: not_started'));
  assert.deepEqual(readFileSync(config), before);
  run(resolve(binary, '../molr-setup'), ['uninstall', '--purge', '--yes']);
  assert(!existsSync(binary));
  assert(!existsSync(config));
  console.log(`Native install/repair/download-update/retained-data reinstall/sandbox/purge acceptance passed: ${version}`);
} catch (error) {
  // Installer output can include one-time links; keep subprocess output out of CI logs.
  console.error('Native release acceptance failed:', error.message.split('\n')[0]);
  throw new Error('Native release acceptance failed');
} finally {
  try { run('systemctl', ['--user', 'stop', `${name}.service`]); } catch {}
  try { run('systemctl', ['--user', 'disable', `${name}.service`]); } catch {}
  // Unit files are under the isolated XDG_CONFIG_HOME; reload after removing them.
  rmSync(directory, { recursive: true, force: true });
  try { run('systemctl', ['--user', 'daemon-reload']); } catch {}
}
