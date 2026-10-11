import assert from 'node:assert/strict';
import { readFile, readdir, stat } from 'node:fs/promises';
import { dirname, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';
import { docRoutes, devDocRoutes } from '../src/docs-routes.ts';

const root = fileURLToPath(new URL('../../../', import.meta.url));
const routes = [...docRoutes, ...devDocRoutes];
const repositoryOnly = ['dev-docs/contributing/development.md'];

async function markdownFiles(directory) {
  const entries = await readdir(resolve(root, directory), { withFileTypes: true });
  return (await Promise.all(entries.map(entry => {
    const path = `${directory}/${entry.name}`;
    return entry.isDirectory() ? markdownFiles(path) : entry.name.endsWith('.md') ? [path] : [];
  }))).flat();
}

function prose(source) {
  return source.replace(/^(`{3,}|~{3,})[^\n]*\n[\s\S]*?^\1\s*$/gm, '');
}

function headingIds(source) {
  const counts = new Map();
  return [...prose(source).matchAll(/^#{2,3} (.+)$/gm)].map(([, title]) => {
    const base = title.replace(/[`*]/g, '').toLowerCase().replace(/[^\p{L}\p{N}\s_-]/gu, '').trim().replace(/\s/g, '-');
    const count = counts.get(base) ?? 0;
    counts.set(base, count + 1);
    return count ? `${base}-${count}` : base;
  });
}

test('current documents have explicit unique routes and the overview comes first', async () => {
  assert.equal(docRoutes[0].file, 'README.md');
  assert.equal(devDocRoutes[0].file, 'README.md');
  assert.equal(new Set(routes.map(route => route.path)).size, routes.length);
  const registered = routes.map(route => `${route.root}/${route.file}`);
  assert.equal(new Set(registered).size, registered.length);
  const files = [...await markdownFiles('docs'), ...await markdownFiles('dev-docs')];
  assert.deepEqual(files.sort(), [...registered, ...repositoryOnly].sort());
  for (const route of routes) {
    assert(!route.file.includes('..'));
    assert(route.path.startsWith(`/${route.root}/`));
    assert(route.group.trim());
    const source = await readFile(resolve(root, route.root, route.file), 'utf8');
    assert.equal([...prose(source).matchAll(/^# /gm)].length, 1, route.file);
  }
});

test('reorganized documents and contributor entry points resolve files and chapter anchors', async () => {
  const files = [...await markdownFiles('docs'), ...await markdownFiles('dev-docs'), 'AGENTS.md', 'README.md', 'README.zh-CN.md'];
  for (const file of files) {
    const source = await readFile(resolve(root, file), 'utf8');
    for (const [, href] of prose(source).matchAll(/\]\(([^\s)]+)\)/g)) {
      if (/^(?:[a-z][a-z\d+.-]*:|\/)/i.test(href)) continue;
      const [path, fragment] = href.split('#');
      const target = path ? resolve(root, dirname(file), decodeURIComponent(path)) : resolve(root, file);
      const label = `${file} -> ${href}`;
      assert(!relative(root, target).split('/').includes('internal-docs'), label);
      await assert.doesNotReject(stat(target), label);
      if (fragment && target.endsWith('.md')) {
        const ids = headingIds(await readFile(target, 'utf8'));
        assert(ids.includes(decodeURIComponent(fragment)), `Missing chapter: ${label}`);
      }
    }
  }
});
