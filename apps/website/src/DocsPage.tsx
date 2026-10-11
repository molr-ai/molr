import { memo, useLayoutEffect, useMemo, useState, type ReactNode } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { SiteHeader } from './SiteHeader';
import { useScrollActivity } from './use-scroll-activity';
import { allDocs, findDoc, remarkHeadingIds, resolveDocLink, type Doc } from './docs-content';
import { MermaidDiagram } from './Mermaid';
import { localizedDocPath } from './doc-locale';
import { navigateSite } from './site-navigation';
import type { Locale } from './site-content';

type SectionRoot = 'docs' | 'dev-docs';
function sectionFor(pathname: string, locale: Locale, root?: SectionRoot) {
  const wanted = root ?? (pathname.includes('/dev-docs') ? 'dev-docs' : 'docs');
  const items = allDocs.filter(doc => doc.root === wanted && doc.locale === locale);
  return {
    root: wanted, items, groups: [...new Set(items.map(doc => doc.group))],
    path: `/${locale}/${wanted}/`,
    title: locale === 'zh' ? (wanted === 'docs' ? '文档' : '开发者文档') : (wanted === 'docs' ? 'Documentation' : 'Developer docs'),
    overview: locale === 'zh' ? (wanted === 'docs' ? '文档概览' : '开发文档概览') : 'overview',
  };
}

type ElementWithProps = { props?: { className?: unknown; children?: unknown } };

function mermaidSource(node: ReactNode): string | null {
  if (!node || typeof node !== 'object' || !('props' in node)) return null;
  const { props } = node as ElementWithProps;
  if (props?.className !== 'language-mermaid') return null;
  return typeof props.children === 'string' ? props.children.replace(/\n$/, '') : null;
}

function Contents({ doc }: { doc: Doc }) {
  return <nav className="docs-contents" aria-label={doc.locale === 'zh' ? '本页目录' : 'On this page'}><p>{doc.locale === 'zh' ? '本页内容' : 'On this page'}</p>{doc.headings.map(heading => <a key={heading.id} className={heading.level === 3 ? 'docs-subheading' : undefined} href={`#${heading.id}`}>{heading.title}</a>)}</nav>;
}

// Search and menu state do not change the document. Keep Markdown parsing and
// diagram mounts tied to the selected document; theme context still updates diagrams.
const Article = memo(function Article({ doc }: { doc: Doc }) {
  const heading = (Tag: 'h2' | 'h3', children: ReactNode, id?: string) => {
    return <Tag id={id}><a href={`#${id}`}>{children}<span className="docs-anchor" aria-hidden="true">#</span></a></Tag>;
  };
  return <Markdown remarkPlugins={[remarkGfm, remarkHeadingIds]} components={{
    h2: ({ children, id }) => heading('h2', children, id),
    h3: ({ children, id }) => heading('h3', children, id),
    a: ({ href, children }) => {
      if (!href) return <span>{children}</span>;
      const link = resolveDocLink(doc, href);
      return link.repositoryPath
        ? <span className="docs-repository-reference">{children}{doc.locale === 'zh' ? '（仓库文件：' : ' (repository file: '}<code>{link.repositoryPath}</code>{doc.locale === 'zh' ? '）' : ')'}</span>
        : <a href={link.href}>{children}</a>;
    },
    table: ({ children }) => <div className="docs-table" tabIndex={0} role="region" aria-label={doc.locale === 'zh' ? '文档表格' : 'Documentation table'}><table>{children}</table></div>,
    pre: ({ children }) => {
      const source = mermaidSource(Array.isArray(children) ? children[0] : children);
      return source === null ? <pre>{children}</pre> : <MermaidDiagram code={source} />;
    },
  }}>{doc.content}</Markdown>;
});

