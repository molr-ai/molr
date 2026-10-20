import React, { useCallback, useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Minus, Square, Copy, X, ArrowLeft, ArrowRight, Check, CircleHelp, ExternalLink, Monitor, Plus, RefreshCw, Server, Settings2, Trash2 } from 'lucide-react';
import { Select, Badge, Button, IconButton, Logo, TextField, Switch, ThemeSelect, PaletteSelect, UIProvider, ToastProvider, useToast, useTheme, usePalettePreference, paletteAssetBase } from '@molr/ui';
import type { Connection, ConnectionInput, DesktopAPI, DesktopState, LocalAction, ResourcePreset } from '../shared';
import '@molr/ui/styles.css';
import './style.css';

declare global { interface Window { molrDesktop?: DesktopAPI } }
const bridge = window.molrDesktop;
const initial: DesktopState = { connections: [], view: 'manager', local: { available: false, installed: false, state: 'unavailable' }, version: '', platform: '', window: { maximized: false, fullscreen: false } };
const actionNames: Record<LocalAction, string> = { install: '安装并启动', start: '启动', stop: '停止', restart: '重启', upgrade: '更新本机 Runtime', uninstall: '卸载 Runtime' };
const stateNames: Record<string, string> = { active: '运行中', running: '运行中', inactive: '已停止', failed: '需要处理', installed: '已安装', installing: '安装未完成', uninstalled: '已卸载，数据已保留', missing: '尚未安装', unavailable: '仅远程连接', activating: '启动中', deactivating: '停止中' };
function App() {
  const [state, setState] = useState(initial);
  const [form, setForm] = useState<ConnectionInput>();
  const { toast, dismiss } = useToast();
  const lastError = useRef<number | undefined>(undefined);
  const clearError = useCallback(() => { if (lastError.current !== undefined) dismiss(lastError.current); lastError.current = undefined; }, [dismiss]);
  const reportError = useCallback((error: unknown) => {
    clearError();
    const message = (error instanceof Error ? error.message : String(error)).replace(/^Error invoking remote method '[^']+': Error: /, '');
    lastError.current = toast({ tone: 'danger', title: '操作失败', description: message });
  }, [clearError, toast]);
  const [pending, setPending] = useState(false);
  const [settings, setSettings] = useState(false);
  const [resourcePreset, setResourcePreset] = useState<ResourcePreset>('permissive');
  const { theme, preference, changePreference } = useTheme();
  const { palette, changePalette } = usePalettePreference('molr-desktop-palette', theme);
  useEffect(() => {
    if (!bridge) return;
    let mounted = true;
    const dispose = bridge.subscribe(s => { if (mounted) setState(s); });
    void bridge.state().then(s => { if (mounted) setState(s); }).catch(e => { if (mounted) reportError(e); });
    return () => { mounted = false; dispose(); };
  }, [reportError]);
  useEffect(() => { if (state.local.error) reportError(state.local.error); }, [state.local.error, reportError]);
  async function run(fn: () => Promise<unknown>) {
    setPending(true); clearError();
    try { await fn(); } catch (e) { reportError(e); }
    finally { setPending(false); }
  }
  const disabled = pending || !!state.busy || !bridge;
  const current = state.connections.find(c => c.id === state.active);
  const showManager = state.view === 'manager';
  const local = state.local;
  const runtimeReady = local.state === 'active' || local.state === 'running';
  const connect = (c: Connection) => void run(() => bridge!.connect(c.id));
  function localAction(action: LocalAction) { void run(() => bridge!.localAction(action, action === 'install' && local.state !== 'uninstalled' ? resourcePreset : undefined)); }
  return <div className="desktop-app" data-app-platform={state.platform} data-app-fullscreen={state.window.fullscreen}>
    {state.view !== 'runtime' && <header className="desktop-toolbar">
      <div className="desktop-identity"><Logo animation="daily" assetBaseUrl={paletteAssetBase(palette)} variant={theme === 'dark' ? 'light' : 'dark'} /><span>{current?.name ?? '桌面工作台'}</span></div>
      <div className="desktop-toolbar-actions">
        {!showManager && <Button variant="ghost" size="sm" onClick={() => void run(() => bridge!.manager())}><ArrowLeft size={16} /> 管理连接</Button>}
        {showManager && <IconButton label="桌面设置" onClick={() => setSettings(!settings)}><Settings2 size={18} /></IconButton>}
        {bridge && state.platform !== 'darwin' && <div className="desktop-window-controls">
          <IconButton label="最小化" onClick={() => void run(() => bridge.minimize())}><Minus size={16} /></IconButton>
          <IconButton label={state.window.maximized || state.window.fullscreen ? '还原窗口' : '最大化'} onClick={() => void run(() => bridge.toggleMaximize())}>{state.window.maximized || state.window.fullscreen ? <Copy size={14} /> : <Square size={14} />}</IconButton>
          <IconButton label="关闭窗口" className="desktop-close" onClick={() => void run(() => bridge.close())}><X size={18} /></IconButton>
        </div>}
      </div>
    </header>}
    {state.view === 'runtime' ? <div className="desktop-runtime-placeholder" aria-label="Runtime 页面" /> :
      <main className="desktop-main">
        {!bridge && <div className="desktop-notice" role="status"><CircleHelp size={18} /><span>这是桌面管理界面的浏览器预览。连接和本机服务管理请在 Molr 桌面应用中使用。</span></div>}
        {!showManager ? <section className="desktop-recovery">
          <Logo animation={state.view === 'loading' ? 'loading' : 'idle'} assetBaseUrl={paletteAssetBase(palette)} variant={theme === 'dark' ? 'light' : 'dark'} />
          <h1>{state.view === 'loading' ? '正在连接' : '暂时无法连接'}</h1>
          <p>{state.error ?? current?.url}</p>
          <div className="desktop-actions">
            {state.view === 'failed' && current && <Button disabled={disabled} onClick={() => connect(current)}><RefreshCw size={16} />重新连接</Button>}
            <Button variant="secondary" onClick={() => void run(() => bridge!.manager())}>返回连接列表</Button>
          </div>
        </section> : <>
          <div className="desktop-intro"><div><p className="desktop-eyebrow">你的助理，从这里开始</p><h1>连接你的 Runtime</h1><p>选择助理运行的位置，接着完成你的工作。</p></div><Button disabled={disabled} onClick={() => { setForm({ name: '', url: '', allowHTTP: false }); clearError(); }}><Plus size={17} />添加连接</Button></div>
          {settings && <section className="desktop-settings" aria-label="桌面设置">
            <h2>桌面设置</h2><div className="desktop-settings-fields"><ThemeSelect value={preference} onValueChange={changePreference} /><PaletteSelect locale="zh" palette={palette} onChange={changePalette} /></div>
            <p>关闭窗口或退出桌面应用后，本机 Runtime 继续运行。</p>
            <p>桌面版本 {state.version || '开发预览'} · {state.platform || '浏览器'}</p>
            <Button variant="secondary" disabled={!bridge} onClick={() => void run(() => bridge!.openDownloads())}><ExternalLink size={16} />查看官网发行版本</Button>
          </section>}
          {form && <form className="desktop-connection-form" onSubmit={event => { event.preventDefault(); void run(async () => { await bridge!.save(form); setForm(undefined); }); }}>
            <h2>{form.id ? '编辑连接' : '添加远程连接'}</h2>
            <div className="desktop-form-fields"><TextField label="连接名称" value={form.name} maxLength={80} required autoFocus placeholder="例如：工作服务器" onChange={e => setForm({ ...form, name: e.target.value })} /><TextField label="Runtime 地址" value={form.url} type="url" required placeholder="https://molr.example.com" onChange={e => setForm({ ...form, url: e.target.value })} /></div>
            <Switch label="允许可信局域网或回环地址使用 HTTP" checked={form.allowHTTP} onChange={e => setForm({ ...form, allowHTTP: e.target.checked })} />
            <p className="desktop-hint">WSL 也作为远程连接添加。HTTP 连接没有传输加密，请只在可信网络中使用。</p>
            <div className="desktop-actions"><Button type="submit" disabled={disabled} loading={pending}>保存连接</Button><Button variant="ghost" onClick={() => setForm(undefined)}>取消</Button></div>
          </form>}
          {local.available && <section className="desktop-local" aria-label="本机 Runtime">
            <div className="desktop-section-heading"><div className="desktop-section-icon"><Monitor size={24} /></div><div><h2>本机 Runtime</h2><p>{local.installed ? '助理和数据保存在这台电脑上' : '在这台电脑上运行你的助理'}</p></div><Badge tone={runtimeReady ? 'positive' : local.error ? 'danger' : 'neutral'}>{state.busy ? '正在维护' : stateNames[local.state] ?? local.state}</Badge></div>
            {!local.installed && local.state !== 'uninstalled' && <div className="desktop-settings-fields"><Select label="资源权限预设" value={resourcePreset} onValueChange={setResourcePreset} disabled={disabled} options={[{value:'permissive',label:'宽松预设（个人部署推荐）'},{value:'least-privilege',label:'最小权限预设'}]} /><p className="desktop-hint">{resourcePreset === 'permissive' ? '在运行账户的系统权限范围内，允许接入任意本机目录、网络及计算资源，通过 Molr 配置和分配权限。助理仍需单独授权。' : '默认不开放本机目录、网络访问或命令执行资源。随后可按需调整部署配置，再为助理授权。'}</p></div>}
            {local.state === 'uninstalled' && <p className="desktop-hint">重新安装会沿用保留的账号、数据和资源权限配置。</p>}
            {local.installed && <p className="desktop-hint">版本 {local.version} · {local.health === 'verified' ? '安装器能力验证已通过' : local.health === 'verification_pending' ? '请登录 Runtime 检查能力状态' : runtimeReady ? '请登录 Runtime 检查能力状态' : '启动后可继续使用'}</p>}
            <div className="desktop-actions">
              {!local.installed ? <Button disabled={disabled} loading={state.busy === 'install'} onClick={() => localAction('install')}>安装并启动<ArrowRight size={16} /></Button> : <>
                <Button disabled={disabled} onClick={() => { const c = state.connections.find(c => c.kind === 'managed-local'); if (runtimeReady && c) connect(c); else localAction('start'); }}>{runtimeReady ? '打开本机 Runtime' : '启动 Runtime'}<ArrowRight size={16} /></Button>
                {runtimeReady && <><Button variant="secondary" disabled={disabled} onClick={() => localAction('stop')}>停止</Button><Button variant="ghost" disabled={disabled} onClick={() => localAction('restart')}>重启</Button></>}
                {local.bundledVersion && local.bundledVersion !== local.version && <Button variant="secondary" disabled={disabled} onClick={() => localAction('upgrade')}>安装随桌面附带的版本 {local.bundledVersion}</Button>}
                <Button variant="ghost" disabled={disabled} onClick={() => localAction('uninstall')}>卸载 Runtime</Button>
              </>}
              <IconButton label="刷新本机状态" disabled={disabled} onClick={() => void run(() => bridge!.refreshLocal())}><RefreshCw size={16} /></IconButton>
              <Button variant="ghost" disabled={!bridge || !!state.busy} onClick={() => void run(() => bridge!.openLogs())}>维护日志</Button>
            </div>
            {state.busy && <p role="status" className="desktop-hint">{({ checking: '检查安装环境', installing: '安装程序与配置', restarting: '启动服务并验证能力' } as Record<string, string>)[state.progress ?? ''] ?? `正在${actionNames[state.busy as LocalAction] ?? '处理'}`}，完成前请保持桌面应用打开。</p>}
          </section>}
          <section className="desktop-connections" aria-label="远程连接"><div className="desktop-list-heading"><h2>远程连接</h2><span>{state.connections.filter(c => c.kind === 'remote').length} 个连接</span></div>
            {state.connections.filter(c => c.kind === 'remote').length === 0 ? <div className="desktop-empty"><Server size={32} /><h3>连接已有的 Runtime</h3><p>添加服务器、其他电脑或 WSL 中的 Runtime 地址。</p><Button variant="secondary" disabled={disabled} onClick={() => setForm({ name: '', url: '', allowHTTP: false })}><Plus size={16} />添加远程连接</Button></div> : <ul className="desktop-connection-list">
              {state.connections.filter(c => c.kind === 'remote').map(c => <li key={c.id}>
                <span className="desktop-server-icon"><Server size={20} /></span><div className="desktop-connection-info"><h3>{c.name}{c.id === state.lastConnection && <span className="desktop-recent"><Check size={12} />最近使用</span>}</h3><p>{c.url}</p></div>
                <div className="desktop-row-actions"><IconButton label={`编辑 ${c.name}`} disabled={disabled} onClick={() => setForm({ id: c.id, name: c.name, url: c.url, allowHTTP: c.allowHTTP })}><Settings2 size={16} /></IconButton><IconButton label={`移除 ${c.name}`} disabled={disabled} onClick={() => void run(() => bridge!.remove(c.id))}><Trash2 size={16} /></IconButton><Button variant="secondary" size="sm" disabled={disabled} onClick={() => connect(c)}>连接<ArrowRight size={15} /></Button></div>
              </li>)}
            </ul>}
          </section>
          <footer className="desktop-footer"><span>每个连接独立保存登录状态，助理与数据由对应 Runtime 管理。</span>{state.platform === 'win32' && <span>Windows 原生 Runtime · PowerShell / AppContainer</span>}</footer>
        </>}
      </main>}
  </div>;
}
createRoot(document.getElementById('root')!).render(<React.StrictMode><UIProvider locale="zh"><ToastProvider><App /></ToastProvider></UIProvider></React.StrictMode>);
