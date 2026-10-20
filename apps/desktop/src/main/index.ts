import { applicationMenu } from './menu.js';
import { app, BrowserWindow, WebContentsView, ipcMain, session, protocol, net, dialog, shell, Menu, powerMonitor, Notification } from 'electron';
import type { IpcMainInvokeEvent, Session, WebContents } from 'electron';
import { join, resolve, relative, sep, basename } from 'node:path';
import { existsSync, realpathSync, mkdirSync, statSync } from 'node:fs';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { ConnectionStore, partition, sameOrigin, externalURL, allowedFrameURL } from './connections.js';
import type { Connection, ConnectionInput, DesktopState, LocalAction, ResourcePreset, LocalStatus } from '../shared.js';
import type { AppInfo, AppWindowState } from '@molr/ui-host/app';
import type { LocalRuntime } from '../runtime/local.js';
import { DesktopNotifications } from './notifications.js';
import { OAuthFlow } from './oauth.js';
import { runtimeOAuthTransport } from './oauth-transport.js';

const here = fileURLToPath(new URL('.', import.meta.url));
const rendererRoot = resolve(here, '../../renderer');
const managerURL = 'molr-desktop://app/';
protocol.registerSchemesAsPrivileged([{ scheme: 'molr-desktop', privileges: { standard: true, secure: true, supportFetchAPI: true } }]);
app.enableSandbox();
// Test/development paths never change the packaged application's data directory.
if (!app.isPackaged && process.env.MOLR_DESKTOP_DATA) app.setPath('userData', resolve(process.env.MOLR_DESKTOP_DATA));
let win: BrowserWindow | undefined;
const oauth = new OAuthFlow();
let runtimeView: WebContentsView | undefined;
let notifications: DesktopNotifications | undefined;
let local: LocalRuntime | undefined;
let store: ConnectionStore;
let view: DesktopState['view'] = 'manager', error: string | undefined, active: string | undefined, busy: string | undefined;
let progress: string | undefined;
let localStatus: LocalStatus = { available: false, installed: false, state: 'unavailable' };
let loadingTimer: ReturnType<typeof setTimeout> | undefined;
let quitting = false;
let runtimeLoaded = false, runtimeReady = false;
const sessions = new Map<string, Session>();
const managedDownloads = new Map<number, Set<Electron.DownloadItem>>();
const appVersion = app.getVersion();
const devURL = !app.isPackaged ? process.env.MOLR_DESKTOP_DEV_URL : undefined;
if (devURL && (!/^http:\/\/(127\.0\.0\.1|localhost):\d+\/$/.test(devURL))) throw new Error('Development UI must use a loopback HTTP origin.');
const trustedURL = devURL ?? managerURL;

