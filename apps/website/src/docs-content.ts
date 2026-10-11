import { allDocRoutes, type DocRoute } from './docs-routes';
import sources from 'virtual:molr-documents';

export function headingId(text: string) {
  return text.toLowerCase().replace(/[^\p{L}\p{N}\s_-]/gu, '').trim().replace(/\s/g, '-');
}

function loadDoc(entry: DocRoute) {
  const content = sources[`../../../${entry.root}/${entry.file}`];
  if (!content) throw new Error(`Missing documentation: ${entry.root}/${entry.file}`);
  const counts = new Map<string, number>();
  // Fenced examples must not become navigation headings.
  const prose = content.replace(/^(`{3,}|~{3,})[^\n]*\n[\s\S]*?^\1\s*$/gm, '');
  const headings = [...prose.matchAll(/^(#{2,3}) (.+)$/gm)].map(match => {
    const base = headingId(match[2]);
    const count = counts.get(base) ?? 0;
    counts.set(base, count + 1);
    return { title: match[2], level: match[1].length, id: count ? `${base}-${count}` : base };
  });
  return { ...entry, content, headings, searchText: `${entry.label}\n${content}`.toLocaleLowerCase(), title: /^# (.+)$/m.exec(content)?.[1] ?? entry.label };
}

export const allDocs = allDocRoutes.map(loadDoc);
export type Doc = typeof allDocs[number];

export function findDoc(path: string) {
  return allDocs.find(doc => doc.path.replace(/\/$/, '') === path.replace(/\/$/, ''));
}

export function resolveDocLink(doc: Doc, href: string) {
  if (/^(?:[a-z][a-z\d+.-]*:|\/\/|#)/i.test(href)) return { href };
  const source = new URL(href, `https://docs.invalid/${doc.root}/${doc.file}`);
  const target = allDocs.find(entry => `/${entry.root}/${entry.file}` === source.pathname);
  if (target) return { href: target.path + source.search + source.hash };
  // Repository-only references have no public URL. Keep their path visible
  // instead of sending readers to a nonexistent page or a private Git server.
  return { repositoryPath: decodeURIComponent(source.pathname.slice(1)) };
}

interface MarkdownNode {
  type: string;
  value?: string;
  depth?: number;
  children?: MarkdownNode[];
  data?: { hProperties?: Record<string, unknown> };
}

// Assign IDs during Markdown transformation, not React rendering (which may
// run more than once under StrictMode).
export function remarkHeadingIds() {
  return (tree: MarkdownNode) => {
    const counts = new Map<string, number>();
    const text = (node: MarkdownNode): string => node.value ?? node.children?.map(text).join('') ?? '';
    const visit = (node: MarkdownNode) => {
      if (node.type === 'heading' && (node.depth === 2 || node.depth === 3)) {
        const base = headingId(text(node));
        const count = counts.get(base) ?? 0;
        counts.set(base, count + 1);
        node.data = { ...node.data, hProperties: { ...node.data?.hProperties, id: count ? `${base}-${count}` : base } };
      }
      node.children?.forEach(visit);
    };
    visit(tree);
  };
}
