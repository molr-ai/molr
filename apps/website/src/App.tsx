import { lazy, Suspense } from 'react';
import { SiteStatusPage } from './SiteStatusPage';
import type { SiteMode } from './site-mode';
import { useLocale } from './use-locale';
import { RouteEffects, useSiteNavigation } from './site-navigation';

import { isDocPath } from './doc-locale';

const HomePage = lazy(() => import('./HomePage').then(module => ({ default: module.HomePage })));
const DocsPage = lazy(() => import('./DocsPage'));

export default function App({ mode }: { mode: SiteMode }) {
  const navigation = useSiteNavigation(mode === 'live');
  const { copy, logoAction, locale, changeLocale } = useLocale(mode, navigation.pathname);

  if (mode !== 'live') return <SiteStatusPage mode={mode} heading={copy.heading} logoAction={logoAction} />;

  return <Suspense fallback={<main className="docs-loading">{locale === 'zh' ? '加载中…' : 'Loading…'}</main>}>
    <RouteEffects navigation={navigation}>
      {isDocPath(navigation.pathname)
        ? <DocsPage pathname={navigation.pathname} navigationRevision={navigation.revision} locale={locale} changeLocale={changeLocale} />
        : <HomePage heading={copy.heading} locale={locale} changeLocale={changeLocale} />}
    </RouteEffects>
  </Suspense>;
}
