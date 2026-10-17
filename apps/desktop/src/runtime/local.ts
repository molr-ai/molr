import { spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { existsSync, readFileSync, writeFileSync, mkdirSync, appendFileSync, lstatSync } from 'node:fs';
import { join, resolve } from 'node:path';
import type { LocalAction, LocalStatus, ResourcePreset } from '../shared.js';

type Receipt = { type: 'desktop'; installed: boolean; state: string; health?: string; version?: string; url?: string; record?: string; loginURL?: string; missing?: string[] };
export class LocalRuntime {
  private name: string;
  private programRoot: string;
  private active = false;
  readonly logFile: string;
  readonly bundledVersion: string;
  constructor(private packageRoot: string, directory: string, private onProgress?: (stage: string) => void) {
    const release = JSON.parse(readFileSync(join(packageRoot, 'release.json'), 'utf8'));
    const goArch = ({ x64: 'amd64', arm64: 'arm64' } as Record<string, string>)[process.arch];
    if (!['linux', 'darwin'].includes(process.platform) || release.os !== process.platform || release.arch !== goArch || typeof release.version !== 'string') throw new Error('附带的 Runtime 与当前平台不匹配。');
    this.bundledVersion = release.version;
    mkdirSync(directory, { recursive: true, mode: 0o700 });
    const identityFile = join(directory, 'instance.json');
    if (!existsSync(identityFile)) writeFileSync(identityFile, JSON.stringify({ name: `molr-desktop-${randomUUID().replaceAll('-', '').slice(0, 16)}` }), { mode: 0o600, flag: 'wx' });
    this.name = JSON.parse(readFileSync(identityFile, 'utf8')).name;
    if (!/^molr-desktop-[a-f0-9]{16}$/.test(this.name)) throw new Error('本机实例记录损坏，请保留数据并检查安装。');
    this.programRoot = resolve(directory, 'program');
    this.logFile = join(directory, 'maintenance.log');
  }
  private async invoke(action: string, preset?: ResourcePreset): Promise<Receipt> {
    const executable = join(this.packageRoot, 'bin', 'molr-setup');
    const stat = lstatSync(executable);
    if (!stat.isFile() || (stat.mode & 0o022) !== 0) throw new Error('Runtime 安装器权限不安全。');
    const args = ['desktop', action, '--name', this.name, '--program-root', this.programRoot, '--json', '--non-interactive', '--no-browser'];
    if (preset) args.push('--resource-preset', preset);
    if (action !== 'status' && action !== 'plan') args.push('--yes');
    return new Promise((accept, reject) => {
      const child = spawn(executable, args, { stdio: ['ignore', 'pipe', 'pipe'], windowsHide: true });
      let stdout = '', stderr = '', partial = '', oversized = false;
      // Installer controls dependencies and privilege policy. No shell or password
      // is passed from a renderer, and stdout can contain a one-time login link.
      child.stdout.on('data', chunk => {
        stdout += chunk;
        if (stdout.length > 1024 * 1024) { oversized = true; child.kill('SIGTERM'); return; }
        partial += chunk;
        const lines = partial.split('\n'); partial = lines.pop()!;
        for (const line of lines) {
          try { const event = JSON.parse(line); if (event.type === 'progress' && typeof event.stage === 'string') this.onProgress?.(event.stage); } catch { /* Final parsing rejects malformed installer output. */ }
        }
      });
      child.stderr.on('data', chunk => { stderr = (stderr + chunk).slice(-16384); });
      child.on('error', reject);
      child.on('close', code => {
        if (stderr && action !== 'status') appendFileSync(this.logFile, `\n${new Date().toISOString()} ${action}\n${stderr}`, { mode: 0o600 });
        if (code !== 0 || oversized) return reject(new Error(oversized ? '安装器输出超过限制。' : stderr.trim().slice(-2500) || `安装器退出：${code}`));
        try {
          const records = stdout.trim().split('\n').filter(Boolean).map(line => JSON.parse(line) as Receipt);
          const result = records.reverse().find(r => r.type === 'desktop');
          if (!result) throw new Error('安装器未返回有效结果。');
          accept(result);
        } catch (error) { reject(error); }
      });
    });
  }
  async status(): Promise<LocalStatus> {
    const r = await this.invoke('status');
    return { available: true, installed: r.installed, state: r.state, health: r.health, version: r.version, url: r.url, record: r.record, bundledVersion: this.bundledVersion };
  }
  async action(action: LocalAction, preset?: ResourcePreset) {
    if (!['install', 'start', 'stop', 'restart', 'upgrade', 'uninstall'].includes(action)) throw new Error('不支持的本机操作。');
    if (preset && (!['permissive', 'least-privilege'].includes(preset) || action !== 'install')) throw new Error('权限预设仅用于首次安装。');
    if (action === 'install' && !preset) preset = 'least-privilege';
    if (this.active) throw new Error('另一项维护操作正在进行。');
    this.active = true;
    try {
      if (action === 'install' || action === 'upgrade') {
        const plan = await this.invoke('plan', preset);
        if (plan.missing?.length) throw new Error(`请先准备系统依赖，再重试：${plan.missing.join('、')}。Linux 完整版需要 systemd 254+、cgroup v2、Bubblewrap、静态 BusyBox、util-linux 和 CA 证书。`);
      }
      return await this.invoke(action, preset);
    } finally { this.active = false; }
  }
}