export default function DocsPage({ pathname, navigationRevision, locale, changeLocale }: { pathname: string; navigationRevision: number; locale: Locale; changeLocale: (locale: Locale) => void }) {
  const sidebarScrollRef = useScrollActivity(true), contentsScrollRef = useScrollActivity();
  const doc = findDoc(pathname);
  const section = useMemo(() => sectionFor(pathname, locale, doc?.root), [pathname, locale, doc?.root]);
  const items = section.items;
  const t = (zh: string, en: string) => locale === 'zh' ? zh : en;
  const [query, setQuery] = useState('');
  const [openMenuRevision, setOpenMenuRevision] = useState<number | null>(null);
  const menuOpen = openMenuRevision === navigationRevision;
  const title = doc?.title ?? t('未找到文档', 'Document not found');
  const description = doc ? `${doc.title} — ${t('Molr 使用与部署指南。', 'Molr usage and deployment guide.')}` : t('此文档不存在，请返回文档概览。', 'This document does not exist. Return to the documentation overview.');
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const results = useMemo(() => normalizedQuery
    ? items.filter(entry => entry.searchText.includes(normalizedQuery))
    : [], [items, normalizedQuery]);
  const currentIndex = doc ? items.indexOf(doc) : -1;
  const previous = items[currentIndex - 1];
  const next = doc ? items[currentIndex + 1] : undefined;

  useLayoutEffect(() => {
    document.title = `${title} · Molr Docs`;
    document.querySelector('meta[name="description"]')?.setAttribute('content', description);
    document.querySelector('meta[property="og:title"]')?.setAttribute('content', `${title} · Molr Docs`);
    document.querySelector('meta[property="og:description"]')?.setAttribute('content', description);
    const canonical = `https://molr.ai${doc?.path ?? section.path}`;
    document.querySelector('link[rel="canonical"]')?.setAttribute('href', canonical);
    document.querySelector('meta[property="og:url"]')?.setAttribute('content', canonical);
    document.querySelectorAll('link[hreflang]').forEach(link => link.remove());
    if (doc) for (const language of ['zh', 'en'] as const) {
      const link = document.createElement('link');
      link.rel = 'alternate';
      link.hreflang = language === 'zh' ? 'zh-CN' : 'en';
      link.href = `https://molr.ai${localizedDocPath(doc.path, language)}`;
      document.head.append(link);
    }
  }, [title, description, doc, section.path, locale]);

  const switchLanguage = (nextLocale: Locale) => {
    const counterpart = doc && allDocs.find(entry => entry.root === doc.root && entry.sourceFile === doc.sourceFile && entry.locale === nextLocale);
    let hash = '';
    if (doc && counterpart && window.location.hash) {
      try {
        const index = doc.headings.findIndex(heading => heading.id === decodeURIComponent(window.location.hash.slice(1)));
        if (index >= 0 && counterpart.headings[index]) hash = `#${encodeURIComponent(counterpart.headings[index].id)}`;
      } catch { /* Ignore malformed fragments. */ }
    }
    changeLocale(nextLocale);
    navigateSite(localizedDocPath(pathname, nextLocale) + window.location.search + hash);
  };


  return <div className="docs-page">
    <a className="home-skip" href="#main">{locale === 'zh' ? '跳转到正文' : 'Skip to content'}</a>
    <SiteHeader locale={locale} changeLocale={switchLanguage} active={section.root} />
    <div className="docs-layout" lang={locale === 'zh' ? 'zh-CN' : 'en'}>
      <aside className="docs-sidebar">
        <div className="docs-sidebar-heading"><a href={section.path}>{section.title} <span>DOCS</span></a><button className="docs-menu-toggle" type="button" aria-expanded={menuOpen} aria-controls="docs-navigation" onClick={() => setOpenMenuRevision(menuOpen ? null : navigationRevision)}>{menuOpen ? t('收起目录 −', 'Close menu −') : t('展开目录 +', 'Open menu +')}</button></div>
        <div id="docs-navigation" className={menuOpen ? 'docs-navigation is-open' : 'docs-navigation'}>
          <label className="docs-search"><span>{t('搜索文档', 'Search documentation')}</span><input autoComplete="off" type="search" placeholder={t('搜索文档…', 'Search documentation…')} value={query} onChange={event => setQuery(event.target.value)} /></label>
          <div ref={sidebarScrollRef} className="docs-navigation-scroll">
            {normalizedQuery ? <nav className="docs-search-results" aria-label={t('搜索结果', 'Search results')}><p role="status">{results.length ? t(`找到 ${results.length} 篇文档`, `${results.length} documents found`) : t('没有找到相关文档，试试其他关键词。', 'No documents found. Try another keyword.')}</p>{results.map(entry => <a key={entry.path} href={entry.path} aria-current={entry === doc ? 'page' : undefined}>{entry.label}</a>)}</nav> : <nav aria-label={t('文档目录', 'Documentation navigation')}>{section.groups.map(group => <div className="docs-nav-group" key={group}><p>{group}</p>{items.filter(entry => entry.group === group).map(entry => <a key={entry.path} href={entry.path} aria-current={entry === doc ? 'page' : undefined}>{entry.label}</a>)}</div>)}</nav>}
          </div>
          <div className="docs-scroll-cue" aria-hidden="true"><svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="m6 9 6 6 6-6" /></svg></div>
        </div>
      </aside>
      <main id="main" className="docs-main" tabIndex={-1}>
        <div className="docs-breadcrumb"><a href={section.path}>{section.title}</a><span aria-hidden="true">/</span><span>{doc?.label ?? t('页面不存在', 'Page not found')}</span></div>
        {doc ? <>
          <details key={doc.path} className="docs-mobile-contents"><summary>{t('本页内容', 'On this page')}</summary><Contents doc={doc} /></details>
          <article className="docs-prose"><Article doc={doc} /></article>
          <nav className="docs-pagination" aria-label={t('前后篇文档', 'Previous and next documents')}>{previous ? <a href={previous.path}><span>{t('← 上一篇', '← Previous')}</span><strong>{previous.label}</strong></a> : <div />}{next && <a href={next.path}><span>{t('下一篇 →', 'Next →')}</span><strong>{next.label}</strong></a>}</nav>
        </> : <div className="docs-not-found"><p>404</p><h1>{title}</h1><p>{t('链接可能已变更，请从目录选择文档，或返回概览继续阅读。', 'The link may have changed. Choose a document from the menu or return to the overview.')}</p><a href={section.path}>{t(`返回${section.overview} →`, 'Back to overview →')}</a></div>}
        <footer className="docs-footer"><span>{t('Molr · 让好奇心继续。', 'Molr · Stay curious.')}</span><a href="/">{t('返回官网', 'Back to home')}</a></footer>
      </main>
      {doc && <aside ref={contentsScrollRef} className="docs-toc"><Contents doc={doc} /></aside>}
    </div>
  </div>;
}
