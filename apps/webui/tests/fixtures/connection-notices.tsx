import { StrictMode, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Button, ConnectionNotice, palettes, useBreakpoint, useTheme } from '@molr/ui';
import type { ConnectionState } from '@molr/ui';
import '@fontsource-variable/dm-sans';
import '@molr/ui/styles.css';
import '../../src/styles.css';
import { LocaleProvider, useAppearance, useCopy } from '../../src/state';

function Example() {
  const { t, locale, setLocale } = useCopy(), { theme, changePreference } = useTheme(), { palette, changePalette } = useAppearance();
  const [connection, setConnection] = useState<ConnectionState>('reconnecting');
  const [reduced, setReduced] = useState(false);
  const mobile = useBreakpoint('mobile');
  return <main style={{ maxWidth: 1000, margin: '0 auto', padding: 16, display: 'grid', gap: 16 }} data-reduced-motion={reduced}>
    <h1>{locale === 'zh' ? '连接提示预览' : 'Connection notices'}</h1>
    <p className="muted">{locale === 'zh' ? '切换状态查看工作区提示；下方同时展示三种状态。此页面只演示样式，不连接服务器。' : 'Switch states to preview the workspace notice. All three states appear below. This preview does not connect to a server.'}</p>
    <div className="form-actions">
      {palettes.map(item => <Button key={item.id} size="sm" variant={palette === item.id ? 'primary' : 'secondary'} onClick={() => changePalette(item.id)}>{item[locale]}</Button>)}
      <Button size="sm" variant="secondary" onClick={() => changePreference(theme === 'dark' ? 'light' : 'dark')}>{theme === 'dark' ? 'Light' : 'Dark'}</Button>
      <Button size="sm" variant="secondary" onClick={() => setLocale(locale === 'zh' ? 'en' : 'zh')}>{locale === 'zh' ? 'English' : '中文'}</Button>
      <Button size="sm" variant="secondary" aria-pressed={reduced} onClick={() => setReduced(value => !value)}>{locale === 'zh' ? '减少动态效果' : 'Reduce motion'}</Button>
    </div>
    <div className="form-actions">
      {(['reconnecting', 'disconnected', 'failed', 'connected'] as const).map(state => <Button key={state} size="sm" variant={connection === state ? 'primary' : 'secondary'} onClick={() => setConnection(state)}>{state === 'failed' ? t.connectionFailed : state === 'disconnected' ? t.offline : t[state]}</Button>)}
    </div>
    <section className={`workspace${mobile ? ' workspace--mobile' : ''}`} style={{ height: 280 }}>
      <div className="workspace-content">
        <ConnectionNotice connection={connection} onRetry={() => setConnection('reconnecting')} />
        <div className="workspace-main">
        <div className="assistant-workspace">
          <div className="workspace-empty"><p className="muted">{locale === 'zh' ? '工作区内容' : 'Workspace content'}</p></div>
        </div>
        </div>
      </div>
    </section>
    <section style={{ display: 'grid', gap: 8 }}>
      {(['reconnecting', 'disconnected', 'failed'] as const).map(state => <ConnectionNotice key={state} connection={state} onRetry={() => setConnection('reconnecting')} />)}
    </section>
  </main>;
}
createRoot(document.getElementById('root')!).render(<StrictMode><LocaleProvider><Example /></LocaleProvider></StrictMode>);
