import type { AppAppearance } from '@molr/ui-host/app';
import type { DesktopAPI, DesktopState } from '../../src/shared';
const scenario = new URLSearchParams(location.search).get('scenario') ?? 'list';
const installed = ['list', 'stopped', 'update', 'service-newer', 'different', 'unknown'].includes(scenario);
const state: DesktopState = {
  appearance: { theme: (localStorage.getItem('molr-theme') || 'system') as AppAppearance['theme'], palette: (localStorage.getItem('molr-desktop-palette') || 'forest') as AppAppearance['palette'] },
  connections: [ ...(installed ? [{ id: 'local', name: '这台电脑', url: 'http://127.0.0.1:8787', kind: 'managed-local' as const, allowHTTP: true }] : []), ...(scenario !== 'empty' ? [{ id: 'remote', name: '工作服务器', url: 'https://molr.example.com', kind: 'remote' as const, allowHTTP: false }] : []) ],
  view: 'manager', updates: { comparison: 'unknown', checking: false }, local: { available: true, installed, state: installed ? scenario === 'stopped' ? 'inactive' : 'running' : 'missing', version: scenario === 'service-newer' ? '0.3.0' : scenario === 'different' ? '0.2.0+custom' : scenario === 'unknown' ? undefined : '0.1.0', bundledVersion: ['update', 'service-newer', 'different'].includes(scenario) ? '0.2.0' : '0.1.0' }, version: '交互预览', platform: new URLSearchParams(location.search).get('platform') || 'linux', window: { maximized: false, fullscreen: new URLSearchParams(location.search).get('fullscreen') === '1' },
};
const listeners = new Set<(state: DesktopState) => void>();
function changed() { for (const listener of listeners) listener(structuredClone(state)); }
const bridge: DesktopAPI = {
  state: async () => structuredClone(state), subscribe: callback => { listeners.add(callback); return () => listeners.delete(callback); },
  save: async input => { const url = new URL(input.url); if (url.protocol === 'http:' && !input.allowHTTP) throw new Error('请明确允许 HTTP 连接。'); const c = { ...input, id: input.id ?? crypto.randomUUID(), name: input.name.trim() || url.hostname, url: url.origin, kind: 'remote' as const }; state.connections = [...state.connections.filter(x => x.id !== c.id), c]; changed(); },
  remove: async id => { if (id === 'local') throw new Error('请卸载本机服务。'); state.connections = state.connections.filter(c => c.id !== id); changed(); },
  connect: async id => { state.active = id; state.view = 'failed'; state.error = '这是交互预览。真实连接和安装功能已在桌面应用中实现，请使用桌面应用打开服务。'; changed(); },
  manager: async () => { state.view = 'manager'; state.active = undefined; changed(); },
  localAction: async action => {
    state.busy = action; state.progress = 'installing'; changed(); await new Promise(resolve => setTimeout(resolve, 300));
    if (action === 'install') { state.local.installed = true; state.local.state = 'running'; state.connections.unshift({ id: 'local', name: '这台电脑', url: 'http://127.0.0.1:8787', kind: 'managed-local', allowHTTP: true }); }
    if (action === 'uninstall') { state.local.installed = false; state.local.state = 'uninstalled'; state.connections = state.connections.filter(c => c.id !== 'local'); }
    if (action === 'upgrade') state.local.version = state.local.bundledVersion;
    if (action === 'stop') state.local.state = 'inactive';
    if (action === 'start' || action === 'restart') state.local.state = 'running';
    state.busy = undefined; state.progress = undefined; changed();
  },
  refreshLocal: async () => changed(), openLogs: async () => {}, minimize: async () => {}, toggleMaximize: async () => {}, close: async () => {},
};
Object.assign(window, { molrDesktop: bridge });
await import('../../src/renderer/main');
