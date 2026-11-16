import { readFileSync, readdirSync } from 'node:fs';
import { resolve, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

// Display specimens and scaled product illustrations are not application text.
const displayFiles = new Set(['apps/website/src/home.css', 'apps/website/src/docs.css', 'apps/website/src/install.css', 'apps/website/src/styles.css', 'apps/ui-design/src/styles.css']);
export function typographyErrors(path, source) {
  path = path.replaceAll('\\', '/');
  const errors = [];
  source = source.replace(/\/\*[\s\S]*?\*\//g, '');
  if (path === 'apps/website/src/product-preview.css') return errors;
  // The hidden clipboard fallback uses 16px to avoid mobile focus zoom.
  if (path === 'packages/ui/src/components/ChatCopyButton.tsx') source = source.replace('font-size:16px', '');
  for (const match of source.matchAll(/\b(font-size|font)\s*:\s*([^;}\n]+)/g)) {
    const value = match[2];
    const pixels = [...value.matchAll(/([\d.]+)px\b/g)].map(v => Number(v[1]));
    if (!pixels.length) continue;
    if (displayFiles.has(path) && pixels.every(size => size >= 20)) continue;
    errors.push(`${match[1]} must use a shared typography token`);
  }
  for (const match of source.matchAll(/\bfontSize\s*[:=]\s*(?:\d|['"]\d)/g)) errors.push('fontSize must resolve a shared typography token');
  return [...new Set(errors)];
}
function walk(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = resolve(directory, entry.name);
    return entry.isDirectory() ? walk(path) : /\.(css|tsx?)$/.test(entry.name) ? [path] : [];
  });
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const root = fileURLToPath(new URL('../', import.meta.url));
  const paths = ['packages/ui/src', 'apps/webui/src', 'apps/desktop/src', 'apps/ui-design/src', 'apps/website/src'].flatMap(path => walk(resolve(root, path)));
  const errors = paths.flatMap(path => typographyErrors(relative(root, path), readFileSync(path, 'utf8')).map(message => `${relative(root, path)}: ${message}`));
  if (errors.length) { console.error(errors.join('\n')); process.exitCode = 1; }
  else console.log('Typography tokens checked.');
}
