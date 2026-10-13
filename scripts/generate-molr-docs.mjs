import { readFileSync, writeFileSync, mkdirSync, readdirSync, rmSync, realpathSync, statSync } from 'node:fs';
import { resolve, dirname, posix, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';

export function generateDocumentation(root, { check = false } = {}) {
  const output = resolve(root, 'runtime/internal/molrdocs/content');
  const routes = JSON.parse(readFileSync(resolve(root, 'docs-routes.json'), 'utf8'));
  const base = '/molr/.agents/skills/molr-docs';
  const files = new Map();
  const index = [];
  const publicPaths = new Map();
  for (const route of routes) {
    if (!['docs', 'dev-docs'].includes(route.root) || !/^[A-Za-z0-9_/-]+\.md$/.test(route.file)
        || route.file.split('/').some(p => ['..', 'internal-docs'].includes(p))) throw new Error('Invalid public document path');
    const source = `${route.root}/${route.file}`;
    if (publicPaths.has(source)) throw new Error(`Duplicate document: ${source}`);
    publicPaths.set(source, `https://molr.ai/zh${route.path}`);
    index.push({ path: `${base}/references/${source}`, contentLanguage: 'zh',
      titles: { zh: route.label, en: route.englishLabel },
      topics: { zh: route.group, en: route.englishGroup },
      urls: { zh: publicPaths.get(source), en: `https://molr.ai/en${route.path}` } });
  }
  for (const [source] of publicPaths) {
    const filename = resolve(root, source);
    if (realpathSync(filename) !== filename || !statSync(filename).isFile()) throw new Error(`Not a regular public document: ${source}`);
    let body = readFileSync(filename, 'utf8');
    // Resolve Markdown links without importing their targets into the public bundle.
    body = body.replace(/(!?\[[^\]\n]*\]\()([^\s)]+)(\))/g, (all, before, href, after) => {
      if (/^(?:[a-z][a-z0-9+.-]*:|\/|#)/i.test(href)) return all;
      const [target, anchor = ''] = href.split('#');
      const destination = posix.normalize(posix.join(posix.dirname(source), target));
      if (destination.startsWith('../') || destination.split('/').includes('internal-docs')) throw new Error(`Private or escaping link: ${source}: ${href}`);
      const absolute = resolve(root, destination);
      if (realpathSync(absolute) !== absolute) throw new Error(`Linked symlink: ${destination}`);
      const url = publicPaths.get(destination);
      // Public source-only references remain web links, never fake local bundle paths.
      const link = url || `https://github.com/molr-ai/molr/blob/main/${destination}`;
      return `${before}${link}${anchor ? '#' + anchor : ''}${after}`;
    });
    files.set(`references/${source}`, body);
  }
  files.set('references/index.json', JSON.stringify(index, null, 2) + '\n');
  const hash = createHash('sha256');
  const skill = readFileSync(resolve(root, 'runtime/internal/molrdocs/SKILL.md'), 'utf8');
  hash.update(skill);
  for (const [name, value] of files) hash.update(name).update('\0').update(value).update('\0');
  const revision = hash.digest('hex');
  files.set('SKILL.md', skill.replace('\n---\n', `\nmetadata:\n  bundle-revision: ${revision}\n---\n`));
  files.set('revision.txt', revision + '\n');
  function existing(dir, prefix = '') {
    return readdirSync(dir, { withFileTypes: true }).flatMap(e => e.isDirectory() ? existing(resolve(dir, e.name), prefix + e.name + '/') : [prefix + e.name]);
  }
  if (check) {
    const actual = existing(output);
    if (actual.length !== files.size || actual.some(name => !files.has(name) || readFileSync(resolve(output, name), 'utf8') !== files.get(name))) {
      throw new Error('Stale Molr documentation bundle; run npm run docs:generate');
    }
  } else {
    rmSync(output, { recursive: true, force: true });
    for (const [name, value] of files) {
      const filename = resolve(output, name);
      mkdirSync(dirname(filename), { recursive: true });
      writeFileSync(filename, value);
    }
  }
  return { documents: index.length, revision, output: relative(root, output) };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const result = generateDocumentation(resolve(dirname(fileURLToPath(import.meta.url)), '..'), { check: process.argv.includes('--check') });
  console.log(`Molr docs: ${result.documents} documents, ${result.revision.slice(0, 12)} (${result.output})`);
}
