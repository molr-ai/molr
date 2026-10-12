interface MarkdownNode {
  type: string;
  value?: string;
  children?: MarkdownNode[];
  data?: { hProperties?: Record<string, unknown> };
}

// Recognize explicit callout markers only at the start of a blockquote.
export function remarkCallouts() {
  return (tree: MarkdownNode) => {
    const visit = (node: MarkdownNode) => {
      // Empty HTML comments separate adjacent quotes without visible content.
      if (node.children) node.children = node.children.filter(child => child.type !== 'html' || child.value?.trim() !== '<!-- -->');
      const paragraph = node.children?.[0];
      const marker = paragraph?.children?.[0];
      const kind = /^\[!(WARNING|PROMPT)\](?:\r?\n|$)/.exec(marker?.value ?? '')?.[1];
      if (node.type === 'blockquote' && paragraph?.type === 'paragraph' && marker?.type === 'text' && kind) {
        marker.value = marker.value!.replace(/^\[!(?:WARNING|PROMPT)\](?:\r?\n|$)/, '');
        if (!marker.value) paragraph.children!.shift();
        if (!paragraph.children?.length) node.children!.shift();
        node.data = { ...node.data, hProperties: { ...node.data?.hProperties, className: kind === 'WARNING' ? 'docs-warning' : 'docs-prompt' } };
      }
      node.children?.forEach(visit);
    };
    visit(tree);
  };
}
