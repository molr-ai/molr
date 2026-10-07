export type CodeToken = { text: string; kind?: 'property' | 'string' | 'number' | 'boolean' | 'null' | 'punctuation'; offset: number };

// Keep original source (including number precision and whitespace). Limit work
// and DOM nodes for diagnostic payloads; fallback still contains the full text.
export function highlightJSON(code: string): CodeToken[] | null {
  if (code.length > 100_000) return null;
  try { JSON.parse(code); } catch { return null; }
  const pattern = /("(?:\\.|[^"\\])*"\s*)(?=:)|("(?:\\.|[^"\\])*")|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)|\b(true|false)\b|\b(null)\b|([{}\[\],:])/g;
  const tokens: CodeToken[] = [];
  let offset = 0;
  for (const match of code.matchAll(pattern)) {
    if (tokens.length >= 10_000) return null;
    if (match.index > offset) tokens.push({ text: code.slice(offset, match.index), offset });
    const kind = match[1] ? 'property' : match[2] ? 'string' : match[3] ? 'number' : match[4] ? 'boolean' : match[5] ? 'null' : 'punctuation';
    tokens.push({ text: match[0], kind, offset: match.index });
    offset = match.index + match[0].length;
  }
  if (offset < code.length) tokens.push({ text: code.slice(offset), offset });
  return tokens;
}
