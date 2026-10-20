import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { ReactNode } from 'react';
import { Copy, Minus, Square, Unplug, X } from 'lucide-react';
import { Button, IconButton, Logo, ModalScope, useBreakpoint, useTheme, useToast } from '@molr/ui';
import type { AppInfo, MolrAppAPI } from '@molr/ui-host/app';
import { useCopy } from './state';
import './app-mode.css';

declare global { interface Window { molrApp?: MolrAppAPI } }

const TitlebarBrandContext = createContext<HTMLDivElement | null>(null);
export function AppTitlebarBrand({ children }: { children: ReactNode }) {
  const target = useContext(TitlebarBrandContext);
  return target ? createPortal(children, target) : null;
}

/** Outside the session gate and custom UI frames so controls survive failures. */
export function AppFrame({ children }: { children: ReactNode }) {
  const bridge = window.molrApp;
  const { locale } = useCopy();
  const { theme } = useTheme();
  const mobile = useBreakpoint('mobile');
  const [frame, setFrame] = useState<HTMLDivElement | null>(null);
  const [brand, setBrand] = useState<HTMLDivElement | null>(null);
  const [info, setInfo] = useState<AppInfo>();
  const { toast, dismiss } = useToast();
  const lastError = useRef<number | undefined>(undefined);
  const [disconnecting, setDisconnecting] = useState(false);
  const zh = locale === 'zh';
  const reportError = useCallback((error: unknown) => {
    if (lastError.current !== undefined) dismiss(lastError.current);
    lastError.current = toast({ tone: 'danger', title: zh ? '桌面操作失败' : 'Desktop action failed', description: error instanceof Error ? error.message : String(error) });
  }, [toast, dismiss, zh]);
  useEffect(() => {
    if (!bridge) return;
    let mounted = true;
    const dispose = bridge.onWindowStateChanged(state => { if (mounted) setInfo(previous => previous ? { ...previous, window: state } : previous); });
    void bridge.getInfo().then(value => {
      if (value.apiVersion !== 1) throw new Error(zh ? '桌面接口版本不匹配' : 'Unsupported desktop API version');
      if (mounted) setInfo(value);
    }).catch(error => { if (mounted) reportError(error); });
    return () => { mounted = false; dispose(); };
  }, [bridge, zh, reportError]);
  if (!bridge) return children;
  async function run(action: () => Promise<void>) {
    try { await action(); } catch (error) { reportError(error); setDisconnecting(false); }
  }
  const restore = info?.window.maximized || info?.window.fullscreen;
  return <TitlebarBrandContext.Provider value={brand}><div ref={setFrame} className="app-frame" data-app-platform={info?.platform} data-app-fullscreen={info?.window.fullscreen}>
    <ModalScope container={frame} presentation={mobile ? 'sheet' : 'dialog'}>
    <header className="app-titlebar" aria-label={zh ? '桌面窗口' : 'Desktop window'}>
      <div className="app-titlebar-identity"><div ref={setBrand} className="app-titlebar-brand"><Logo variant={theme === 'dark' ? 'light' : 'dark'} /></div><span title={info?.connection.url}>{info?.connection.name}</span></div>
      <div className="app-titlebar-actions">
        <Button variant="ghost" size="sm" disabled={disconnecting} onClick={() => { setDisconnecting(true); void run(() => bridge!.disconnect()); }}><Unplug size={16} /><span className="app-disconnect-label">{zh ? '断开连接' : 'Disconnect'}</span></Button>
        {info?.platform !== 'darwin' && <div className="app-window-controls">
          <IconButton label={zh ? '最小化' : 'Minimize'} onClick={() => void run(() => bridge!.window.minimize())}><Minus size={16} /></IconButton>
          <IconButton label={restore ? (zh ? '还原窗口' : 'Restore window') : (zh ? '最大化' : 'Maximize')} onClick={() => void run(() => bridge!.window.toggleMaximize())}>{restore ? <Copy size={14} /> : <Square size={14} />}</IconButton>
          <IconButton className="app-window-close" label={zh ? '关闭窗口' : 'Close window'} onClick={() => void run(() => bridge!.window.close())}><X size={18} /></IconButton>
        </div>}
      </div>
    </header>
    <div className="app-frame-content">{children}</div>
    </ModalScope>
  </div></TitlebarBrandContext.Provider>;
}
