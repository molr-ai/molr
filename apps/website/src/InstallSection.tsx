import { useState } from 'react';
import type { HomeCopy } from './home-content';
import type { Locale } from './site-content';
import { installMessages } from './install-content';
import { siteLinks } from './site-links';
import './install.css';

function PlatformIcon({ platform }: { platform: string }) {
  return <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {platform === 'Linux' ? <><rect x="3" y="4" width="18" height="16" rx="3" /><path d="m7 9 3 3-3 3m6 0h4" /></>
      : platform === 'macOS' ? <><rect x="3" y="3" width="18" height="14" rx="2" /><path d="M8 21h8m-4-4v4" /></>
      : <><path d="m3 5 8-1v7H3V5Zm10-1 8-1v8h-8V4ZM3 13h8v7l-8-1v-6Zm10 0h8v8l-8-1v-7Z" /></>}
  </svg>;
}

export function InstallSection({ copy, locale }: { copy: HomeCopy; locale: Locale }) {
  const t = installMessages[locale];
  const [method, setMethod] = useState<'command' | 'download'>('command');
  return <section id="install" data-palette="forest" data-theme="dark" className="home-start home-container" aria-labelledby="start-title">
    <p className="home-section-label">{copy.startLabel}</p>
    <h2 id="start-title">{copy.startHeading}</h2>
    <p className="home-section-intro">{t.browser}</p>
    <div className="home-installer">
      <div className="home-install-platform-label" id="install-platform-label">{t.platform}</div>
      <div className="home-install-platforms" role="group" aria-labelledby="install-platform-label">
        {['Linux', 'macOS', 'Windows'].map(platform => <button type="button" key={platform} className="home-install-platform" disabled={platform !== 'Linux'} aria-pressed={platform === 'Linux'}>
          <PlatformIcon platform={platform} />
          <span><strong>{platform}</strong><small>{platform === 'Linux' ? 'x64 / ARM64' : t.soon}</small></span>
          {platform === 'Linux' && <span className="home-install-selected" role="img" aria-label={t.selected}>✓</span>}
        </button>)}
      </div>
      <div className="home-install-methods" role="group" aria-label={t.method}>
        <button type="button" aria-pressed={method === 'command'} aria-controls="install-details" onClick={() => setMethod('command')}>{t.command}<small>{t.recommended}</small></button>
        <button type="button" aria-pressed={method === 'download'} aria-controls="install-details" onClick={() => setMethod('download')}>{t.download}</button>
      </div>
      <div id="install-details" className="home-install-details">
        <div className="home-install-heading"><h3>{method === 'command' ? t.commandTitle : t.downloadTitle}</h3><span className="home-install-soon">{t.soon}</span></div>
        <p>{method === 'command' ? t.commandBody : t.downloadBody}</p>
        {method === 'command' ? <>
          <div className="home-install-terminal">
            <div><span>Terminal</span><button type="button" disabled>{t.copyCommand}</button></div>
            <p><span aria-hidden="true">$</span> {t.commandPending}</p>
          </div>
          <ol className="home-install-steps">{t.steps.map((step, index) => <li key={step}><span aria-hidden="true">{index + 1}</span>{step}</li>)}</ol>
        </> : <div className="home-install-packages">
          {(['x64', 'arm64'] as const).map(arch => <article key={arch}>
            <span className="home-install-archive">{t.archive}</span><h4>{arch === 'x64' ? 'x64 / AMD64' : 'ARM64'}</h4><p>{t[arch]}</p>
            <button className="home-button home-button-secondary" type="button" disabled>{t.downloadAction}</button>
          </article>)}
        </div>}
      </div>
      <div className="home-install-footer"><span>{t.requirements}</span><a href={siteLinks.install!}>{t.guide}<span aria-hidden="true"> ↗</span></a></div>
    </div>
  </section>;
}
