import { useEffect, useState, type ReactNode } from 'react';
import type { HomeCopy } from './home-content';
import type { Locale } from './site-content';
import { installMessages } from './install-content';
import { localizedDocPath } from './doc-locale';
import { siteLinks } from './site-links';
import { channels, installCommand, packageInstallCommand, readCatalog } from './release-data';
import type { Catalog, Channel, DownloadTool } from './release-data';
import './install.css';
import { InstallCommand } from './InstallCommand';
import { Icon } from './HomeIcon';

function OptionRow({ label, children }: { label: string; children: ReactNode }) {
  return <div className="home-install-option-row" role="group" aria-label={label}>
    <span className="home-install-option-label">{label}</span>
    <div className="home-install-options">{children}</div>
  </div>;
}

export function InstallSection({ copy, locale }: { copy: HomeCopy; locale: Locale }) {
  const t = installMessages[locale];
  const [method, setMethod] = useState<'command' | 'download'>('command');
  const [channel, setChannel] = useState<Channel>('stable');
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [selectedOS, setOS] = useState('linux');
  const [selectedArch, setArch] = useState('amd64');
  const [tool, setTool] = useState<DownloadTool>('curl');
  const [error, setError] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const source = new URL(import.meta.env.VITE_RELEASE_SOURCE || '/releases', window.location.origin).href.replace(/\/$/, '');
  useEffect(() => {
    const controller = new AbortController(); setError(false);
    fetch(`${source}/catalog.json`, { signal: controller.signal, cache: 'no-cache' })
      .then(response => { if (!response.ok) throw new Error('Release catalog unavailable'); return response.json(); })
      .then(value => setCatalog(readCatalog(value)))
      .catch(() => { if (!controller.signal.aborted) setError(true); });
    return () => controller.abort();
  }, [source, attempt]);
  const release = catalog?.channels[channel];
  const platforms = release?.platforms || [];
  const systems = [...new Set(platforms.map(p => p.os))];
  const os = systems.includes(selectedOS) ? selectedOS : systems[0];
  const targets = platforms.filter(p => p.os === os);
  const platform = targets.find(p => p.arch === selectedArch) || targets[0];
  const command = platform ? installCommand(channel, { arch: platform.arch, tool }) : '';
  const packageCommand = platform ? packageInstallCommand(platform, channel) : '';
  const osLabel = (value: string) => ({ linux: 'Linux', darwin: 'macOS', windows: 'Windows' })[value] || value;
  const archLabel = (value: string) => (os === 'darwin' ? { amd64: 'Intel', arm64: 'Apple Silicon' } : { amd64: 'x64 / AMD64', arm64: 'ARM64' })[value] || value;
  return <section id="install" data-palette="forest" data-theme="dark" className="home-start home-container" aria-labelledby="start-title">
    <p className="home-section-label">{copy.startLabel}</p>
    <h2 id="start-title">{copy.startHeading}</h2>
    <p className="home-section-intro">{t.browser}</p>
    <div className="home-installer">
      <div className="home-install-methods" role="group" aria-label={t.method}>
        <button type="button" aria-pressed={method === 'command'} aria-controls="install-details" onClick={() => setMethod('command')}><span aria-hidden="true" className="home-install-terminal-icon">&gt;_</span>{t.command}<small>{t.recommended}</small></button>
        <button type="button" aria-pressed={method === 'download'} aria-controls="install-details" onClick={() => setMethod('download')}><Icon name="download" />{t.download}</button>
      </div>
      <div className="home-install-workspace">
        <div className="home-install-config">
          <OptionRow label={t.channel}>
            {channels.map(c => <button type="button" key={c} aria-pressed={channel === c} onClick={() => setChannel(c)}>{t[c]}</button>)}
          </OptionRow>
          {release && !error && <>
            <OptionRow label={t.system}>
              {systems.map(system => <button type="button" key={system} aria-pressed={os === system} onClick={() => setOS(system)}>{osLabel(system)}</button>)}
            </OptionRow>
            <OptionRow label={t.architecture}>
              {targets.map(p => <button type="button" key={p.id} aria-pressed={platform?.arch === p.arch} onClick={() => setArch(p.arch)}>{archLabel(p.arch)}</button>)}
            </OptionRow>
            {method === 'command' && <OptionRow label={t.downloadTool}>
              {(['curl', 'wget'] as const).map(value => <button type="button" key={value} aria-pressed={tool === value} onClick={() => setTool(value)}>{value}</button>)}
            </OptionRow>}
          </>}
          <p className="home-install-channel-note">{t[`${channel}Description`]}</p>
        </div>
        <div id="install-details" className="home-install-result">
        {error ? <p className="home-install-status" role="alert">{t.loadError} <button type="button" onClick={() => setAttempt(v => v + 1)}>{t.retry}</button></p>
          : !catalog ? <p className="home-install-status" role="status">{t.loading}</p>
          : !release || !platform ? <p className="home-install-status" role="status">{t.empty}</p>
          : <>
            <div className="home-install-result-meta"><span>{osLabel(os)} / {archLabel(platform.arch)}</span><strong>v{release.version}</strong></div>
            <div className="home-install-details">
              <div className="home-install-heading"><h3>{method === 'command' ? t.commandTitle : t.downloadTitle}</h3></div>
              <p>{method === 'command' ? t.commandBody : t.downloadBody}</p>
              {method === 'command' ? <InstallCommand key={command} command={command} locale={locale} /> : <>
                <div className="home-install-package">
                  <div className="home-install-package-top">
                    <div className="home-install-package-heading"><Icon name="file" /><div><span className="home-install-archive">tar.gz</span><h4>{platform.file}</h4></div></div>
                    <a className="home-button home-button-primary" href={platform.url}><Icon name="download" />{t.downloadAction}</a>
                  </div>
                  <div className="home-install-address"><span>{t.downloadAddress}</span><a href={platform.url}>{platform.url}</a></div>
                  <div className="home-install-checks">
                  <a className="home-install-checksum" href={platform.checksum_url}>{t.checksum}<span aria-hidden="true"> ↗</span></a>
                  <details className="home-install-verification"><summary>SHA-256</summary><code className="home-install-digest">{platform.sha256}</code></details>
                  </div>
                </div>
                <p>{t.offline}</p>
                <InstallCommand key={packageCommand} command={packageCommand} locale={locale} />
              </>}
            </div>
            <div className="home-install-release"><time dateTime={release.published_at}>{new Date(release.published_at).toLocaleDateString(locale === 'zh' ? 'zh-CN' : 'en-US', { timeZone: 'UTC' })}</time><a href={release.notes_url}>{t.notes}<span aria-hidden="true"> ↗</span></a></div>
            <p className="home-install-requirements">{os === 'darwin' ? `macOS ${platform.requirements.macos}+ · ${locale === 'zh' ? '用户级后台服务；命令执行仅支持显式 Full Access' : 'User background service; command execution requires explicit Full Access'}` : `${osLabel(os)} · systemd ${platform.requirements.systemd}+ · cgroup v${platform.requirements.cgroup} · ${platform.requirements.packages.join(', ')}`}</p>
          </>}
        </div>
      </div>
      <div className="home-install-footer"><span>{t.targetHint}</span><a href={localizedDocPath(siteLinks.install!, locale)}>{t.guide}<span aria-hidden="true"> ↗</span></a></div>
    </div>
  </section>;
}