function snapshot(): DesktopState { return { ...store.data, active, view, error, local: localStatus, busy, progress, version: appVersion, platform: process.platform, window: windowState() }; }
const titlebarHeight = 48;
const initialMacButtonMargin = 16;
let macButtonHeight: number | undefined;
function syncMacWindowButtons() {
  if (process.platform !== 'darwin' || !win || win.isDestroyed() || win.isFullScreen() || macButtonHeight === undefined) return;
  const contents = view === 'runtime' ? runtimeView?.webContents : win.webContents;
  if (!contents || contents.isDestroyed()) return;
  // CSS pixels scale with the active renderer's zoom; native points do not.
  const margin = Math.max(0, Math.round((titlebarHeight * contents.getZoomFactor() - macButtonHeight) / 2));
  const previous = win.getWindowButtonPosition();
  if (previous?.x !== margin || previous.y !== margin) win.setWindowButtonPosition({ x: margin, y: margin });
}
function changed() {
  syncMacWindowButtons();
  if (win && !win.isDestroyed()) win.webContents.send('desktop:changed', snapshot());
}
function authorized(event: IpcMainInvokeEvent) {
  if (!win || event.sender !== win.webContents || event.senderFrame !== win.webContents.mainFrame || event.senderFrame.url.split('#')[0] !== trustedURL) throw new Error('Desktop management is unavailable to this page.');
}
function handle(name: string, fn: (...args: any[]) => unknown) { ipcMain.handle(`desktop:${name}`, (event, ...args) => { authorized(event); return fn(...args); }); }
function windowState(): AppWindowState { return { maximized: win?.isMaximized() ?? false, fullscreen: win?.isFullScreen() ?? false }; }
function windowChanged() {
  changed();
  const contents = runtimeView?.webContents;
  if (contents && !contents.isDestroyed() && active && sameOrigin(contents.getURL(), store.get(active).url)) contents.send('app:window-state', windowState());
}
function appHandle(name: string, fn: (...args: unknown[]) => unknown, argumentCount = 0) {
  ipcMain.handle(`app:${name}`, (event, ...args: unknown[]) => {
    const contents = runtimeView?.webContents;
    if (!win || !contents || !active || event.sender !== contents || event.senderFrame !== contents.mainFrame || !sameOrigin(event.senderFrame.url, store.get(active).url)) throw new Error('App controls are unavailable to this page.');
    if (args.length !== argumentCount) throw new Error('Invalid argument count.');
    return fn(...args);
  });
}
function toggleMaximize() { if (win?.isFullScreen()) win.setFullScreen(false); else if (win?.isMaximized()) win.unmaximize(); else win?.maximize(); }
// Return IPC replies before tearing down the calling renderer. Also guard
// against a newly connected view/window being affected by a stale request.
function disconnectLater() { const current = runtimeView; setTimeout(() => { if (runtimeView === current) manager(); }, 0); }
function closeLater() { const current = win; setTimeout(() => { if (win === current) current?.close(); }, 0); }
function layout() {
  if (!win || !runtimeView) return;
  const [width, height] = win.getContentSize();
  runtimeView.setBounds({ x: 0, y: 0, width, height });
}
function revealRuntime() {
  if (!runtimeLoaded || !runtimeReady || !runtimeView || view === 'runtime') return;
  clearTimeout(loadingTimer); runtimeView.setVisible(true); runtimeView.webContents.focus(); view = 'runtime'; error = undefined; changed();
}
function discard() {
  notifications?.dispose(); notifications = undefined;
  oauth.cancel();
  clearTimeout(loadingTimer); runtimeLoaded = false; runtimeReady = false;
  if (!runtimeView) return;
  const old = runtimeView; runtimeView = undefined;
  if (win && !win.isDestroyed()) win.contentView.removeChildView(old);
  for (const item of managedDownloads.get(old.webContents.id) ?? []) item.cancel();
  managedDownloads.delete(old.webContents.id);
  old.webContents.close({ waitForBeforeUnload: false });
}
function manager() { discard(); active = undefined; view = 'manager'; error = undefined; changed(); win?.webContents.focus(); }
function failed(message: string) { discard(); view = 'failed'; error = message; changed(); }
async function clearSession(id: string) {
  const ses = session.fromPartition(partition(id));
  await ses.closeAllConnections();
  await ses.clearStorageData();
  await ses.clearCache();
  await ses.clearAuthCache();
}
async function askExternal(url: string) {
  if (!win || !externalURL(url)) return;
  const result = await dialog.showMessageBox(win, { type: 'question', message: '在系统浏览器打开链接？', detail: new URL(url).origin, buttons: ['取消', '打开'], defaultId: 0, cancelId: 0 });
  if (result.response === 1) await shell.openExternal(url);
}
function configureSession(connection: Connection) {
  const id = connection.id;
  const existing = sessions.get(id); if (existing) return existing;
  const ses = session.fromPartition(partition(id)); sessions.set(id, ses);
  ses.setPermissionCheckHandler((contents, permission, requestingOrigin) => permission === 'clipboard-sanitized-write' && contents === runtimeView?.webContents && active === id && sameOrigin(requestingOrigin, connection.url));
  ses.setDevicePermissionHandler(() => false);
  ses.setPermissionRequestHandler((contents, permission, callback, details) => {
    callback(!!win && contents === runtimeView?.webContents && active === id && details.isMainFrame && sameOrigin(details.requestingUrl, connection.url) && permission === 'clipboard-sanitized-write');
  });
  ses.on('will-download', (event, item, contents) => {
    if (!runtimeView || contents !== runtimeView.webContents || active !== id) { event.preventDefault(); return; }
    const items = managedDownloads.get(contents.id) ?? new Set<Electron.DownloadItem>();
    items.add(item); managedDownloads.set(contents.id, items);
    item.setSaveDialogOptions({ title: `保存来自 ${connection.name} 的文件`, defaultPath: join(app.getPath('downloads'), basename(item.getFilename()).replace(/[\\/:*?"<>|\x00-\x1f]/g, '_')) });
    item.once('done', (_event, state) => {
      items.delete(item);
      if (state === 'interrupted' && win && !quitting) void dialog.showMessageBox(win, { type: 'error', message: '文件下载中断，请重试。' });
    });
  });
  return ses;
}
function guardRuntime(contents: WebContents, connection: Connection) {
  installShortcuts(contents);
  contents.setWindowOpenHandler(({ url }) => { void askExternal(url); return { action: 'deny' }; });
  contents.on('will-navigate', (event, url) => { if (!sameOrigin(url, connection.url)) { event.preventDefault(); void askExternal(url); } });
  contents.on('will-redirect', (event, url) => { if (!allowedFrameURL(url, connection.url, event.isMainFrame)) { event.preventDefault(); if (event.isMainFrame && runtimeView?.webContents === contents) failed('Runtime 重定向到了其他地址，请检查连接配置。'); } });
  contents.on('will-attach-webview', event => event.preventDefault());
  contents.on('will-frame-navigate', event => {
    // Runtime plugin pages are sandboxed same-origin Blob documents. Preserve
    // that contract without permitting local files or desktop custom protocols.
    if (!event.isMainFrame && !allowedFrameURL(event.url, connection.url, false)) event.preventDefault();
  });
  contents.on('render-process-gone', () => { if (runtimeView?.webContents === contents) failed('Runtime 页面已退出，可以重新连接。'); });
  contents.on('did-fail-load', (_event, code, _description, _url, mainFrame) => {
    if (mainFrame && code !== -3 && runtimeView?.webContents === contents) failed(code <= -200 && code >= -299 ? '无法验证服务器证书，请修复证书后重试。' : `无法连接 Runtime（${code}）。请检查地址和服务状态。`);
  });
}
async function connect(id: string, loginURL?: string) {
  const connection = store.get(id);
  if (connection.kind === 'managed-local' && !local) throw new Error('当前安装包不能管理本机 Runtime。');
  if (loginURL && !sameOrigin(loginURL, connection.url)) throw new Error('登录地址与本机 Runtime 不匹配。');
  discard(); active = id; view = 'loading'; error = undefined; store.select(id); changed();
  const contentsView = new WebContentsView({ webPreferences: { preload: join(here, '../preload/runtime.cjs'), session: configureSession(connection), nodeIntegration: false, contextIsolation: true, sandbox: true, webSecurity: true, allowRunningInsecureContent: false, webviewTag: false, navigateOnDragDrop: false } });
  runtimeView = contentsView;
  notifications = new DesktopNotifications(
    value => new Notification({ title: value.title, body: value.body, silent: true }),
    (scope, route) => {
      if (runtimeView !== contentsView || contentsView.webContents.isDestroyed() || !sameOrigin(contentsView.webContents.getURL(), connection.url)) return;
      if (win?.isMinimized()) win.restore();
      win?.show(); win?.focus(); contentsView.webContents.focus();
      contentsView.webContents.send('app:notification-click', scope, route);
    },
  );
  contentsView.setVisible(false);
  win!.contentView.addChildView(contentsView); layout();
  guardRuntime(contentsView.webContents, connection);
  contentsView.webContents.on('did-start-navigation', details => {
    if (!details.isMainFrame || details.isSameDocument || runtimeView !== contentsView) return;
    notifications?.dispose();
    oauth.cancel();
    runtimeLoaded = false; runtimeReady = false; contentsView.setVisible(false); view = 'loading'; error = undefined; changed();
    clearTimeout(loadingTimer);
    loadingTimer = setTimeout(() => { if (runtimeView === contentsView && view === 'loading') failed('连接超时，或 WebUI 未能启动 App 模式。请重试连接。'); }, 30000);
  });
  contentsView.webContents.on('preload-error', () => { if (runtimeView === contentsView) failed('桌面接口加载失败，请重启桌面应用。'); });
  contentsView.webContents.on('did-finish-load', () => {
    if (runtimeView !== contentsView || !sameOrigin(contentsView.webContents.getURL(), connection.url)) return;
    runtimeLoaded = true; revealRuntime();
  });
  // The renderer is untrusted. Successful navigation is not authenticated health.
  void contentsView.webContents.loadURL(loginURL ?? connection.url).catch(() => {});
}
async function refreshLocal() {
  if (!local) return;
  try { localStatus = await local.status(); } catch (e) { localStatus = { ...localStatus, error: String((e as Error).message), state: 'failed' }; }
  changed();
}
async function localAction(action: LocalAction, preset?: ResourcePreset) {
  if (!local) throw new Error('当前安装包不包含本机 Runtime。');
  if (busy) throw new Error('请等待当前维护操作完成。');
  if (!['install', 'start', 'stop', 'restart', 'upgrade', 'uninstall'].includes(action)) throw new Error('无效的操作。');
  // Set the lock before opening a dialog: repeated IPC must not create parallel actions.
  busy = action; changed();
  try {
    if (['stop', 'restart', 'upgrade', 'uninstall'].includes(action)) {
      let tasks = '尚未取得当前账号的任务状态。';
      const connection = store.data.connections.find(c => c.kind === 'managed-local');
      if (connection) {
        try {
          const response = await configureSession(connection).fetch(connection.url + '/api/tasks?state=running&limit=5', { redirect: 'error', signal: AbortSignal.timeout(5000) });
          if (response.ok) {
            const body = await response.json();
            const items = body.Items;
            if (Array.isArray(items)) tasks = items.length ? '当前账号正在运行的任务：\n' + items.map((t: { Goal?: string }) => `• ${String(t.Goal ?? '任务').slice(0, 120)}`).join('\n') + (body.NextCursor ? '\n还有更多任务。' : '') : '当前账号没有运行中的任务。';
          }
        } catch { /* Service control remains available when the Runtime is offline. */ }
      }
      const detail = `${tasks}\n\n此操作也会影响其他账号、计划和渠道中的执行。${action === 'uninstall' ? '服务将被移除，账号、配置和数据保留。' : '远程 Runtime 不受影响。'}`;
      const result = await dialog.showMessageBox(win!, { type: 'warning', message: '此操作会中断本机 Runtime 中正在执行的任务。', detail, buttons: ['取消', '继续'], defaultId: 0, cancelId: 0 });
      if (result.response !== 1) return;
    }
    const previous = store.data.connections.find(c => c.kind === 'managed-local');
    if (previous?.id === active) manager();
    const result = await local.action(action, preset);
    await refreshLocal();
    if (result.installed && result.url) {
      const c = store.managed(result.url);
      if (previous && c.id !== previous.id) await clearSession(previous.id);
      if (action === 'install' || action === 'start') await connect(c.id, result.loginURL);
    } else if (action === 'uninstall' && previous) {
      store.remove(previous.id); await clearSession(previous.id);
    }
  } finally { busy = undefined; progress = undefined; await refreshLocal(); changed(); }
}
async function createWindow() {
  macButtonHeight = undefined;
  win = new BrowserWindow({
    width: 1180, height: 820, minWidth: 620, minHeight: 540, title: 'Molr', show: false,
    // WCO initially measures the native button height; equal margins are then
    // calibrated to the fixed 48px web titlebar without assuming a button size.
    ...(process.platform === 'darwin' ? { titleBarStyle: 'hidden' as const, titleBarOverlay: true, trafficLightPosition: { x: initialMacButtonMargin, y: initialMacButtonMargin } } : { frame: false }),
    webPreferences: { preload: join(here, '../preload/index.cjs'), nodeIntegration: false, contextIsolation: true, sandbox: true, webSecurity: true, webviewTag: false },
  });
  win.on('resize', layout);
  win.on('maximize', windowChanged); win.on('unmaximize', windowChanged);
  win.on('enter-full-screen', windowChanged); win.on('leave-full-screen', windowChanged);
  installShortcuts(win.webContents);
  win.on('close', event => { if (busy) { event.preventDefault(); void dialog.showMessageBox(win!, { message: '本机 Runtime 正在维护，请等待操作结束后退出。' }); } });
  win.on('closed', () => { discard(); win = undefined; active = undefined; view = 'manager'; error = undefined; });
  win.webContents.on('will-navigate', event => event.preventDefault());
  win.webContents.setWindowOpenHandler(() => ({ action: 'deny' }));
  win.webContents.session.setPermissionRequestHandler((_c, _p, callback) => callback(false));
  win.once('ready-to-show', () => win?.show());
  await win.loadURL(trustedURL);
  if (process.platform === 'darwin') {
    // Read only the bundled, trusted renderer; Runtime pages cannot supply
    // native dimensions or arbitrary button positions.
    const height: number = await win.webContents.executeJavaScript(`new Promise(resolve => {
      const overlay = navigator.windowControlsOverlay;
      const read = () => {
        const height = overlay.getTitlebarAreaRect().height;
        if (height > 0) { overlay.removeEventListener('geometrychange', read); resolve(height); }
      };
      overlay.addEventListener('geometrychange', read);
      read();
    })`);
    const measured = Math.round(height * win.webContents.getZoomFactor()) - 2 * initialMacButtonMargin;
    if (measured > 0 && measured < titlebarHeight) {
      macButtonHeight = measured;
      syncMacWindowButtons();
    }
  }
}
function installShortcuts(contents: WebContents) {
  contents.on('before-input-event', (event, input) => {
    if (input.type !== 'keyDown' || input.alt) return;
    const modifier = process.platform === 'darwin' ? input.meta : input.control;
    if (modifier && input.shift && input.key.toLowerCase() === 'm') { event.preventDefault(); manager(); }
    else if (modifier && !input.shift && input.key.toLowerCase() === 'r' && active) { event.preventDefault(); void connect(active); }
    else if (process.platform !== 'darwin' && modifier && ['0', '+', '=', '-'].includes(input.key)) {
      event.preventDefault(); const delta = input.key === '-' ? -0.5 : 0.5;
      contents.setZoomLevel(input.key === '0' ? 0 : Math.max(-3, Math.min(3, contents.getZoomLevel() + delta)));
    } else if (process.platform !== 'darwin' && input.key === 'F11') { event.preventDefault(); win?.setFullScreen(!win.isFullScreen()); }
  });
}
async function main() {
  await app.whenReady();
  mkdirSync(app.getPath('userData'), { recursive: true, mode: 0o700 });
  store = new ConnectionStore(join(app.getPath('userData'), 'connections.json'));
  protocol.handle('molr-desktop', async request => {
    const u = new URL(request.url);
    if (u.host !== 'app' || request.method !== 'GET') return new Response(null, { status: 403 });
    try {
      const path = resolve(rendererRoot, '.' + decodeURIComponent(u.pathname === '/' ? '/index.html' : u.pathname));
      const real = realpathSync(path), rel = relative(rendererRoot, real);
      if (rel.startsWith('..' + sep) || rel === '..' || !statSync(real).isFile()) return new Response(null, { status: 403 });
      return net.fetch(pathToFileURL(real).href);
    } catch { return new Response(null, { status: 404 }); }
  });
  const packageRoot = app.isPackaged ? join(process.resourcesPath, 'runtime') : process.env.MOLR_DESKTOP_RUNTIME;
  if (packageRoot && existsSync(join(packageRoot, 'release.json'))) {
    try {
      const { LocalRuntime } = await import('../runtime/local.js');
      local = new LocalRuntime(packageRoot, join(app.getPath('userData'), 'runtime'), stage => { progress = stage; changed(); });
      localStatus.available = true;
      await refreshLocal();
      if (localStatus.installed && localStatus.url) store.managed(localStatus.url);
    } catch (e) { localStatus = { available: false, installed: false, state: 'failed', error: (e as Error).message }; }
  }
  handle('state', snapshot);
  handle('sync-titlebar', syncMacWindowButtons);
  handle('minimize', () => win?.minimize()); handle('toggle-maximize', toggleMaximize); handle('close', closeLater);
  appHandle('info', (): AppInfo => { runtimeReady = true; revealRuntime(); return { apiVersion: 1, platform: process.platform, connection: { name: store.get(active!).name, url: store.get(active!).url }, window: windowState() }; });
  appHandle('minimize', () => win?.minimize()); appHandle('toggle-maximize', toggleMaximize); appHandle('close', closeLater); appHandle('disconnect', disconnectLater);
  appHandle('notification-begin', scope => notifications!.begin(scope), 1);
  // On macOS initialization asks the OS for permission. Do not call during
  // page load: isSupported is a capability check, not an authorization status.
  appHandle('notification-permission', () => Notification.isSupported());
  appHandle('notification-show', (scope, value) => notifications!.show(scope, value), 2);
  appHandle('notification-close', (scope, tag) => notifications!.close(scope, tag), 2);
  appHandle('sync-titlebar', syncMacWindowButtons);
  appHandle('oauth-cancel', () => oauth.cancel());
  appHandle('oauth-authorize', provider => {
    const current = runtimeView!, connection = store.get(active!), window = win!;
    return oauth.authorize(provider, runtimeOAuthTransport({
      url: connection.url, allowHTTP: connection.allowHTTP, session: current.webContents.session,
      current: () => runtimeView === current && active === connection.id && !current.webContents.isDestroyed(),
      confirm: async (origin, signal) => {
        const result = await dialog.showMessageBox(window, { type: 'question', message: '在浏览器中授权服务？', detail: `授权网站：${origin}\n接收连接：${connection.name}\nRuntime：${connection.url}`, buttons: ['取消', '继续'], defaultId: 0, cancelId: 0, signal });
        return result.response === 1;
      },
      openExternal: url => shell.openExternal(url),
    }));
  }, 1);
  handle('save', async (input: ConnectionInput) => {
    if (input?.id === active) manager();
    const result = store.save(input); if (result.retired) await clearSession(result.retired); changed();
  });
  handle('remove', async (id: string) => {
    if (store.get(id).kind === 'managed-local') throw new Error('请通过本机服务管理卸载此实例。');
    const answer = await dialog.showMessageBox(win!, { message: '移除此连接并清除它在桌面端的登录状态？', buttons: ['取消', '移除'], defaultId: 0, cancelId: 0 });
    if (answer.response !== 1) return;
    if (id === active) manager();
    await clearSession(id); store.remove(id); changed();
  });
  handle('connect', connect); handle('manager', manager);
  // Local management is exposed only when a matching bundled Runtime is available.
  if (local) {
    handle('local-status', refreshLocal); handle('local-action', localAction);
    handle('logs', async () => { if (local && existsSync(local.logFile)) shell.showItemInFolder(local.logFile); });
  }
  handle('downloads', () => shell.openExternal('https://molr.ai/'));
  Menu.setApplicationMenu(process.platform === 'darwin' ? Menu.buildFromTemplate(applicationMenu(
    app.getLocale().toLowerCase().startsWith('zh') ? 'zh' : 'en',
    { manager, reconnect: () => { if (active) void connect(active); } },
  )) : null);
  await createWindow();
  powerMonitor.on('resume', () => { void refreshLocal(); });
}
if (!app.requestSingleInstanceLock()) app.quit();
else {
  app.on('second-instance', () => { if (win) { if (win.isMinimized()) win.restore(); win.show(); win.focus(); } else void createWindow(); });
  app.on('activate', () => { if (!win) void createWindow(); });
  app.on('before-quit', event => { if (busy) { event.preventDefault(); return; } quitting = true; });
  app.on('window-all-closed', () => { if (process.platform !== 'darwin') app.quit(); });
  void main().catch(e => { dialog.showErrorBox('Molr 无法启动', String(e.message)); app.quit(); });
}
