import { useLayoutEffect, useState, type ReactNode } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { SiteHeader } from './SiteHeader';
import { useScrollActivity } from './use-scroll-activity';
import { devDocs, docs, findDoc, remarkHeadingIds, resolveDocLink, type Doc } from './docs-content';
import { MermaidDiagram } from './Mermaid';
import type { Locale } from './site-content';

type SectionRoot = 'docs' | 'dev-docs';
const sections: { root: SectionRoot; title: string; overview: string; groups: string[]; items: Doc[] }[] = [
  { root: 'docs', title: '文档', overview: '文档概览', groups: ['开始使用', '使用指南', '部署与维护'], items: docs },
  { root: 'dev-docs', title: '开发者文档', overview: '开发文档概览', groups: ['概览', '产品', 'Runtime', '前端'], items: devDocs },
];

function sectionFor(pathname: string, root?: SectionRoot) {
  const wanted = root ?? (/^\/dev-docs(?:\/|$)/.test(pathname) ? 'dev-docs' : 'docs') as SectionRoot;
  return sections.find(section => section.root === wanted) ?? sections[0];
}

type ElementWithProps = { props?: { className?: unknown; children?: unknown } };

function mermaidSource(node: ReactNode): string | null {
  if (!node || typeof node !== 'object' || !('props' in node)) return null;
  const { props } = node as ElementWithProps;
  if (props?.className !== 'language-mermaid') return null;
  return typeof props.children === 'string' ? props.children.replace(/\n$/, '') : null;
}

function Contents({ doc }: { doc: Doc }) {
  return <nav className="docs-contents" aria-label="本页目录"><p>本页内容</p>{doc.headings.map(heading => <a key={heading.id} className={heading.level === 3 ? 'docs-subheading' : undefined} href={`#${heading.id}`}>{heading.title}</a>)}</nav>;
}

function Article({ doc }: { doc: Doc }) {
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
        ? <span className="docs-repository-reference">{children}（仓库文件：<code>{link.repositoryPath}</code>）</span>
        : <a href={link.href}>{children}</a>;
    },
    table: ({ children }) => <div className="docs-table" tabIndex={0} role="region" aria-label="文档表格"><table>{children}</table></div>,
    pre: ({ children }) => {
      const source = mermaidSource(Array.isArray(children) ? children[0] : children);
      return source === null ? <pre>{children}</pre> : <MermaidDiagram code={source} />;
    },
  }}>{doc.content}</Markdown>;
}

