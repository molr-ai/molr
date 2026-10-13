import reviewedSources from '../../../docs-routes.json' with { type: 'json' };
import type { Locale } from './site-content';

type RouteRoot = 'docs' | 'dev-docs';
type SourceRoute = { file: string; path: string; label: string; group: string; englishLabel: string; englishGroup: string; root: RouteRoot };

const sources = reviewedSources as SourceRoute[];

const userSources = sources.filter(source => source.root === 'docs');

const developerSources = sources.filter(source => source.root === 'dev-docs');

export type DocRoute = Omit<SourceRoute, 'englishLabel' | 'englishGroup'> & { locale: Locale; sourceFile: string };

function localize(sources: SourceRoute[], locale: Locale): DocRoute[] {
  return sources.map(({ englishLabel, englishGroup, ...source }) => ({
    ...source,
    locale,
    sourceFile: source.file,
    file: locale === 'en' ? `en/${source.file}` : source.file,
    path: `/${locale}${source.path}`,
    label: locale === 'en' ? englishLabel : source.label,
    group: locale === 'en' ? englishGroup : source.group,
  }));
}

export const docRoutes = localize(userSources, 'zh');
export const devDocRoutes = localize(developerSources, 'zh');
export const allDocRoutes = [...docRoutes, ...devDocRoutes, ...localize(userSources, 'en'), ...localize(developerSources, 'en')];
