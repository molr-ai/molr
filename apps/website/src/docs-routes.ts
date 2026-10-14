import reviewedSources from '../../../docs-routes.json' with { type: 'json' };
import type { Locale } from './site-content';

export type DocSection = 'overview' | 'user' | 'admin' | 'dev';
type SourceRoute = { file: string; path: string; label: string; group: string; englishLabel: string; englishGroup: string; root: 'docs'; section: DocSection };
const sources = reviewedSources as SourceRoute[];
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

export const allDocRoutes = [...localize(sources, 'zh'), ...localize(sources, 'en')];
export const docRoutes = allDocRoutes.filter(route => route.locale === 'zh' && route.section === 'user');
export const adminDocRoutes = allDocRoutes.filter(route => route.locale === 'zh' && route.section === 'admin');
export const devDocRoutes = allDocRoutes.filter(route => route.locale === 'zh' && route.section === 'dev');