export default function DocsPage({ pathname, navigationRevision, locale, changeLocale }: { pathname: string; navigationRevision: number; locale: Locale; changeLocale: (locale: Locale) => void }) {
  const sidebarScrollRef = useScrollActivity(true), contentsScrollRef = useScrollActivity();
  const doc = findDoc(pathname);
  const section = sectionFor(pathname, doc?.root);
  const items = section.items;
  const [query, setQuery] = useState('');
  const [openMenuRevision, setOpenMenuRevision] = useState<number | null>(null);
  const menuOpen = openMenuRevision === navigationRevision;
  const title = doc?.title ?? '未找到文档';
  const description = doc ? `${doc.title} — Molr 使用与部署指南。` : `此文档不存在，请返回 Molr ${section.overview}。`;
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const results = items.filter(entry => `${entry.label}\n${entry.content}`.toLocaleLowerCase().includes(normalizedQuery));
  const currentIndex = doc ? items.indexOf(doc) : -1;
  const previous = items[currentIndex - 1];
  const next = doc ? items[currentIndex + 1] : undefined;

  useLayoutEffect(() => {
    document.title = `${title} · Molr Docs`;
    document.querySelector('meta[name="description"]')?.setAttribute('content', description);
    document.querySelector('meta[property="og:title"]')?.setAttribute('content', `${title} · Molr Docs`);
    document.querySelector('meta[property="og:description"]')?.setAttribute('content', description);
    const canonical = `https://molr.ai${doc ? doc.path : section.root === 'dev-docs' ? '/dev-docs/' : '/docs/'}`;
    document.querySelector('link[rel="canonical"]')?.setAttribute('href', canonical);
    document.querySelector('meta[property="og:url"]')?.setAttribute('content', canonical);
  }, [title, description, doc, section, locale]);


  return <div className="docs-page">
    <a className="home-skip" href="#main">{locale === 'zh' ? '跳转到正文' : 'Skip to content'}</a>
    <SiteHeader locale={locale} changeLocale={changeLocale} active={section.root} />
    <div className="docs-layout" lang="zh-CN">
      <aside className="docs-sidebar">
        <div className="docs-sidebar-heading"><a href={section.root === 'dev-docs' ? '/dev-docs/' : '/docs/'}>{section.title} <span>DOCS</span></a><button className="docs-menu-toggle" type="button" aria-expanded={menuOpen} aria-controls="docs-navigation" onClick={() => setOpenMenuRevision(menuOpen ? null : navigationRevision)}>{menuOpen ? '收起目录 −' : '展开目录 +'}</button></div>
        <div id="docs-navigation" className={menuOpen ? 'docs-navigation is-open' : 'docs-navigation'}>
          <label className="docs-search"><span>搜索文档</span><input autoComplete="off" type="search" placeholder="搜索文档…" value={query} onChange={event => setQuery(event.target.value)} /></label>
          <div ref={sidebarScrollRef} className="docs-navigation-scroll">
            {normalizedQuery ? <nav className="docs-search-results" aria-label="搜索结果"><p role="status">{results.length ? `找到 ${results.length} 篇文档` : '没有找到相关文档，试试其他关键词。'}</p>{results.map(entry => <a key={entry.path} href={entry.path} aria-current={entry === doc ? 'page' : undefined}>{entry.label}</a>)}</nav> : <nav aria-label="文档目录">{section.groups.map(group => <div className="docs-nav-group" key={group}><p>{group}</p>{items.filter(entry => entry.group === group).map(entry => <a key={entry.path} href={entry.path} aria-current={entry === doc ? 'page' : undefined}>{entry.label}</a>)}</div>)}</nav>}
            <p className="docs-language-note">文档目前提供中文版本。<br />{locale === 'en' && 'Documentation is currently available in Chinese.'}</p>
          </div>
          <div className="docs-scroll-cue" aria-hidden="true"><svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d="m6 9 6 6 6-6" /></svg></div>
        </div>
      </aside>
      <main id="main" className="docs-main" tabIndex={-1}>
        <div className="docs-breadcrumb"><a href={section.root === 'dev-docs' ? '/dev-docs/' : '/docs/'}>{section.title}</a><span aria-hidden="true">/</span><span>{doc?.label ?? '页面不存在'}</span></div>
        {doc ? <>
          <details key={doc.path} className="docs-mobile-contents"><summary>本页内容</summary><Contents doc={doc} /></details>
          <article className="docs-prose"><Article doc={doc} /></article>
          <nav className="docs-pagination" aria-label="前后篇文档">{previous ? <a href={previous.path}><span>← 上一篇</span><strong>{previous.label}</strong></a> : <div />}{next && <a href={next.path}><span>下一篇 →</span><strong>{next.label}</strong></a>}</nav>
        </> : <div className="docs-not-found"><p>404</p><h1>未找到文档</h1><p>链接可能已变更，请从目录选择文档，或返回概览继续阅读。</p><a href={section.root === 'dev-docs' ? '/dev-docs/' : '/docs/'}>返回{section.overview} →</a></div>}
        <footer className="docs-footer"><span>Molr · 让好奇心继续。</span><a href="/">返回官网</a></footer>
      </main>
      {doc && <aside ref={contentsScrollRef} className="docs-toc"><Contents doc={doc} /></aside>}
    </div>
  </div>;
}
