import { useEffect, useState } from 'react';
import type { HomeCopy } from './home-content';
import type { Locale } from './site-content';
import { installMessages } from './install-content';
import { localizedDocPath } from './doc-locale';
import { siteLinks } from './site-links';
import { channels, installCommand, readCatalog } from './release-data';
import type { Catalog, Channel } from './release-data';
import './install.css';
import { InstallCommand } from './InstallCommand';

export function InstallSection({ copy, locale }: { copy: HomeCopy; locale: Locale }) {
  const t = installMessages[locale];
  const [method, setMethod] = useState<'command' | 'download'>('command');
  const [channel, setChannel] = useState<Channel>('stable');
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [platformID, setPlatformID] = useState('');
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
  const platform = platforms.find(p => p.id === platformID) || platforms[0];
  const command = release ? installCommand(channel) : '';
  const chooseChannel = (next: Channel) => setChannel(next);
  return <section id="install" data-palette="forest" data-theme="dark" className="home-start home-container" aria-labelledby="start-title">
    <p className="home-section-label">{copy.startLabel}</p>
    <h2 id="start-title">{copy.startHeading}</h2>
    <p className="home-section-intro">{t.browser}</p>
    <div className="home-installer">
      <div className="home-install-methods home-install-channels" role="group" aria-label={t.channel}>
        {channels.map(c => <button type="button" key={c} aria-pressed={channel === c} onClick={() => chooseChannel(c)}>{t[c]}</button>)}
      </div>
      <p className="home-install-channel-note">{channel === 'stable' ? t.stableDescription : channel === 'preview' ? t.previewDescription : t.nightlyDescription}</p>
      {error ? <p className="home-install-status" role="alert">{t.loadError} <button type="button" onClick={() => setAttempt(v => v + 1)}>{t.retry}</button></p>
        : !catalog ? <p className="home-install-status" role="status">{t.loading}</p>
        : !release ? <p className="home-install-status" role="status">{t.empty}</p>
        : <>
          <div className="home-install-release"><strong>v{release.version}</strong><time dateTime={release.published_at}>{new Date(release.published_at).toLocaleDateString(locale === 'zh' ? 'zh-CN' : 'en-US', { timeZone: 'UTC' })}</time><a href={release.notes_url}>{t.notes}</a></div>
          <div className="home-install-platform-label" id="install-platform-label">{t.platform}</div>
          <div className="home-install-platforms" role="group" aria-labelledby="install-platform-label">
            {platforms.map(p => <button type="button" key={p.id} className="home-install-platform" aria-pressed={platform?.id === p.id} onClick={() => setPlatformID(p.id)}><strong>{p.label}</strong>{platform?.id === p.id && <span className="home-install-selected" aria-label={t.selected}>✓</span>}</button>)}
          </div>
          <div className="home-install-methods" role="group" aria-label={t.method}>
            <button type="button" aria-pressed={method === 'command'} aria-controls="install-details" onClick={() => setMethod('command')}>{t.command}<small>{t.recommended}</small></button>
            <button type="button" aria-pressed={method === 'download'} aria-controls="install-details" onClick={() => setMethod('download')}>{t.download}</button>
          </div>
          <div id="install-details" className="home-install-details">
            <div className="home-install-heading"><h3>{method === 'command' ? t.commandTitle : t.downloadTitle}</h3></div>
            <p>{method === 'command' ? t.commandBody : t.downloadBody}</p>
            {method === 'command' ? <>
              <InstallCommand key={command} command={command} locale={locale} />
              <ol className="home-install-steps">{t.steps.map((step, i) => <li key={step}><span>{i + 1}</span>{step}</li>)}</ol>
            </> : platform && <div className="home-install-packages"><article>
              <span className="home-install-archive">tar.gz</span><h4>{platform.label}</h4>
              <a className="home-button home-button-secondary" href={platform.url}>{t.downloadAction}</a>
              <a className="home-install-checksum" href={platform.checksum_url}>{t.checksum}</a>
              <code className="home-install-digest">SHA-256: {platform.sha256}</code>
              <p>{t.offline}</p><code className="home-install-digest">sha256sum -c {platform.file}.sha256<br />tar -xzf {platform.file}<br />cd molr<br />./bin/molr-setup</code>
            </article></div>}
          </div>
          {platform && <p className="home-install-requirements">{platform.os} · systemd {platform.requirements.systemd}+ · cgroup v{platform.requirements.cgroup} · {platform.requirements.packages.join(', ')}</p>}
        </>}
      <div className="home-install-footer"><span>{t.targetHint}</span><a href={localizedDocPath(siteLinks.install!, locale)}>{t.guide}<span aria-hidden="true"> ↗</span></a></div>
    </div>
  </section>;
}
