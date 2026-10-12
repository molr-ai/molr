export type SearchSection = { title: string; id: string; text: string };
export type SearchIndex = { title: string; label: string; sections: SearchSection[] };
export type SearchDocument = { path: string; label: string; title: string; group: string; searchIndex: SearchIndex };
export type MatchKind = 'title' | 'heading' | 'body';

function plainText(value: string) {
  return value.replace(/!?\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/\[!(?:WARNING|PROMPT)\]/g, '')
    .replace(/^ {0,3}(?:> ?|[-+*] +|\d+[.)] +|#{1,6} +)/gm, '')
    .replace(/`+([^`]+)`+/g, '$1').replace(/\*\*([^*]+)\*\*/g, '$1').replace(/__([^_]+)__/g, '$1')
    .replace(/\s+/g, ' ').trim();
}

export function indexDocument(content: string, title: string, label: string, headings: { title: string; id: string }[]): SearchIndex {
  const sections: SearchSection[] = [{ title: '', id: '', text: '' }];
  let headingIndex = 0;
  let fence = '';
  for (const line of content.replace(/<!--[\s\S]*?-->/g, '').split('\n')) {
    const delimiter = /^\s*(`{3,}|~{3,})/.exec(line)?.[1];
    if (delimiter && (!fence || delimiter[0] === fence[0] && delimiter.length >= fence.length)) {
      fence = fence ? '' : delimiter;
      continue;
    }
    if (!fence && /^# /.test(line)) continue;
    if (!fence && /^#{2,3} /.test(line)) {
      const heading = headings[headingIndex++];
      sections.push({ title: plainText(heading?.title ?? line), id: heading?.id ?? '', text: '' });
    } else sections[sections.length - 1].text += line + '\n';
  }
  return { title: plainText(title), label: plainText(label), sections: sections.map(section => ({ ...section, text: plainText(section.text) })) };
}

function excerpt(text: string, term: string) {
  const index = text.toLowerCase().indexOf(term);
  const start = Math.max(0, index - 55);
  const end = Math.min(text.length, Math.max(start + 180, index + term.length + 55));
  return (start ? '…' : '') + text.slice(start, end) + (end < text.length ? '…' : '');
}

export function searchDocuments<T extends SearchDocument>(documents: T[], query: string) {
  const term = query.trim().toLowerCase();
  if (!term) return [];
  return documents.flatMap(doc => {
    const index = doc.searchIndex;
    const titleMatch = [index.title, index.label].some(text => text.toLowerCase().includes(term));
    const heading = index.sections.find(section => section.title.toLowerCase().includes(term));
    const body = index.sections.find(section => section.text.toLowerCase().includes(term));
    const kinds: MatchKind[] = [...(titleMatch ? ['title' as const] : []), ...(heading ? ['heading' as const] : []), ...(body ? ['body' as const] : [])];
    if (!kinds.length) return [];
    const target = titleMatch ? undefined : heading ?? body;
    const preview = target ?? body ?? index.sections.find(section => section.text);
    return [{ doc, kinds, href: doc.path + (target?.id ? `#${encodeURIComponent(target.id)}` : ''), section: target?.title ?? '', snippet: excerpt(preview?.text ?? '', term), rank: titleMatch ? 0 : heading ? 1 : 2 }];
  }).sort((a, b) => a.rank - b.rank);
}
