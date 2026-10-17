import { useEffect, useState } from 'react';
import type { Locale } from './site-content';
import { channels } from './release-data';
import type { Channel } from './release-data';
import { readDesktopCatalog } from './desktop-releases';
import type { DesktopCatalog } from './desktop-releases';
import { installMessages } from './install-content';
import { Icon } from './HomeIcon';
import { InstallStatus } from './InstallStatus';

export function DesktopDownloads({ locale, source }: { locale: Locale; source: string }) {
  const zh = locale === 'zh';
  const t = installMessages[locale];
  const [catalog, setCatalog] = useState<DesktopCatalog>();
  const [error, setError] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [channel, setChannel] = useState<Channel>('stable');
  useEffect(() => {
    const abort = new AbortController(); setError(false); setCatalog(undefined);
    fetch(`${source}/desktop/catalog.json`, { signal: abort.signal, cache: 'no-cache' }).then(async response => {
      if (response.status === 404) return { schema: 1, channels: { stable: null, preview: null, nightly: null } };
      if (!response.ok) throw new Error('Unavailable');
      return response.json();
    }).then(value => setCatalog(readDesktopCatalog(value))).catch(() => { if (!abort.signal.aborted) setError(true); });
    return () => abort.abort();
  }, [source, attempt]);
  const release = catalog?.channels[channel];
  return <div className="home-desktop-downloads">
    <div className="home-desktop-header">
      <div className="home-install-heading"><h3>{zh ? 'Molr 桌面应用' : 'Molr desktop app'}</h3>
        <p>{zh ? '在桌面上，与你的助理保持连接。' : 'Stay connected to your assistant, right from your desktop.'}</p>
      </div>
      {release && !error && <span className="home-desktop-version">v{release.version}</span>}
    </div>
    <div className="home-desktop-editions">
      <div><Icon name="devices" /><p><strong>{zh ? '本机运行，也能远程连接' : 'Run locally or connect remotely'}</strong><span>{zh ? 'Linux / macOS 包含 Runtime，可创建本机服务或连接远程服务。' : 'Linux / macOS includes the Runtime to create a local server or connect remotely.'}</span></p></div>
      <div><Icon name="server" /><p><strong>{zh ? '连接你已有的服务' : 'Connect to your existing server'}</strong><span>{zh ? 'Windows 客户端连接远程 Runtime，也支持 WSL。' : 'The Windows client connects to a remote Runtime, including WSL.'}</span></p></div>
    </div>
    <details className="home-install-advanced"><summary>{t.channel} · {t[channel]}</summary>
    <div className="home-install-channels">
      <div className="home-install-options" role="group" aria-label={zh ? '桌面发行渠道' : 'Desktop release channel'}>{channels.map(c => <button type="button" key={c} aria-pressed={channel === c} onClick={() => setChannel(c)}>{t[c]}</button>)}</div>
      <p>{t[`${channel}Description`]}</p>
    </div>
    </details>
    {error || !catalog || !release ? <InstallStatus error={error} hint={catalog && !error ? (zh ? '可切换发布渠道，查看其他可用版本。' : 'Switch release channels to check other available versions.') : undefined}>
      {error ? <>{zh ? '暂时无法读取桌面发行信息。' : 'Desktop releases are currently unavailable.'} <button type="button" onClick={() => setAttempt(x => x + 1)}>{t.retry}</button></> : !catalog ? t.loading : (zh ? '此渠道暂无已发布的桌面安装包。' : 'No desktop installers have been published in this channel.')}
    </InstallStatus> : <div className="home-desktop-grid">
      {(['darwin', 'win32', 'linux'] as const).map(os => {
        const platforms = release.platforms.filter(p => p.os === os);
        if (!platforms.length) return null;
        return <section className="home-desktop-platform" key={os} aria-labelledby={`desktop-${os}`}>
          <div className="home-desktop-platform-heading"><Icon name={os === 'linux' ? 'terminal' : 'devices'} /><span className="home-desktop-format">{({ linux: '.deb', darwin: '.dmg', win32: '.exe' })[os]}</span></div>
          <h4 id={`desktop-${os}`}>{({ linux: 'Linux', darwin: 'macOS', win32: 'Windows' })[os]}</h4>
          <p className="home-desktop-edition">{os === 'win32' ? (zh ? '客户端 · 仅远程连接' : 'Client · remote connections only') : (zh ? '完整版 · 包含 Runtime' : 'Full · includes Runtime')}</p>
          <div className="home-desktop-artifacts">{platforms.map(p => <div className="home-desktop-artifact" key={p.arch}>
            <span className="home-desktop-arch">{os === 'darwin' ? (p.arch === 'arm64' ? 'Apple Silicon' : 'Intel') : (p.arch === 'amd64' ? 'x64 / AMD64' : 'ARM64')}</span>
            <a className="home-button home-button-primary" href={p.url}><Icon name="download" />{zh ? '下载安装包' : 'Download installer'}</a>
            {!p.signed && os === 'win32' && <p className="home-desktop-warning">{zh ? '未签名测试包：Windows 可能提示未知发布者，或按设备安全策略阻止运行。' : 'Unsigned test build: Windows may show an unknown publisher warning or block execution under device security policies.'}</p>}
            {!p.signed && os === 'darwin' && <p className="home-desktop-warning">{zh ? '未公证测试包：仅使用 ad-hoc 签名，macOS 可能阻止首次打开。' : 'Unnotarized test build: ad-hoc signed only; macOS may block the first launch.'}</p>}
            <details className="home-install-verification"><summary>SHA-256</summary><code className="home-install-digest">{p.sha256}</code></details>
          </div>)}</div>
        </section>;
      })}
    </div>}
  </div>;
}
