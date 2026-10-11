// Native package acceptance. Requires systemd user delegation and sandbox dependencies.
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { createServer } from 'node:net';
const archive = resolve(process.env.MOLR_TEST_ARCHIVE || process.argv[2] || '');
const version = JSON.parse(execFileSync('tar', ['-xOf', archive, 'molr/release.json'], { encoding: 'utf8' })).version;
const directory = mkdtempSync(`${tmpdir()}/molr-release-test-`), name = `molr-release-test-${process.pid}`;
const probe = createServer(); await new Promise(r => probe.listen(0, '127.0.0.1', r));
const port = probe.address().port; await new Promise(r => probe.close(r));
const env = { ...process.env, XDG_CONFIG_HOME: `${directory}/config`, XDG_DATA_HOME: `${directory}/data`, XDG_CACHE_HOME: `${directory}/cache` };
const run = (cmd, args) => execFileSync(cmd, args, { env, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] });
try {
  // The package installer owns paths and the isolated service. No existing instance is touched.
  run('bash', ['scripts/install.sh', '--archive', archive, '--name', name, '--program-root', `${directory}/program`, '--port', String(port), '--public-host', '127.0.0.1', '--skip-dependencies', '--no-browser', '--no-start']);
  // Locate by the actual receipt, rather than assuming the installer's instance layout.
  const find = (path, filename) => run('find', [path, '-name', filename, '-type', 'f']).trim().split('\n').filter(Boolean);
  const configs = find(`${directory}/config`, 'server.json'); assert.equal(configs.length, 1);
  const binaries = find(`${directory}/program`, 'molr-server'); assert.equal(binaries.length, 1);
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
  const unit = `${directory}/config/systemd/user/${name}.service`;
  run('systemctl', ['--user', 'enable', unit]);
  const start = async () => {
    run('systemctl', ['--user', 'restart', `${name}.service`]);
    for (let i = 0; i < 60; i++) {
      try { if ((await fetch(`http://127.0.0.1:${port}/`)).ok) return; } catch {}
      await new Promise(r => setTimeout(r, 500));
    }
    throw new Error('Runtime did not become ready');
  };
  await start();
  const health = doctor();
  assert.equal(health.Sandbox.Enabled, true);
  assert.equal(health.Sandbox.Capabilities.Reason, 'available');
  for (const field of ['Shell', 'ProcessIsolation', 'FileIsolation', 'NetworkIsolation', 'ResourceLimits']) assert.equal(health.Sandbox.Capabilities[field], true, field);
  assert(run(binary, ['version']).includes(version));
  assert((await fetch(`http://127.0.0.1:${port}/`)).ok);
  // Reinstall the same final archive: the data and owner must remain intact.
  const before = readFileSync(config);
  run('systemctl', ['--user', 'stop', `${name}.service`]);
  run('bash', ['scripts/install.sh', '--archive', archive, '--name', name, '--program-root', `${directory}/program`, '--port', String(port), '--public-host', '127.0.0.1', '--skip-dependencies', '--no-browser', '--no-start']);
  assert.deepEqual(readFileSync(config), before);
  await start();
  const after = doctor();
  assert.equal(after.Sandbox.Capabilities.Reason, 'available');
  console.log(`Native install/reinstall/sandbox acceptance passed: ${version}`);
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
