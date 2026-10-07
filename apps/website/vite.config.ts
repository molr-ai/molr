import { documentPath, publicDocuments } from './docs-plugin';
import { releaseAssets } from '../../scripts/release-assets';
import { licenseAssets } from '../../scripts/license-assets';
import { installerPlugin } from './installer-plugin';
import breakpointQueries from '@molr/ui/postcss-breakpoints';
import { readFileSync } from 'node:fs';
import { devDocRoutes, docRoutes } from './src/docs-routes.ts';
import { fileURLToPath } from 'node:url';
import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import { messages } from './src/site-content.ts';
import { resolveSiteMode } from './src/site-mode.ts';

function escapeHtml(value: string) {
  return value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;').replaceAll("'", '&#39;');
}

export default defineConfig(({ mode }) => {
  const environment = loadEnv(mode, fileURLToPath(new URL('.', import.meta.url)), 'VITE_');
  const siteMode = resolveSiteMode(environment.VITE_SITE_MODE);
  const copy = { zh: messages.zh.pages[siteMode], en: messages.en.pages[siteMode] };
  const replacements: Record<string, string> = {
    MODE: siteMode,
    TITLE: escapeHtml(`${copy.zh.title} / ${copy.en.heading}`),
    DESCRIPTION: escapeHtml(`${copy.zh.description} ${copy.en.description}`),
    ZH_HEADING: escapeHtml(copy.zh.heading),
    EN_HEADING: escapeHtml(copy.en.heading),
    COPY: JSON.stringify(copy).replaceAll('<', '\\u003c'),
  };

  return {
    plugins: [publicDocuments(), releaseAssets(), licenseAssets(), installerPlugin(), react(), {
      name: 'website-state-html',
      transformIndexHtml: {
        order: 'pre',
        handler: html => html.replace(/__SITE_(MODE|TITLE|DESCRIPTION|ZH_HEADING|EN_HEADING|COPY)__/g, (_, key: string) => replacements[key]),
      },
    }, {
      name: 'website-docs-entries',
      enforce: 'post',
      generateBundle(_options, bundle) {
        if (siteMode !== 'live') return;
        const index = bundle['index.html'];
        if (!index || index.type !== 'asset') return;
        for (const doc of [...docRoutes, ...devDocRoutes]) {
          const content = readFileSync(documentPath(doc), 'utf8');
          const title = /^# (.+)$/m.exec(content)?.[1] ?? doc.label;
          const description = `${title} — Molr 使用与部署指南。`;
          const html = String(index.source)
            .replace(/<title>.*?<\/title>/, `<title>${escapeHtml(title)} · Molr Docs</title>`)
            .replace(/(<meta (?:name="description"|property="og:description") content=")[^"]*/g, `$1${escapeHtml(description)}`)
            .replace(/(<meta property="og:title" content=")[^"]*/, `$1${escapeHtml(title)} · Molr Docs`)
            .replaceAll('https://molr.ai/', `https://molr.ai${doc.path}`)
            .replace(/<main class="website-fallback">[\s\S]*?<\/main>/, `<main class="website-fallback" lang="zh-CN"><span>Molr Docs</span><h1>${escapeHtml(title)}</h1><p>请启用 JavaScript 阅读文档。</p></main>`);
          this.emitFile({ type: 'asset', fileName: `${doc.path.slice(1)}index.html`, source: html });
        }
      },
    }],
    css: { postcss: { plugins: [breakpointQueries()] } },
    publicDir: fileURLToPath(new URL('../../packages/ui/public', import.meta.url)),
    server: {
      host: '0.0.0.0',
      port: 5189,
      strictPort: true,
    },
    preview: {
      host: '0.0.0.0',
      port: 5190,
      strictPort: true,
    },
  };
});
