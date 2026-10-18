import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { Copy, Minus, Square, Unplug, X } from 'lucide-react';
import { Button, IconButton, ModalScope } from '@molr/ui';
import type { AppInfo, MolrAppAPI } from '@molr/ui-host/app';
import { useCopy } from './state';
import './app-mode.css';

declare global { interface Window { molrApp?: MolrAppAPI } }

/** Outside the session gate and custom UI frames so controls survive failures. */
export function AppFrame({ children }: { children: ReactNode }) {
  const bridge = window.molrApp;
  const { locale } = useCopy();
  const [content, setContent] = useState<HTMLDivElement | null>(null);
  const [info, setInfo] = useState<AppInfo>();
  const [error, setError] = useState('');
  const [disconnecting, setDisconnecting] = useState(false);
  const zh = locale === 'zh';
  useEffect(() => {
    if (!bridge) return;
    let mounted = true;
    const dispose = bridge.onWindowStateChanged(state => { if (mounted) setInfo(previous => previous ? { ...previous, window: state } : previous); });
    void bridge.getInfo().then(value => {
      if (value.apiVersion !== 1) throw new Error(zh ? '桌面接口版本不匹配' : 'Unsupported desktop API version');
      if (mounted) setInfo(value);
    }).catch(error => { if (mounted) setError(String(error.message)); });
    return () => { mounted = false; dispose(); };
  }, [bridge, zh]);
  if (!bridge) return children;
  async function run(action: () => Promise<void>) {
    setError('');
    try { await action(); } catch (error) { setError((error as Error).message); setDisconnecting(false); }
  }
  const restore = info?.window.maximized || info?.window.fullscreen;
  return <div className="app-frame" data-app-platform={info?.platform}>
    <header className="app-titlebar" aria-label={zh ? '桌面窗口' : 'Desktop window'}>
      <div className="app-titlebar-identity"><strong>Molr</strong><span title={info?.connection.url}>{info?.connection.name}</span></div>
      <div className="app-titlebar-actions">
        <Button variant="ghost" size="sm" disabled={disconnecting} onClick={() => { setDisconnecting(true); void run(() => bridge!.disconnect()); }}><Unplug size={16} /><span className="app-disconnect-label">{zh ? '断开连接' : 'Disconnect'}</span></Button>
        <div className="app-window-controls">
          <IconButton label={zh ? '最小化' : 'Minimize'} onClick={() => void run(() => bridge!.window.minimize())}><Minus size={16} /></IconButton>
          <IconButton label={restore ? (zh ? '还原窗口' : 'Restore window') : (zh ? '最大化' : 'Maximize')} onClick={() => void run(() => bridge!.window.toggleMaximize())}>{restore ? <Copy size={14} /> : <Square size={14} />}</IconButton>
          <IconButton className="app-window-close" label={zh ? '关闭窗口' : 'Close window'} onClick={() => void run(() => bridge!.window.close())}><X size={18} /></IconButton>
        </div>
      </div>
    </header>
    {error && <div className="app-frame-error" role="alert">{error}</div>}
    <div ref={setContent} className="app-frame-content"><ModalScope container={content} presentation="dialog">{children}</ModalScope></div>
  </div>;
}
