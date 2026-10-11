import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, readdir, rm, symlink, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { build, createServer } from 'vite';
import { documentPath, publicDocuments } from '../docs-plugin.ts';
import { releaseAssets } from '../../../scripts/release-assets.ts';

test('document routes reject internal paths, traversal and links', async () => {
  const root = await mkdtemp(join(tmpdir(), 'molr-doc-routes-'));
  try {
    await mkdir(join(root, 'docs'));
    await mkdir(join(root, 'internal-docs'));
    await writeFile(join(root, 'docs/public.md'), '# Public');
    await writeFile(join(root, 'internal-docs/private.md'), 'internal fixture');
    const route = { root: 'docs', file: 'public.md' };
    assert.equal(documentPath(route, root), join(root, 'docs/public.md'));
    for (const entry of [
      { ...route, root: 'internal-docs' },
      { ...route, file: '../internal-docs/private.md' },
      { ...route, file: '/internal-docs/private.md' },
      { ...route, file: 'internal-docs/private.md' },
    ]) assert.throws(() => documentPath(entry, root), /public|forbidden/);
    await symlink(join(root, 'internal-docs/private.md'), join(root, 'docs/link.md'));
    assert.throws(() => documentPath({ ...route, file: 'link.md' }, root), /forbidden/);
  } finally { await rm(root, { recursive: true, force: true }); }
});

test('public applications reject raw imports from internal documents', async () => {
  const root = await mkdtemp(join(tmpdir(), 'molr-doc-import-'));
  try {
    await mkdir(join(root, 'internal-docs'));
    await writeFile(join(root, 'internal-docs/notes.md'), 'private fixture text');
    await writeFile(join(root, 'index.html'), '<script type="module" src="/main.js"></script>');
    await writeFile(join(root, 'main.js'), 'import text from "./internal-docs/notes.md?raw"; document.body.textContent=text;');
    await assert.rejects(build({ configFile: false, root, publicDir: false, plugins: [releaseAssets()], logLevel: 'silent' }), /Internal documents cannot be imported/);
  } finally { await rm(root, { recursive: true, force: true }); }
});

test('development server refuses direct internal files and retains credential deny rules', async () => {
  const root = await mkdtemp(join(tmpdir(), 'molr-doc-server-'));
  let server;
  try {
    await mkdir(join(root, 'internal-docs'));
    await writeFile(join(root, 'internal-docs/notes.md'), 'private fixture text');
    await writeFile(join(root, '.env'), 'private fixture environment');
    server = await createServer({ configFile: false, root, publicDir: false, plugins: [releaseAssets()], logLevel: 'silent', server: { host: '127.0.0.1', port: 0 } });
    await server.listen();
    const { port } = server.httpServer.address();
    for (const path of ['/internal-docs/notes.md', `/@fs${root}/internal-docs/notes.md`, '/.env']) {
      const response = await fetch(`http://127.0.0.1:${port}${path}`);
      assert.equal(response.status, 403);
      assert(!(await response.text()).includes('private fixture'));
    }
  } finally { await server?.close(); await rm(root, { recursive: true, force: true }); }
});

test('unlisted Markdown is absent from the generated document module', async () => {
  const root = await mkdtemp(join(tmpdir(), 'molr-doc-bundle-'));
  const documents = fileURLToPath(new URL('../../../docs/', import.meta.url));
  // This path is inside the old docs/**/*.md glob, but is not a reviewed route.
  const scratch = await mkdtemp(join(documents, 'privacy-fixture-'));
  const sentinel = 'UNLISTED_DOCUMENT_MUST_NEVER_REACH_THE_BROWSER';
  try {
    await writeFile(join(scratch, 'notes.md'), sentinel);
    await writeFile(join(root, 'index.html'), '<script type="module" src="/main.js"></script>');
    await writeFile(join(root, 'main.js'), 'import documents from "virtual:molr-documents"; globalThis.reviewedDocuments=documents;');
    await build({ configFile: false, root, publicDir: false, plugins: [publicDocuments(), releaseAssets()], logLevel: 'silent' });
    const assets = join(root, 'dist/assets');
    const text = (await Promise.all((await readdir(assets)).map(name => readFile(join(assets, name), 'utf8')))).join('');
    assert(text.includes('文档维护约定'));
    assert(!text.includes('从单账号升级到多用户（一次性）'));
    assert(!text.includes(sentinel));
  } finally {
    await rm(scratch, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});
