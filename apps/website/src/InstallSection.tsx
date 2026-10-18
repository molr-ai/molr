import { useEffect, useState } from 'react';
import { Button, Tabs, useChoiceScroll } from '@molr/ui';
import type { HomeCopy } from './home-content';
import type { Locale } from './site-content';
import { installMessages } from './install-content';
import { localizedDocPath } from './doc-locale';
import { installCommand, readCatalog } from './release-data';
import type { Catalog, RuntimeMode } from './release-data';
import './install.css';
import { AgentInstall, agentInstallPath } from './AgentInstall';
import { InstallCommand } from './InstallCommand';
import { InstallStatus } from './InstallStatus';
import { Icon } from './HomeIcon';
import { DesktopDownloads } from './DesktopDownloads';
import { useDesktopRelease } from './use-desktop-release';

const installMethods = ['command', 'agent'] as const;
type InstallMethod = typeof installMethods[number];

export function InstallSection({ copy, locale }: { copy: HomeCopy; locale: Locale }) {
  const t = installMessages[locale];
  const [entry, setEntry] = useState<'desktop' | RuntimeMode>('server');
  const entryList = useChoiceScroll(entry);
  const mode: RuntimeMode = entry === 'sandbox' ? 'sandbox' : 'server';
  const [method, setMethod] = useState<InstallMethod>('command');
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [error, setError] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const source = new URL(import.meta.env.VITE_RELEASE_SOURCE || '/releases', window.location.origin).href.replace(/\/$/, '');
  const desktop = useDesktopRelease(source);
  useEffect(() => {
    const controller = new AbortController(); setError(false); setCatalog(null);
    fetch(`${source}/catalog.json`, { signal: controller.signal, cache: 'no-cache' })
      .then(response => { if (!response.ok) throw new Error('Release catalog unavailable'); return response.json(); })
      .then(value => setCatalog(readCatalog(value)))
      .catch(() => { if (!controller.signal.aborted) setError(true); });
    return () => controller.abort();
  }, [source, attempt]);
  const release = catalog?.channels.stable;
  const platforms = release?.platforms || [];
  const command = installCommand(mode);
  const version = entry === 'desktop' ? (!desktop.error && !desktop.loading ? desktop.release?.version : undefined)
    : !error ? release?.version : undefined;
  const osLabel = (value: string) => ({ linux: 'Linux', darwin: 'macOS', windows: 'Windows' })[value] || value;
  const methodPanel = <>
    {method === 'agent' ? <>
    <AgentInstall key={`${locale}-${mode}`} locale={locale} mode={mode} />
    {error ? <p className="home-install-status" role="status">{t.agentUnavailable} <button type="button" onClick={() => setAttempt(v => v + 1)}>{t.retry}</button></p>
      : !catalog ? <p className="home-install-status" role="status">{t.loading}</p>
      : !release ? <p className="home-install-status" role="status">{t.agentEmpty}</p>
      : <p className="home-install-requirements">{[...new Set(platforms.map(p => osLabel(p.os)))].join(' / ')}</p>}
  </> : error ? <InstallStatus error>{t.loadError} <button type="button" onClick={() => setAttempt(v => v + 1)}>{t.retry}</button></InstallStatus>
    : !catalog ? <InstallStatus>{t.loading}</InstallStatus>
    : !release ? <InstallStatus>{t.empty}</InstallStatus>
    : <>
      <div className="home-install-details">
        <div className="home-install-heading"><h3>{t[mode]}</h3></div>
        <p>{t.commandBody}</p>
        <InstallCommand key={command} command={command} locale={locale} />
      </div>
      <div className="home-install-release"><time dateTime={release.published_at}>{new Date(release.published_at).toLocaleDateString(locale === 'zh' ? 'zh-CN' : 'en-US', { timeZone: 'UTC' })}</time><a href={release.notes_url}>{t.notes}<span aria-hidden="true"> ↗</span></a></div>
    </>}
  </>;
  const servicePanel = <>
    <div className="home-install-role">
      <p>{mode === 'server' ? t.serverBody : t.sandboxBody}</p>
    </div>
    <Tabs label={t.auxiliary} value={method} onValueChange={setMethod} className="home-install-method-tabs"
      items={installMethods.map(value => ({ value, label: t[value], content: method === value ? methodPanel : null }))} />
  </>;
  return <section id="install" data-palette="forest" data-theme="dark" className="home-start home-container" aria-labelledby="start-title">
    <p className="home-section-label">{copy.startLabel}</p>
    <h2 id="start-title">{copy.startHeading}</h2>
    <p className="home-section-intro">{t.browser}</p>
    <div className="home-installer">
      <div className="home-install-header">
        <div ref={entryList} className="home-install-entries" role="group" aria-label={t.method}>
          {(['desktop', 'server', 'sandbox'] as const).map(value => <Button key={value} variant="ghost" className="home-install-entry-button" aria-pressed={entry === value} aria-controls="install-details"
            onClick={() => setEntry(value)}>
            <Icon name={value === 'desktop' ? 'devices' : value === 'server' ? 'server' : 'shield'} />{t[value]}
          </Button>)}
        </div>
        {version && <span className="home-install-version">v{version}</span>}
      </div>
      <div id="install-details" className="home-install-result">
        {entry === 'desktop' ? <DesktopDownloads locale={locale} releaseState={desktop} /> : servicePanel}
      </div>
      <div className="home-install-footer"><span>{entry === 'desktop' ? (locale === 'zh' ? '选择与你的操作系统和处理器匹配的安装包。' : 'Choose an installer for your operating system and processor.') : method === 'agent' ? t.agentHint : (locale === 'zh' ? '运行模式与资源权限分别配置。' : 'Runtime mode and resource permissions are configured separately.')}</span><a href={localizedDocPath(entry === 'desktop' ? '/docs/admin/deployment/installation/' : method === 'agent' ? agentInstallPath : mode === 'sandbox' ? '/docs/admin/sandbox/remote/' : '/docs/admin/deployment/installation/', locale)}>{entry !== 'desktop' && method === 'agent' ? t.agentGuide : t.guide}<span aria-hidden="true"> ↗</span></a></div>
    </div>
  </section>;
}
