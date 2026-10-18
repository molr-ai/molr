import type { Locale } from './site-content';
import type { useDesktopRelease } from './use-desktop-release';
import { installMessages } from './install-content';
import { Icon } from './HomeIcon';
import { SystemIcon } from './SystemIcon';
import { InstallStatus } from './InstallStatus';

export function DesktopDownloads({ locale, releaseState }: { locale: Locale; releaseState: ReturnType<typeof useDesktopRelease> }) {
  const zh = locale === 'zh';
  const t = installMessages[locale];
  const { release, loading, error, retry } = releaseState;
  return <div className="home-desktop-downloads">
    <p className="home-desktop-intro">{zh ? '在桌面上，与你的助理保持连接。' : 'Stay connected to your assistant, right from your desktop.'}</p>
    <div className="home-desktop-editions">
      <div><Icon name="monitor" /><p><strong>{zh ? '本机运行' : 'Run locally'}</strong><span>{zh ? '直接在当前设备上创建并运行后端服务。' : 'Create and run a backend service directly on this device.'}</span></p></div>
      <div><Icon name="server" /><p><strong>{zh ? '连接远程后端' : 'Connect to a remote backend'}</strong><span>{zh ? '连接同一后端，在多台设备间无缝切换。' : 'Connect to the same backend and switch seamlessly between devices.'}</span></p></div>
    </div>
    {error || loading || !release ? <InstallStatus error={error}>
      {error ? <>{zh ? '暂时无法读取桌面发行信息。' : 'Desktop releases are currently unavailable.'} <button type="button" onClick={retry}>{t.retry}</button></> : loading ? t.loading : t.empty}
    </InstallStatus> : <div className="home-desktop-grid">
      {(['darwin', 'win32', 'linux'] as const).map(os => {
        const platforms = release.platforms.filter(p => p.os === os);
        if (!platforms.length) return null;
        return <section className="home-desktop-platform" key={os} aria-labelledby={`desktop-${os}`}>
          <div className="home-desktop-platform-heading"><SystemIcon os={os} /><span className="home-desktop-format">{({ linux: '.deb', darwin: '.dmg', win32: '.exe' })[os]}</span></div>
          <h4 id={`desktop-${os}`}>{({ linux: 'Linux', darwin: 'macOS', win32: 'Windows' })[os]}</h4>
          <p className="home-desktop-edition">{zh ? '完整版 · 包含 Runtime' : 'Full · includes Runtime'}</p>
          <div className="home-desktop-artifacts">{platforms.map(p => <div className="home-desktop-artifact" key={p.arch}>
            <span className="home-desktop-arch">{os === 'darwin' ? (p.arch === 'arm64' ? 'Apple Silicon' : 'Intel') : (p.arch === 'amd64' ? 'AMD64' : 'ARM64')}</span>
            <a className="button button--primary button--md" href={p.url}><Icon name="download" />{zh ? '下载安装包' : 'Download installer'}</a>
            {!p.signed && os === 'win32' && <p className="home-desktop-warning">{zh ? '未签名测试包：Windows 可能提示未知发布者，或按设备安全策略阻止运行。' : 'Unsigned test build: Windows may show an unknown publisher warning or block execution under device security policies.'}</p>}
            {!p.signed && os === 'darwin' && <p className="home-desktop-warning">{zh ? '未公证测试包：仅使用 ad-hoc 签名，macOS 可能阻止首次打开。' : 'Unnotarized test build: ad-hoc signed only; macOS may block the first launch.'}</p>}
            <details className="home-install-verification"><summary>SHA-256</summary><code className="home-install-digest">{p.sha256}</code></details>
          </div>)}</div>
        </section>;
      })}
    </div>}
  </div>;
}
