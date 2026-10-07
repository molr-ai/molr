import { useEffect, useState } from 'react';
import { useTheme } from '@molr/ui';

let renderCount = 0;

/**
 * Renders a mermaid diagram authored in the documentation. Diagrams come from
 * repository markdown, and mermaid runs with strict security sanitization; a
 * rendering failure falls back to the diagram source instead of a blank box.
 */
export function MermaidDiagram({ code }: { code: string }) {
  const { theme } = useTheme();
  const [svg, setSvg] = useState('');
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let cancelled = false;
    setFailed(false);
    import('mermaid').then(async ({ default: mermaid }) => {
      mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', theme: theme === 'dark' ? 'dark' : 'default', fontFamily: 'var(--font-sans)' });
      const { svg } = await mermaid.render(`docs-mermaid-${(renderCount += 1)}`, code);
      if (!cancelled) setSvg(svg);
    }).catch(() => { if (!cancelled) setFailed(true); });
    return () => { cancelled = true; };
  }, [code, theme]);
  if (failed) return <pre className="docs-mermaid-fallback"><code>{code}</code></pre>;
  return <div className="docs-mermaid" role="img" aria-busy={!svg} dangerouslySetInnerHTML={svg ? { __html: svg } : undefined} />;
}
