import artwork from './logo-assets.json';
import { paletteBrandColors } from './palettes';

/** Custom asset directories still load their own artwork. Built-in palettes
 * travel with the application, including variants not yet displayed. */
export function getBundledLogo(base: string, filename: string): string | undefined {
  const match = /^\/brand(?:\/themes\/(forest|ocean|iris|sand|rose|monochrome))?$/.exec(base);
  if (!match) return;
  const source = (artwork as Record<string, string>)[filename];
  if (!source || !match[1] || match[1] === 'forest') return source;
  const colors = paletteBrandColors[match[1] as keyof typeof paletteBrandColors];
  const replacements: Record<string, string> = { '#263b31': colors.ink, '#f5f6ef': colors.paper, '#d5f478': colors.accent };
  return source.replace(/#(?:263b31|f5f6ef|d5f478)\b/gi, color => replacements[color.toLowerCase()]);
}

export function logoDataUrl(source: string, asset: string) {
  // Change the payload as well as the fragment: image caches may otherwise
  // share a completed SVG clock across replays. Escaping hyphens makes the
  // identifier safe inside an XML comment, including custom asset paths.
  const identifier = encodeURIComponent(asset).replace(/-/g, '%2D');
  return `data:image/svg+xml,${encodeURIComponent(`${source}\n<!--${identifier}-->`)}#${asset}`;
}
