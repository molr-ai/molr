import { useEffect, useState, type ReactNode } from 'react';
import type { HomeCopy } from './home-content';
import type { Locale } from './site-content';
import { installMessages } from './install-content';
import { localizedDocPath } from './doc-locale';
import { siteLinks } from './site-links';
import { channels, installCommand, packageInstallCommand, readCatalog } from './release-data';
import type { Catalog, Channel, DownloadTool, RuntimeMode } from './release-data';
import './install.css';
import { AgentInstall, agentInstallPath } from './AgentInstall';
import { InstallCommand } from './InstallCommand';
import { InstallStatus } from './InstallStatus';
import { Icon } from './HomeIcon';
import { DesktopDownloads } from './DesktopDownloads';

function OptionRow({ label, children }: { label: string; children: ReactNode }) {
  return <div className="home-install-option-row" role="group" aria-label={label}>
    <span className="home-install-option-label">{label}</span>
    <div className="home-install-options">{children}</div>
  </div>;
}

export function InstallSection({ copy, locale }: { copy: HomeCopy; locale: Locale }) {
  const t = installMessages[locale];
  const [entry, setEntry] = useState<'desktop' | 'server'>('server');
  const [mode, setMode] = useState<RuntimeMode>('server');
  const [method, setMethod] = useState<'command' | 'download' | 'docker' | 'agent'>('command');
  const [autoArch, setAutoArch] = useState(true);
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
  const architectures = [...new Set(platforms.map(p => p.arch))];
  const commandArch = architectures.includes(selectedArch) ? selectedArch : architectures[0];
  const command = platform ? installCommand(channel, { arch: autoArch ? undefined : commandArch, tool, mode }) : '';
  const packageCommand = platform ? packageInstallCommand(platform, channel, mode) : '';
  const osLabel = (value: string) => ({ linux: 'Linux', darwin: 'macOS', windows: 'Windows' })[value] || value;
  const archLabel = (value: string) => (os === 'darwin' ? { amd64: 'Intel', arm64: 'Apple Silicon' } : { amd64: 'x64 / AMD64', arm64: 'ARM64' })[value] || value;
  return <section id="install" data-palette="forest" data-theme="dark" className="home-start home-container" aria-labelledby="start-title">
    <p className="home-section-label">{copy.startLabel}</p>
    <h2 id="start-title">{copy.startHeading}</h2>
    <p className="home-section-intro">{t.browser}</p>
    <div className="home-installer">
      <div className="home-install-methods" role="group" aria-label={t.method}>
        <button type="button" aria-pressed={entry === 'desktop'} aria-controls="install-details" onClick={() => setEntry('desktop')}><Icon name="devices" /><span>{t.desktop}</span></button>
        <button type="button" aria-pressed={entry === 'server'} aria-controls="install-details" onClick={() => setEntry('server')}><Icon name="server" /><span>{t.deploy}</span></button>
      </div>
      <div className="home-install-workspace home-install-workspace-full">
        <div id="install-details" className="home-install-result">
        {entry === 'desktop' ? <DesktopDownloads locale={locale} source={source} /> : <>
          <div className="home-install-role">
            <OptionRow label={t.mode}>
              {(['server', 'sandbox'] as const).map(value => <button type="button" key={value} aria-pressed={mode === value} onClick={() => { setMode(value); if (value === 'sandbox' && method === 'docker') setMethod('command'); }}>{t[value]}</button>)}
            </OptionRow>
            <p>{mode === 'server' ? t.serverBody : t.sandboxBody}</p>
          </div>
          <div className="home-install-options home-install-helpers" role="group" aria-label={t.auxiliary}>
            {(['command', 'download', 'agent'] as const).map(value => <button key={value} type="button" aria-pressed={method === value} onClick={() => setMethod(value)}>{t[value]}</button>)}
          </div>
          {method !== 'docker' && <details key={method} className="home-install-advanced" open={method === 'download' ? true : undefined}>
            <summary>{t.advanced} · {t[channel]}</summary>
            <OptionRow label={t.channel}>
              {channels.map(c => <button type="button" key={c} aria-pressed={channel === c} onClick={() => setChannel(c)}>{t[c]}</button>)}
            </OptionRow>
            <p className="home-install-channel-note">{t[`${channel}Description`]}</p>
            {method !== 'agent' && release && !error && <>
              {method === 'download' && <OptionRow label={t.system}>
                {systems.map(system => <button type="button" key={system} aria-pressed={os === system} onClick={() => setOS(system)}>{osLabel(system)}</button>)}
              </OptionRow>}
              <OptionRow label={t.architecture}>
                {method === 'command' && <button type="button" aria-pressed={autoArch} onClick={() => setAutoArch(true)}>{t.automatic}</button>}
                {(method === 'command' ? architectures : targets.map(p => p.arch)).map(arch => <button type="button" key={arch} aria-pressed={method === 'command' ? !autoArch && commandArch === arch : platform?.arch === arch} onClick={() => { setArch(arch); if (method === 'command') setAutoArch(false); }}>{method === 'command' ? ({ amd64: 'x64 / AMD64', arm64: 'ARM64' }[arch] || arch) : archLabel(arch)}</button>)}
              </OptionRow>
              {method === 'command' && <OptionRow label={t.downloadTool}>
                {(['curl', 'wget'] as const).map(value => <button type="button" key={value} aria-pressed={tool === value} onClick={() => setTool(value)}>{value}</button>)}
              </OptionRow>}
            </>}
          </details>}
          {method === 'agent' ? <>
          <AgentInstall key={`${locale}-${channel}-${mode}`} locale={locale} channel={channel} mode={mode} />
          {error ? <p className="home-install-status" role="status">{t.agentUnavailable} <button type="button" onClick={() => setAttempt(v => v + 1)}>{t.retry}</button></p>
            : !catalog ? <p className="home-install-status" role="status">{t.loading}</p>
            : !release || !platform ? <p className="home-install-status" role="status">{t.agentEmpty}</p>
            : <p className="home-install-requirements">{t[channel]} · v{release.version} · {[...new Set(platforms.map(p => osLabel(p.os)))].join(' / ')}</p>}
        </> : method === 'docker' ? <div className="home-install-details">
          <div className="home-install-heading"><h3>{t.dockerTitle}</h3></div>
          <p>{t.dockerBody}</p>
          <div className="home-install-prerequisites"><Icon name="container" /><div><strong>{t.dockerIncludes}</strong><p>{t.dockerRequirements}</p></div></div>
          <InstallCommand command={'git clone https://github.com/molr-ai/molr.git\ncd molr\ncp deploy/docker/server.example.json deploy/docker/server.json\ndocker compose build'} locale={locale} />
          <p>{t.dockerConfigure}</p>
        </div> : error ? <InstallStatus error>{t.loadError} <button type="button" onClick={() => setAttempt(v => v + 1)}>{t.retry}</button></InstallStatus>
          : !catalog ? <InstallStatus>{t.loading}</InstallStatus>
          : !release || !platform ? <InstallStatus>{t.empty}</InstallStatus>
          : <>
            <div className="home-install-result-meta"><span>{method === 'command' ? t[channel] : `${osLabel(os)} / ${archLabel(platform.arch)}`}</span><strong>v{release.version}</strong></div>
            <div className="home-install-details">
              <div className="home-install-heading"><h3>{method === 'command' ? t[mode] : t.downloadTitle}</h3></div>
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
            {method === 'download' && <p className="home-install-requirements">{os === 'darwin' ? `macOS ${platform.requirements.macos}+ · ${locale === 'zh' ? '用户级后台服务；沙盒权限在安装时配置' : 'User background service; sandbox permissions are configured during setup'}` : `${osLabel(os)} · systemd ${platform.requirements.systemd}+ · cgroup v${platform.requirements.cgroup} · ${platform.requirements.packages.join(', ')}`}</p>}
          </>}
          {mode === 'server' && <details className="home-install-other">
            <summary>{t.otherMethods}</summary>
            <button type="button" onClick={() => setMethod('docker')}>{t.docker}</button>
          </details>}
        </>}
        </div>
      </div>
      <div className="home-install-footer"><span>{entry === 'desktop' ? (locale === 'zh' ? '选择与你的操作系统和处理器匹配的安装包。' : 'Choose an installer for your operating system and processor.') : method === 'agent' ? t.agentHint : method === 'docker' ? t.dockerHint : (locale === 'zh' ? '运行模式与资源权限分别配置。' : 'Runtime mode and resource permissions are configured separately.')}</span><a href={localizedDocPath(entry === 'desktop' ? '/docs/admin/deployment/installation/' : method === 'agent' ? agentInstallPath : method === 'docker' ? '/docs/admin/deployment/docker/' : mode === 'sandbox' ? '/docs/admin/sandbox/remote/' : siteLinks.install!, locale)}>{entry === 'server' && method === 'agent' ? t.agentGuide : t.guide}<span aria-hidden="true"> ↗</span></a></div>
    </div>
  </section>;
}
