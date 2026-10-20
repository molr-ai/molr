import { isIP } from 'node:net';
import { randomUUID } from 'node:crypto';
import { readFileSync, writeFileSync, renameSync, mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import type { Connection, ConnectionInput } from '../shared.js';

export function privateHost(host: string) {
  host = host.replace(/^\[|\]$/g, '');
  if (host === 'localhost' || host === '::1') return true;
  if (isIP(host) === 4) {
    const [a, b] = host.split('.').map(Number);
    return a === 127 || a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168);
  }
  return isIP(host) === 6 && /^(fc|fd)/i.test(host);
}
export function normalizeURL(value: unknown, allowHTTP: boolean) {
  if (typeof value !== 'string' || value.length > 2048 || /[\x00-\x20\\]/.test(value.trim())) throw new Error('请输入完整的 Runtime 地址。');
  const url = new URL(value.trim());
  if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password || url.search || url.hash || url.pathname !== '/') throw new Error('地址只能包含协议、主机和端口，不应包含账号、路径或令牌。');
  if (url.protocol === 'http:' && (!allowHTTP || !privateHost(url.hostname))) throw new Error('HTTP 只允许明确启用的回环或私网地址；其他连接请使用 HTTPS。');
  return url.origin;
}
export function externalURL(value: string) {
  try { const u = new URL(value); return ['https:', 'http:'].includes(u.protocol) && !u.username && !u.password && value.length <= 4096; } catch { return false; }
}
export function sameOrigin(value: string, origin: string) {
  try { const u = new URL(value); return ['http:', 'https:'].includes(u.protocol) && !u.username && !u.password && u.origin === origin; } catch { return false; }
}
export function allowedFrameURL(value: string, origin: string, mainFrame: boolean) {
  if (mainFrame) return sameOrigin(value, origin);
  if (value === 'about:blank' || value === 'about:srcdoc') return true;
  try {
    const u = new URL(value);
    return !u.username && !u.password && (['http:', 'https:'].includes(u.protocol) || u.protocol === 'blob:' && u.origin === origin);
  } catch { return false; }
}
export function partition(id: string) {
  if (!/^[a-f0-9-]{36}$/.test(id)) throw new Error('连接标识无效。');
  return `persist:molr-runtime-${id}`;
}
type Saved = { connections: Connection[]; lastConnection?: string };
export class ConnectionStore {
  data: Saved = { connections: [] };
  constructor(private file: string) {
    try {
      const raw = readFileSync(file, 'utf8');
      if (raw.length > 128 * 1024) throw new Error('连接文件过大。');
      const saved = JSON.parse(raw) as Saved;
      if (!Array.isArray(saved.connections) || saved.connections.length > 100) throw new Error('连接文件格式错误。');
      const ids = new Set<string>();
      for (const c of saved.connections) {
        partition(c.id);
        if (ids.has(c.id) || typeof c.name !== 'string' || !c.name.trim() || c.name.length > 80 || !['remote', 'managed-local'].includes(c.kind) || typeof c.allowHTTP !== 'boolean') throw new Error('连接文件格式错误。');
        c.url = normalizeURL(c.url, c.allowHTTP); ids.add(c.id);
      }
      if (saved.lastConnection && !ids.has(saved.lastConnection)) delete saved.lastConnection;
      this.data = saved;
    } catch (error) { if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error; }
  }
  private write(next: Saved) {
    mkdirSync(dirname(this.file), { recursive: true, mode: 0o700 });
    const temp = `${this.file}.${randomUUID()}.tmp`;
    writeFileSync(temp, JSON.stringify(next, null, 2), { mode: 0o600, flag: 'wx' });
    renameSync(temp, this.file); this.data = next;
  }
  get(id: string) { const c = this.data.connections.find(c => c.id === id); if (!c) throw new Error('连接不存在。'); return c; }
  save(input: ConnectionInput) {
    if (!input || typeof input.name !== 'string' || !input.name.trim() || input.name.trim().length > 80 || typeof input.allowHTTP !== 'boolean') throw new Error('请填写连接名称和有效地址。');
    const old = input.id ? this.get(input.id) : undefined;
    if (old?.kind === 'managed-local') throw new Error('本机连接由安装器管理。');
    if (!old && this.data.connections.length >= 100) throw new Error('最多保存 100 个连接。');
    const c: Connection = { id: old?.id ?? randomUUID(), name: input.name.trim(), url: normalizeURL(input.url, input.allowHTTP), kind: 'remote', allowHTTP: input.allowHTTP };
    // A new endpoint must never inherit another endpoint's storage or login state.
    if (old && old.url !== c.url) c.id = randomUUID();
    const connections = old ? this.data.connections.map(x => x.id === old.id ? c : x) : [...this.data.connections, c];
    this.write({ connections, lastConnection: old && this.data.lastConnection === old.id ? c.id : this.data.lastConnection });
    return { connection: c, retired: old && old.id !== c.id ? old.id : undefined };
  }
  managed(url: string) {
    const old = this.data.connections.find(c => c.kind === 'managed-local');
    const c: Connection = { id: old?.id ?? randomUUID(), name: '本机 Runtime', kind: 'managed-local', url: normalizeURL(url, true), allowHTTP: true };
    if (old && old.url !== c.url) c.id = randomUUID();
    this.write({ connections: [...this.data.connections.filter(x => x.kind !== 'managed-local'), c], lastConnection: old && this.data.lastConnection === old.id ? c.id : this.data.lastConnection });
    return c;
  }
  remove(id: string) { this.get(id); this.write({ connections: this.data.connections.filter(c => c.id !== id), lastConnection: this.data.lastConnection === id ? undefined : this.data.lastConnection }); }
  select(id?: string) { if (id !== undefined) this.get(id); this.write({ ...this.data, lastConnection: id }); }
}
