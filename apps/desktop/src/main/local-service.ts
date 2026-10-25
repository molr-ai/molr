import type { AdministratorInput, Connection, LocalAction, LocalStatus, ResourcePreset } from '../shared.js';
import type { LocalRuntime } from '../runtime/local.js';
import type { ConnectionStore } from './connections.js';

type Runtime = Pick<LocalRuntime, 'status' | 'action'>;
type Hooks = {
  changed(): void;
  retire(id: string): Promise<void>;
};

// One owner for status publication, connection reconciliation and maintenance.
// Only unfinished reads are shared; completed observations are never cached for
// a later refresh, and a maintenance operation drains earlier reads first.
export class LocalService {
  status: LocalStatus = { available: true, installed: false, state: 'checking' };
  private reading?: Promise<void>;
  private maintaining?: Promise<void>;
  private retired = new Set<string>();

  constructor(private runtime: Runtime, private store: ConnectionStore, private hooks: Hooks) {}

  refresh(): Promise<void> {
    return this.maintaining ?? this.read();
  }

  private read(): Promise<void> {
    if (this.reading) return this.reading;
    this.reading = (async () => {
      try {
        const status = await this.runtime.status();
        const previous = this.store.data.connections.find(c => c.kind === 'managed-local');
        let current: Connection | undefined;
        if (status.installed && status.url) current = this.store.managed(status.url);
        else if (status.state === 'uninstalled' && previous) this.store.remove(previous.id);
        if (previous && (current && current.id !== previous.id || status.state === 'uninstalled')) {
          this.retired.add(previous.id);
        }
        for (const id of this.retired) { await this.hooks.retire(id); this.retired.delete(id); }
        this.status = status;
      } catch (e) {
        this.status = { ...this.status, state: 'failed', error: (e as Error).message };
      }
      this.hooks.changed();
    })().finally(() => { this.reading = undefined; });
    return this.reading;
  }

  async action(action: LocalAction, options: {
    preset?: ResourcePreset;
    administrator?: AdministratorInput;
    confirm(): Promise<boolean>;
    before(): void;
  }) {
    if (this.maintaining) throw new Error('请等待当前维护操作完成。');
    if (!['install', 'start', 'stop', 'restart', 'upgrade', 'uninstall'].includes(action)) throw new Error('无效的操作。');
    const operation = (async () => {
      await (this.reading ?? (this.status.state === 'checking' ? this.read() : undefined));
      // The displayed snapshot gates the offer. LocalRuntime checks the actual
      // installed version again after confirmation, immediately before execution.
      if (action === 'upgrade' && (!this.status.updatePending || this.status.error)) throw new Error('没有可安装的本机服务更新，请刷新状态后重试。');
      if (!await options.confirm()) return;
      options.before();
      try {
        return await this.runtime.action(action, options.preset, options.administrator);
      } finally {
        // Also reconcile partial failures. Pollers wait for this same final read.
        await this.read();
      }
    })();
    this.maintaining = operation.then(() => {}, () => {});
    try { return await operation; }
    finally { this.maintaining = undefined; }
  }
}
