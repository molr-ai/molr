import React, { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Minus, Square, Copy, X, ArrowLeft, ArrowRight, CircleHelp, Monitor, MoreHorizontal, Plus, RefreshCw, Server } from 'lucide-react';
import { Select, Badge, Button, Card, DropdownMenu, IconButton, Logo, TextField, Switch, UIProvider, ToastProvider, useToast, useTheme, usePalettePreference, paletteAssetBase } from '@molr/ui';
import type { Connection, ConnectionInput, DesktopAPI, DesktopState, LocalAction, ResourcePreset } from '../shared';
import { localVersionState } from '../runtime-version';
import '@molr/ui/styles.css';
import './style.css';

declare global { interface Window { molrDesktop?: DesktopAPI } }
const bridge = window.molrDesktop;
let initial: DesktopState = { appearance: { theme: 'system', palette: 'forest' }, connections: [], view: 'manager', updates: { comparison: 'unknown', checking: false }, local: { available: false, installed: false, state: 'unavailable' }, version: '', platform: '', window: { maximized: false, fullscreen: false } };
let initialLoaded = !bridge;
// Restore host-owned appearance and native safe-area attributes before mounting.
// Old manager-local preferences are only a rendering cache, never authoritative.
if (bridge) {
  try { initial = await bridge.state(); initialLoaded = true; } catch { /* The mounted loading view retries and reports failures. */ }
}
try {
  localStorage.setItem('molr-theme', initial.appearance.theme);
  localStorage.setItem('molr-desktop-palette', initial.appearance.palette);
} catch { /* Appearance still works without browser storage. */ }
document.documentElement.dataset.theme = initial.appearance.theme === 'system' ? (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light') : initial.appearance.theme;
document.documentElement.dataset.palette = initial.appearance.palette;
const stateNames: Record<string, string> = { active: '运行中', running: '运行中', inactive: '已停止', failed: '需要处理', installed: '已安装', installing: '安装未完成', uninstalled: '已卸载', missing: '尚未安装', unavailable: '暂不可用', activating: '启动中', deactivating: '停止中' };
const actionNames: Record<LocalAction, string> = { install: '安装', start: '启动', stop: '停止', restart: '重启', upgrade: '更新', uninstall: '卸载' };
type Page = 'list' | 'add' | 'remote' | 'install' | 'local';
const emptyForm = (): ConnectionInput => ({ name: '', url: '', allowHTTP: false });
function App() {
  const [state, setState] = useState(initial), [loaded, setLoaded] = useState(initialLoaded);
  const [page, setPage] = useState<Page>('list'), [form, setForm] = useState<ConnectionInput>(emptyForm);
  const [pending, setPending] = useState(false), [resourcePreset, setResourcePreset] = useState<ResourcePreset>('permissive');
  const localRefreshPending = useRef(false), localRefreshError = useRef<string | undefined>(undefined);
  const [installError, setInstallError] = useState<string>();
  const [username, setUsername] = useState('owner'), [password, setPassword] = useState(''), [confirmation, setConfirmation] = useState('');
  const { toast, dismiss } = useToast();
  const lastError = useRef<number | undefined>(undefined);
  const clearError = useCallback(() => { if (lastError.current !== undefined) dismiss(lastError.current); lastError.current = undefined; }, [dismiss]);
  const reportError = useCallback((error: unknown) => {
    clearError();
    const message = (error instanceof Error ? error.message : String(error)).replace(/^Error invoking remote method '[^']+': Error: /, '');
    lastError.current = toast({ tone: 'danger', title: '操作失败', description: message });
  }, [clearError, toast]);
  const { theme, changePreference } = useTheme();
  const { palette, changePalette } = usePalettePreference('molr-desktop-palette', theme);
  useLayoutEffect(() => {
    changePreference(state.appearance.theme);
    changePalette(state.appearance.palette);
  }, [state.appearance.theme, state.appearance.palette, changePreference, changePalette]);
  useEffect(() => {
    if (!bridge) return;
    let mounted = true;
    const receive = (next: DesktopState) => { if (mounted) { setState(next); setLoaded(true); } };
    const dispose = bridge.subscribe(receive);
    void bridge.state().then(receive).catch(e => { if (mounted) reportError(e); });
    return () => { mounted = false; dispose(); };
  }, [reportError]);
  useEffect(() => { if (state.local.error) reportError(state.local.error); }, [state.local.error, reportError]);
  async function run(fn: () => Promise<unknown>) {
    setPending(true); clearError();
    try { await fn(); } catch (e) { reportError(e); }
    finally { setPending(false); }
  }
  const disabled = pending || !!state.busy || !bridge;
  const local = state.local, ready = local.state === 'active' || local.state === 'running';
  const versionState = localVersionState(local.version, local.bundledVersion);
  const versionNotice = versionState === 'update' ? '可更新' : versionState === 'service-newer' || versionState === 'different' ? '版本不一致' : undefined;
  const current = state.connections.find(c => c.id === state.active);
  const connections = [...state.connections].sort((a, b) => Number(b.kind === 'managed-local') - Number(a.kind === 'managed-local'));
  const canChoose = local.available && !local.installed;
  const screen = page === 'list' && !connections.length ? (canChoose ? 'add' : 'remote') : page === 'add' && !canChoose ? 'remote' : page === 'local' && !local.installed ? (connections.length ? 'list' : canChoose ? 'add' : 'remote') : page;
  const pollLocal = !!bridge && state.view !== 'runtime' && !state.busy && !pending;
  useEffect(() => {
    if (!pollLocal) return;
    let active = true;
    async function refresh() {
      if (!active || localRefreshPending.current) return;
      localRefreshPending.current = true;
      try {
        await bridge!.refreshLocal();
        localRefreshError.current = undefined;
      } catch (error) {
        const message = String(error);
        if (active && localRefreshError.current !== message) {
          localRefreshError.current = message;
          reportError(error);
        }
      } finally { localRefreshPending.current = false; }
    }
    void refresh();
    const timer = window.setInterval(() => { void refresh(); }, 5000);
    return () => { active = false; window.clearInterval(timer); };
  }, [pollLocal, reportError]);
  const connect = (c: Connection) => void run(() => bridge!.connect(c.id));
  function navigate(next: Page) { clearError(); setPage(next); setPassword(''); setConfirmation(''); setInstallError(undefined); }
  function add() { setForm(emptyForm()); navigate('add'); }
  function localAction(action: LocalAction) { void run(() => bridge!.localAction(action)); }
  function open(c: Connection) { if (c.kind === 'managed-local' && !ready) localAction('start'); else connect(c); }
  const progress = state.busy ? <p className="desktop-hint" role="status">{({ checking: '正在检查安装环境', installing: '正在安装', restarting: '正在启动服务' } as Record<string, string>)[state.progress ?? ''] ?? `正在${actionNames[state.busy as LocalAction] ?? '处理'}`}，请保持应用打开。</p> : null;
  const titles: Record<Page, string> = { list: '选择连接', add: '你想在哪里使用 Molr？', remote: form.id ? '编辑连接' : '连接远程服务', install: '在这台电脑上使用 Molr', local: '管理本机服务' };
  const canBack = screen !== 'list' && (connections.length > 0 || (screen === 'remote' || screen === 'install') && canChoose);
  return <div className="desktop-app" data-app-platform={state.platform} data-app-fullscreen={state.window.fullscreen} style={{ '--app-titlebar-inset': state.window.titlebarInset === undefined ? undefined : `${state.window.titlebarInset}px` } as React.CSSProperties}>
    {state.view !== 'runtime' && <header className="desktop-toolbar">
      <div className="desktop-identity"><Logo animation="daily" assetBaseUrl={paletteAssetBase(palette)} variant={theme === 'dark' ? 'light' : 'dark'} /></div>
      <div className="desktop-toolbar-actions">
        {bridge && state.platform !== 'darwin' && <div className="desktop-window-controls">
          <IconButton label="最小化" onClick={() => void run(() => bridge.minimize())}><Minus size={16} /></IconButton>
          <IconButton label={state.window.maximized || state.window.fullscreen ? '还原窗口' : '最大化'} onClick={() => void run(() => bridge.toggleMaximize())}>{state.window.maximized || state.window.fullscreen ? <Copy size={14} /> : <Square size={14} />}</IconButton>
          <IconButton label="关闭窗口" className="desktop-close" onClick={() => void run(() => bridge.close())}><X size={18} /></IconButton>
        </div>}
      </div>
    </header>}
    {state.view === 'runtime' ? <div aria-label="Molr 服务页面" /> : <main className="desktop-main">
      {!bridge && <div className="desktop-notice" role="status"><CircleHelp size={18} /><span>这是桌面界面的浏览器预览。安装和连接服务请使用 Molr 桌面应用。</span></div>}
      {!loaded ? <div className="desktop-recovery"><p role="status">正在读取连接…</p><Button variant="secondary" onClick={() => void run(async () => { setState(await bridge!.state()); setLoaded(true); })}>重新读取</Button></div> : state.view !== 'manager' ? <section className="desktop-recovery">
        <Logo animation={state.view === 'loading' ? 'loading' : 'idle'} assetBaseUrl={paletteAssetBase(palette)} variant={theme === 'dark' ? 'light' : 'dark'} />
        <h1>{state.view === 'loading' ? '正在连接' : '暂时无法连接'}</h1><p>{state.error ?? current?.url}</p>
        <div className="desktop-actions">{state.view === 'failed' && current && <Button disabled={disabled} onClick={() => open(current)}><RefreshCw size={16} />重新连接</Button>}<Button variant="secondary" onClick={() => { navigate('list'); void run(() => bridge!.manager()); }}>返回连接列表</Button></div>
      </section> : <div className={`desktop-content ${screen === 'local' ? 'desktop-content-management' : screen === 'list' || screen === 'add' ? '' : 'desktop-content-narrow'}`}>
        {canBack && <Button className="desktop-back" variant="ghost" size="sm" disabled={pending || !!state.busy} onClick={() => navigate(connections.length ? 'list' : 'add')}><ArrowLeft size={16} />{connections.length ? '返回连接列表' : '返回'}</Button>}
        <div className="desktop-intro"><h1>{titles[screen]}</h1>{screen === 'list' && <p>选择助理运行的位置。</p>}</div>
        {screen === 'list' && <div className="desktop-grid" aria-label="已保存的连接">
          {connections.map(c => <Card role="article" className="desktop-card" key={c.id} aria-label={c.name}>
            <div className="desktop-card-top"><span className="desktop-section-icon">{c.kind === 'managed-local' ? <Monitor size={24} /> : <Server size={24} />}</span>
              <DropdownMenu label={`${c.name}的操作`} align="end" trigger={<IconButton label={`${c.name}的操作`} disabled={disabled}><MoreHorizontal size={20} /></IconButton>} items={c.kind === 'managed-local' ? [{ id: 'manage', label: '管理本机服务', onSelect: () => navigate('local') }] : [{ id: 'edit', label: '编辑连接', onSelect: () => { setForm({ ...c }); navigate('remote'); } }, { id: 'remove', label: '移除连接', danger: true, separatorBefore: true, onSelect: () => void run(() => bridge!.remove(c.id)) }]} />
            </div>
            <h2>{c.name}</h2><div className="desktop-card-description">{c.kind === 'managed-local' ? <><span>本机</span><Badge tone={ready ? 'positive' : local.error ? 'danger' : 'neutral'}>{state.busy ? '正在维护' : stateNames[local.state] ?? '需要处理'}</Badge>{versionNotice && <Badge className="desktop-version-notice" tone={versionState === 'update' ? 'neutral' : 'warning'}>{versionNotice}</Badge>}</> : <span title={c.url}>{new URL(c.url).host}</span>}</div>
            <Button className="desktop-card-open" variant="secondary" disabled={disabled} onClick={() => open(c)}>{c.kind === 'managed-local' && !ready ? '启动并连接' : '连接'}<ArrowRight size={16} /></Button>
          </Card>)}
          <Button variant="ghost" className="card desktop-add-card" disabled={pending || !!state.busy} onClick={add}><Plus aria-hidden="true" /><span>添加连接</span></Button>
        </div>}
        {screen === 'add' && <div className="desktop-grid desktop-choices">
          <section className="card desktop-card"><span className="desktop-section-icon"><Monitor size={28} /></span><h2>在这台电脑上使用</h2><p>在本机安装 Molr 服务，运行助理并保存对话和文件。</p><Button disabled={disabled} onClick={() => navigate('install')}>安装到这台电脑<ArrowRight size={16} /></Button></section>
          <section className="card desktop-card"><span className="desktop-section-icon"><Server size={28} /></span><h2>连接远程服务</h2><p>使用其他电脑或服务器上已安装的 Molr。</p><Button variant="secondary" onClick={() => { setForm(emptyForm()); navigate('remote'); }}>输入服务地址<ArrowRight size={16} /></Button></section>
        </div>}
        {screen === 'remote' && <form className="desktop-form" onSubmit={event => { event.preventDefault(); void run(async () => { await bridge!.save(form); navigate('list'); toast({ tone: 'positive', title: '连接已保存' }); }); }}>
          <TextField label="服务地址" value={form.url} type="url" required autoFocus placeholder="https://molr.example.com" disabled={disabled} onChange={e => setForm({ ...form, url: e.target.value, allowHTTP: /^http:/i.test(e.target.value) && form.allowHTTP })} />
          <TextField label="连接名称（选填）" value={form.name} maxLength={80} placeholder="默认使用主机名" disabled={disabled} onChange={e => setForm({ ...form, name: e.target.value })} />
          {/^http:/i.test(form.url) && <div><Switch label="允许使用未加密的 HTTP 连接" checked={form.allowHTTP} disabled={disabled} onChange={e => setForm({ ...form, allowHTTP: e.target.checked })} /><p className="desktop-hint">仅支持可信的局域网或本机地址。HTTP 不提供传输加密。</p></div>}
          <details className="desktop-help"><summary>在哪里找到服务地址？</summary><p>向服务管理员获取地址，或查看 Molr 安装结果。WSL 中的服务也通过地址添加。</p></details>
          <div className="desktop-actions"><Button type="submit" disabled={disabled} loading={pending}>保存连接</Button></div>
        </form>}
        {screen === 'install' && <form className="desktop-form" onSubmit={event => {
          event.preventDefault();
          if (local.state !== 'uninstalled' && password !== confirmation) { reportError(new Error('两次输入的密码不一致。')); return; }
          const administrator = local.state === 'uninstalled' ? undefined : { username: username.trim(), password };
          setPassword(''); setConfirmation('');
          setInstallError(undefined);
          void run(async () => {
            try { await bridge!.localAction('install', local.state === 'uninstalled' ? undefined : resourcePreset, administrator); }
            catch (error) { setInstallError((error instanceof Error ? error.message : String(error)).replace(/^Error invoking remote method '[^']+': Error: /, '')); throw error; }
            navigate('list'); toast({ tone: 'positive', title: '已安装，可以开始使用' });
          });
        }}>
          {local.state === 'uninstalled' ? <p>重新安装会沿用保留的账号、数据和权限设置。连接后请使用原账号登录。</p> : <>
            <p>设置管理员账号，以后可用此账号和密码登录。</p>
            <TextField label="管理员用户名" name="username" autoComplete="username" value={username} maxLength={128} required disabled={disabled} onChange={e => setUsername(e.target.value)} />
            <TextField label="密码" name="new-password" autoComplete="new-password" type="password" value={password} minLength={12} maxLength={1024} required disabled={disabled} placeholder="至少 12 个字符" onChange={e => setPassword(e.target.value)} />
            <TextField label="确认密码" name="confirm-password" autoComplete="new-password" type="password" value={confirmation} required disabled={disabled} onChange={e => setConfirmation(e.target.value)} />
            <details className="desktop-help"><summary>权限设置</summary><Select label="资源权限" value={resourcePreset} onValueChange={setResourcePreset} disabled={disabled} options={[{ value: 'permissive', label: '个人使用' }, { value: 'least-privilege', label: '最小权限' }]} /><p className="desktop-hint">{resourcePreset === 'permissive' ? '允许接入本机目录、网络及计算资源；助理使用这些资源仍需单独授权。' : '默认不开放本机目录、网络访问或命令执行资源，之后可按需配置。'}</p></details>
            <p className="desktop-hint">默认采用个人使用权限，助理使用资源仍需授权。可展开权限设置调整。</p>
          </>}
          <div className="desktop-actions"><Button type="submit" disabled={disabled} loading={pending}>{installError ? '重试安装' : '安装并启动'}<ArrowRight size={16} /></Button></div>{progress}
          <details className="desktop-help"><summary>安装帮助与日志</summary><p>{installError ?? local.error ?? '安装失败时，可在维护日志中查看原因。重试会保留已创建的账号和数据，请使用原来的账号密码。'}</p><Button variant="secondary" disabled={disabled} onClick={() => void run(() => bridge!.openLogs())}>打开日志所在位置</Button></details>
        </form>}
        {screen === 'local' && <Card className="desktop-management" role="region" aria-label="本机服务管理">
          <div className="desktop-management-summary">
            <div className="desktop-management-heading"><span className="desktop-section-icon"><Monitor size={24} /></span><div><h2>这台电脑</h2><p>版本 {local.version ?? '未知'}</p></div><Badge tone={ready ? 'positive' : local.error ? 'danger' : 'neutral'}>{state.busy ? '正在维护' : stateNames[local.state] ?? '需要处理'}</Badge></div>
            <p>关闭窗口或退出桌面应用后，服务继续运行。</p>
            <div className="desktop-actions"><Button disabled={disabled} onClick={() => { const c = connections.find(c => c.kind === 'managed-local'); if (c) open(c); }}>{ready ? '连接' : '启动并连接'}<ArrowRight size={16} /></Button>{ready && <><Button variant="secondary" disabled={disabled} onClick={() => localAction('restart')}>重启服务</Button><Button variant="ghost" disabled={disabled} onClick={() => localAction('stop')}>停止服务</Button></>}</div>
          </div>
          {versionNotice && <section className="desktop-management-group" aria-labelledby="local-version-heading">
            <h3 id="local-version-heading">{versionState === 'update' ? '更新本机服务' : '版本不一致'}</h3>
            <dl className="desktop-version-details"><div><dt>本机服务</dt><dd>{local.version}</dd></div><div><dt>桌面应用附带</dt><dd>{local.bundledVersion}</dd></div></dl>
            {versionState === 'update' ? <><p>可从 {local.version} 更新至 {local.bundledVersion}。更新会重启服务，保留账号、配置和数据。</p><div className="desktop-actions"><Button variant="secondary" disabled={disabled} onClick={() => localAction('upgrade')}>更新本机服务</Button></div></> : <p>{versionState === 'service-newer' ? '本机服务版本较新，建议更新桌面应用以保持版本一致。' : '两个版本的标记不同，无法确认可升级。请核对发行版本，使用配套的桌面应用。'}</p>}
          </section>}
          <section className="desktop-management-group" aria-labelledby="local-logs-heading">
            <div className="desktop-management-row"><div className="desktop-management-copy"><h3 id="local-logs-heading">维护日志</h3><p>安装、更新或启停服务遇到问题时，可打开日志排查。</p></div>
            <div className="desktop-actions"><Button variant="secondary" disabled={disabled} onClick={() => void run(() => bridge!.openLogs())}>打开日志所在位置</Button></div></div>
            {local.error && <p className="desktop-management-error">{local.error}</p>}
          </section>
          <section className="desktop-management-group desktop-management-row" aria-labelledby="local-uninstall-heading">
            <div className="desktop-management-copy"><h3 id="local-uninstall-heading">卸载本机服务</h3><p>停止服务并从连接列表移除，账号、配置和数据保留。</p></div>
            <Button variant="danger" disabled={disabled} onClick={() => localAction('uninstall')}>卸载本机服务</Button>
          </section>{progress}
        </Card>}
        {screen === 'list' && progress}
      </div>}
    </main>}
  </div>;
}
createRoot(document.getElementById('root')!).render(<React.StrictMode><UIProvider locale="zh"><ToastProvider><App /></ToastProvider></UIProvider></React.StrictMode>);
