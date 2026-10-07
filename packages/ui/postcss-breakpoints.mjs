import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const tokenFile = fileURLToPath(new URL('./breakpoints.json', import.meta.url));

// CSS variables cannot be used in media-query conditions. Expand named queries
// at build time using the same source as useBreakpoint().
export default function breakpointQueries() {
  return {
    postcssPlugin: 'molr-breakpoints',
    Once(root, { result }) {
      // Register the file with Vite's CSS watcher and avoid Node's JSON module
      // cache so editing a token updates both CSS and JS during development.
      const breakpoints = JSON.parse(readFileSync(tokenFile, 'utf8'));
      result.messages.push({ type: 'dependency', plugin: 'molr-breakpoints', file: tokenFile, parent: result.opts.from });
      root.walkAtRules('media', rule => {
        rule.params = rule.params.replace(/\(--molr-([\w-]+)\)/g, (_, name) => {
          if (!Object.hasOwn(breakpoints, name)) throw rule.error(`Unknown Molr breakpoint: ${name}`);
          return `(max-width: ${breakpoints[name]}px)`;
        });
      });
    },
  };
}
breakpointQueries.postcss = true;
