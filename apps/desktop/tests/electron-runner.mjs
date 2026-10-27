// Keep the deadline outside Electron/Playwright, including teardown and open pipes.
import { spawn, execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { basename, join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const root = resolve(import.meta.dirname, '../../..');

export function runElectronSuite(file, { args = [], timeoutMs = 120_000, diagnostics = join(root, '.cache/acceptance/desktop') } = {}) {
  const name = basename(file, '.mjs');
  mkdirSync(diagnostics, { recursive: true });
  const failureLog = join(diagnostics, `${name}-failure.log`);
  rmSync(failureLog, { force: true });
  console.log(`Electron suite ${name}: starting (timeout ${timeoutMs}ms)`);
  return new Promise((resolve, reject) => {
    const env = { ...process.env };
    // These are standalone scripts, even when this runner is under node --test.
    delete env.NODE_TEST_CONTEXT;
    const child = spawn(process.execPath, [file, ...args], {
      env, detached: process.platform !== 'win32', stdio: ['ignore', 'pipe', 'pipe'],
    });
    let tail = Buffer.alloc(0), finished = false;
    const output = (stream, chunk) => {
      tail = Buffer.concat([tail, chunk]).subarray(-32_768);
      stream.write(chunk);
    };
    child.stdout.on('data', chunk => output(process.stdout, chunk));
    child.stderr.on('data', chunk => output(process.stderr, chunk));
    function killTree() {
      if (!child.pid) return;
      try {
        if (process.platform === 'win32') {
          execFileSync('taskkill', ['/pid', String(child.pid), '/T', '/F'], { stdio: 'ignore', timeout: 5000 });
        } else {
          // Only this suite's new process group, including Electron helpers.
          process.kill(-child.pid, 'SIGKILL');
        }
      } catch (error) {
        if (error.code !== 'ESRCH') console.error(`Electron suite ${name}: process cleanup failed: ${error.message}`);
      }
    }
    function finish(error) {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      process.removeListener('SIGINT', interrupted);
      process.removeListener('SIGTERM', terminated);
      // On Unix, also reap descendants that outlived a successfully exited parent.
      if (error || process.platform !== 'win32') killTree();
      child.stdout.destroy(); child.stderr.destroy(); child.unref();
      if (error) {
        try {
          writeFileSync(failureLog, `${error.message}\n\n${tail}`);
        } catch (diagnosticError) { console.error(`Cannot save Electron diagnostics: ${diagnosticError.message}`); }
        reject(error);
      } else {
        console.log(`Electron suite ${name}: completed, process exited`);
        resolve();
      }
    }
    const interrupted = () => finish(new Error(`Electron suite ${name} interrupted (SIGINT)`));
    const terminated = () => finish(new Error(`Electron suite ${name} interrupted (SIGTERM)`));
    process.once('SIGINT', interrupted);
    process.once('SIGTERM', terminated);
    const timer = setTimeout(() => finish(new Error(`Electron suite ${name} timed out after ${timeoutMs}ms (including cleanup)`)), timeoutMs);
    child.once('error', finish);
    // 'close' also waits for inherited pipes; 'exit' alone can miss leaked helpers.
    child.once('close', (code, signal) => finish(code === 0 ? undefined : new Error(`Electron suite ${name} failed (exit=${code}, signal=${signal})`)));
  });
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const suites = ['electron', ...(process.platform === 'win32' ? [] : ['lifecycle-electron', 'updates-electron'])];
  for (const suite of suites) await runElectronSuite(join(import.meta.dirname, `${suite}.mjs`));
}
