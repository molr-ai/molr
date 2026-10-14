import { DropdownMenu, Logo, UIIcon, useTheme } from '@molr/ui';
import { GithubMark } from './GithubMark';
import { homeMessages } from './home-content';
import type { Locale } from './site-content';
import { localizedDocPath } from './doc-locale';
import { siteLinks } from './site-links';
import { navigateSite } from './site-navigation';
import type { DocSection } from './docs-routes';

export function SiteHeader({ locale, changeLocale, active, docSection }: { locale: Locale; changeLocale: (locale: Locale) => void; active: 'home' | 'docs'; docSection?: DocSection }) {
  const copy = homeMessages[locale];
  const { theme, changePreference } = useTheme();
  return <header className="home-header home-container">
    <a href="/" className="home-brand" aria-label="Molr"><Logo animation="daily" variant={theme === 'dark' ? 'light' : 'dark'} /></a>
    <nav aria-label={copy.nav}>
      <a href="/" aria-current={active === 'home' ? 'page' : undefined}>{copy.home}</a>
      <DropdownMenu label={copy.docs} trigger={<button type="button" className="home-docs-trigger" data-active={active === 'docs' || undefined}>{copy.docs}<UIIcon name="chevron-down" className="home-docs-chevron" aria-hidden="true" /></button>} items={[
        { id: 'user', label: copy.userDocs, selected: docSection === 'user', onSelect: () => navigateSite(localizedDocPath(siteLinks.userDocs!, locale)) },
        { id: 'admin', label: copy.adminDocs, selected: docSection === 'admin', onSelect: () => navigateSite(localizedDocPath(siteLinks.adminDocs!, locale)) },
        { id: 'dev', label: copy.devDocs, selected: docSection === 'dev', onSelect: () => navigateSite(localizedDocPath(siteLinks.devDocs!, locale)) },
      ]} />
      {siteLinks.github ? <a href={siteLinks.github}><GithubMark />{copy.github}</a> : <a className="home-placeholder" role="link" aria-disabled="true" title={copy.githubSoon}><GithubMark />{copy.github}</a>}
    </nav>
    <div className="home-actions">
      <button type="button" onClick={() => changeLocale(locale === 'zh' ? 'en' : 'zh')} aria-label={copy.language} title={copy.language}><svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m5 8 6 6" /><path d="m4 14 6-6 2-3" /><path d="M2 5h12" /><path d="M7 2h1" /><path d="m22 22-5-10-5 10" /><path d="M14 18h6" /></svg><span>{locale === 'zh' ? 'EN' : '中'}</span></button>
      <span className="home-action-divider" />
      <button type="button" onClick={() => changePreference(theme === 'dark' ? 'light' : 'dark')} aria-label={theme === 'dark' ? copy.light : copy.dark} title={theme === 'dark' ? copy.light : copy.dark}><svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden="true">{theme === 'dark' ? <><circle cx="12" cy="12" r="4" /><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5" /></> : <path d="M20 15.5A9 9 0 0 1 8.5 4a9 9 0 1 0 11.5 11.5Z" />}</svg></button>
    </div>
  </header>;
}
