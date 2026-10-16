import { useEffect, useState } from 'react';
import type { Locale } from './site-content';
import { channels } from './release-data';
import type { Channel } from './release-data';
import { readDesktopCatalog } from './desktop-releases';
import type { DesktopCatalog } from './desktop-releases';

export function DesktopDownloads({ locale, source }: { locale: Locale; source: string }) {
  const zh = locale === 'zh';
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
  return <div className="home-install-details">
    <div className="home-install-heading"><h3>{zh ? 'Molr 桌面应用' : 'Molr desktop app'}</h3></div>
    <p>{zh ? 'Linux 和 macOS 完整版包含 Runtime，也可以连接远程实例。Windows 客户端仅连接远程 Runtime，WSL 也按远程连接使用。' : 'Linux and macOS include a Runtime and can connect to remote instances. The Windows client connects to remote Runtimes, including WSL.'}</p>
    <div className="home-install-options" role="group" aria-label={zh ? '桌面发行渠道' : 'Desktop release channel'}>{channels.map(c => <button key={c} aria-pressed={channel === c} onClick={() => setChannel(c)}>{c}</button>)}</div>
    {error ? <p role="alert">{zh ? '暂时无法读取桌面发行信息。' : 'Desktop releases are currently unavailable.'} <button onClick={() => setAttempt(x => x + 1)}>{zh ? '重试' : 'Retry'}</button></p> : !catalog ? <p role="status">{zh ? '正在读取发行信息…' : 'Loading releases…'}</p> : !release ? <p role="status">{zh ? '此渠道暂无已发布的桌面安装包。' : 'No desktop installers have been published in this channel.'}</p> : release.platforms.map(p => <div className="home-install-package" key={`${p.os}-${p.arch}`}>
      <div className="home-install-package-top"><div><h4>{({ linux: 'Linux', darwin: 'macOS', win32: 'Windows' })[p.os]} · {p.arch === 'amd64' ? 'x64' : 'ARM64'}</h4><p>{p.variant === 'full' ? (zh ? '完整版 · 包含 Runtime' : 'Full · includes Runtime') : (zh ? '客户端 · 仅远程连接' : 'Client · remote connections only')} · v{release.version}</p></div><a className="home-button home-button-primary" href={p.url}>{zh ? '下载安装包' : 'Download installer'}</a></div>
      {!p.signed && p.os === 'win32' && <p>{zh ? '未签名测试包：Windows 可能提示未知发布者，或按设备安全策略阻止运行。' : 'Unsigned test build: Windows may show an unknown publisher warning or block execution under device security policies.'}</p>}
      {!p.signed && p.os === 'darwin' && <p>{zh ? '未公证测试包：仅使用 ad-hoc 签名，macOS 可能阻止首次打开。' : 'Unnotarized test build: ad-hoc signed only; macOS may block the first launch.'}</p>}
      <details className="home-install-verification"><summary>SHA-256</summary><code className="home-install-digest">{p.sha256}</code></details>
    </div>)}
  </div>;
}
