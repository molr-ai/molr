import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import test from 'node:test';
import { runElectronSuite } from './electron-runner.mjs';

function fixture(t, source) {
  const directory = mkdtempSync(join(tmpdir(), 'molr-electron-runner-'));
  t.after(() => {
    try { process.kill(Number(readFileSync(join(directory, 'helper.pid'), 'utf8')), 'SIGKILL'); }
    catch (error) { if (!['ENOENT', 'ESRCH'].includes(error.code)) throw error; }
    finally { rmSync(directory, { recursive: true, force: true }); }
  });
  const file = join(directory, 'suite.mjs');
  writeFileSync(file, source);
  return { directory, file, options: { diagnostics: directory, timeoutMs: 2000 } };
}

test('Electron runner accepts an exited suite and preserves a failing exit and output', async t => {
  const { directory, file, options } = fixture(t, 'console.log("cleanup complete");');
  await runElectronSuite(file, options);
  writeFileSync(file, 'console.error("synthetic assertion failed"); process.exitCode = 7;');
  await assert.rejects(runElectronSuite(file, options), /exit=7/);
  assert.match(readFileSync(join(directory, 'suite-failure.log'), 'utf8'), /synthetic assertion failed/);
});

test('Electron runner preserves package directory arguments, including spaces', async t => {
  const { file, options } = fixture(t, `
    import assert from 'node:assert/strict';
    assert.deepEqual(process.argv.slice(2), ['installer output/中文 directory']);
  `);
  await runElectronSuite(file, { ...options, args: ['installer output/中文 directory'] });
});

test('Electron runner bounds even a blocked child event loop', async t => {
  const { directory, file, options } = fixture(t, 'console.log("cleanup: blocked event loop"); while (true) {}');
  await assert.rejects(runElectronSuite(file, options), /timed out/);
  assert.match(readFileSync(join(directory, 'suite-failure.log'), 'utf8'), /cleanup: blocked event loop/);
});

for (const parentExits of [false, true]) {
  test(`Electron runner kills helper processes when ${parentExits ? 'the parent exits but pipes stay open' : 'cleanup hangs'}`, {
    skip: parentExits && process.platform === 'win32',
  }, async t => {
    const { directory, file, options } = fixture(t, '');
    const heartbeat = join(directory, 'heartbeat');
    const pidFile = join(directory, 'helper.pid');
    const helper = join(directory, 'helper.mjs');
    writeFileSync(helper, `
      import { appendFileSync, writeFileSync } from 'node:fs';
      writeFileSync(${JSON.stringify(pidFile)}, String(process.pid));
      appendFileSync(${JSON.stringify(heartbeat)}, '.');
      setInterval(() => appendFileSync(${JSON.stringify(heartbeat)}, '.'), 25);
      process.send('ready');
    `);
    writeFileSync(file, `
      import { spawn } from 'node:child_process';
      const child = spawn(process.execPath, [${JSON.stringify(helper)}], { stdio: ['ignore', 'inherit', 'inherit', 'ipc'] });
      child.once('message', () => {
        console.log('cleanup: helper still running');
        ${parentExits ? 'process.exit(0);' : 'setInterval(() => {}, 1000);'}
      });
    `);
    await assert.rejects(runElectronSuite(file, options), /timed out/);
    assert.match(readFileSync(join(directory, 'suite-failure.log'), 'utf8'), /cleanup: helper still running/);
    await delay(100);
    const stopped = readFileSync(heartbeat, 'utf8');
    await delay(150);
    assert.equal(readFileSync(heartbeat, 'utf8'), stopped, 'The helper must stop, not just its parent');
  });
}
