import { Logo, useTheme } from '@molr/ui';
import { GithubMark } from './GithubMark';
import { homeMessages } from './home-content';
import type { Locale } from './site-content';
import { siteLinks } from './site-links';

export function SiteHeader({ locale, changeLocale, active }: { locale: Locale; changeLocale: (locale: Locale) => void; active: 'home' | 'docs' | 'dev-docs' }) {
  const copy = homeMessages[locale];
  const { theme, changePreference } = useTheme();
  return <header className="home-header home-container">
    <a href="/" className="home-brand" aria-label="Molr"><Logo animation="daily" variant={theme === 'dark' ? 'light' : 'dark'} /></a>
    <nav aria-label={copy.nav}>
      <a href="/" aria-current={active === 'home' ? 'page' : undefined}>{copy.home}</a>
      <a href={siteLinks.userDocs!} aria-current={active === 'docs' ? 'page' : undefined}>{copy.userDocs}</a>
      {siteLinks.devDocs ? <a href={siteLinks.devDocs} aria-current={active === 'dev-docs' ? 'page' : undefined}>{copy.devDocs}</a> : <a className="home-placeholder" role="link" aria-disabled="true" title={copy.devDocsSoon}>{copy.devDocs}</a>}
      {siteLinks.github ? <a href={siteLinks.github}><GithubMark />{copy.github}</a> : <a className="home-placeholder" role="link" aria-disabled="true" title={copy.githubSoon}><GithubMark />{copy.github}</a>}
    </nav>
    <div className="home-actions">
      <button type="button" onClick={() => changeLocale(locale === 'zh' ? 'en' : 'zh')} aria-label={copy.language} title={copy.language}><svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m5 8 6 6" /><path d="m4 14 6-6 2-3" /><path d="M2 5h12" /><path d="M7 2h1" /><path d="m22 22-5-10-5 10" /><path d="M14 18h6" /></svg><span>{locale === 'zh' ? 'EN' : '中'}</span></button>
      <span className="home-action-divider" />
      <button type="button" onClick={() => changePreference(theme === 'dark' ? 'light' : 'dark')} aria-label={theme === 'dark' ? copy.light : copy.dark} title={theme === 'dark' ? copy.light : copy.dark}><svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden="true">{theme === 'dark' ? <><circle cx="12" cy="12" r="4" /><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5" /></> : <path d="M20 15.5A9 9 0 0 1 8.5 4a9 9 0 1 0 11.5 11.5Z" />}</svg></button>
    </div>
  </header>;
}
