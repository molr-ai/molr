import { spawnSync } from 'node:child_process';
import { readFileSync, readdirSync, existsSync } from 'node:fs';
import { resolve, relative, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import ts from 'typescript';

export const repository = fileURLToPath(new URL('../', import.meta.url));
const printer = ts.createPrinter({ removeComments: true });
const timerNames = new Set(['setInterval', 'setTimeout']);
function print(node, file) { return node ? printer.printNode(ts.EmitHint.Unspecified, node, file).replace(/\s+/g, ' ').trim() : ''; }
function owner(node, file) {
  for (let p = node.parent; p; p = p.parent) {
    if (ts.isFunctionDeclaration(p) || ts.isMethodDeclaration(p)) return p.name ? print(p.name, file) : '<anonymous>';
    if ((ts.isArrowFunction(p) || ts.isFunctionExpression(p)) && ts.isVariableDeclaration(p.parent)) return print(p.parent.name, file);
  }
  return '<module>';
}
export function scanTypeScript(path, source) {
  const file = ts.createSourceFile(path, source, ts.ScriptTarget.Latest, true);
  if (file.parseDiagnostics.length) throw new Error(`Cannot parse ${path}: ${ts.flattenDiagnosticMessageText(file.parseDiagnostics[0].messageText, '\n')}`);
  const aliases = new Map(), values = new Map(), namespaces = new Map(), stateSetters = new Set();
  const identify = node => {
    if (ts.isIdentifier(node)) return aliases.get(node.text) ?? (timerNames.has(node.text) ? node.text : '');
    if (ts.isPropertyAccessExpression(node) && timerNames.has(node.name.text)) return namespaces.get(node.expression.getText(file)) === 'node:timers/promises' ? `promise:${node.name.text}` : node.name.text;
    if (ts.isElementAccessExpression(node) && ts.isStringLiteral(node.argumentExpression) && timerNames.has(node.argumentExpression.text)) return node.argumentExpression.text;
    return '';
  };
  const visit = (node, fn) => { fn(node); ts.forEachChild(node, child => visit(child, fn)); };
  visit(file, node => {
    if (ts.isImportDeclaration(node) && node.importClause?.namedBindings && ts.isNamespaceImport(node.importClause.namedBindings)) namespaces.set(node.importClause.namedBindings.name.text, node.moduleSpecifier.text);
    if (ts.isImportDeclaration(node) && node.importClause?.namedBindings && ts.isNamedImports(node.importClause.namedBindings)) {
      const module = node.moduleSpecifier.text;
      for (const spec of node.importClause.namedBindings.elements) {
        const name = (spec.propertyName ?? spec.name).text;
        if (timerNames.has(name) || /(?:^|\/)polling(?:\.[cm]?[jt]s)?$/.test(module) && name === 'poll' || /(?:^|\/)state(?:\.tsx?)?$/.test(module) && ['useQuery', 'useQueryValue'].includes(name)) aliases.set(spec.name.text, module === 'node:timers/promises' ? `promise:${name}` : name);
      }
    }
  });
  visit(file, node => {
    if (!ts.isVariableDeclaration(node) || !node.initializer) return;
    if (ts.isArrayBindingPattern(node.name) && ts.isCallExpression(node.initializer) && /(?:^|\.)useState$/.test(node.initializer.expression.getText(file))) {
      for (const item of node.name.elements) if (ts.isBindingElement(item) && ts.isIdentifier(item.name)) stateSetters.add(`${owner(node, file)}:${item.name.text}`);
    }
    if (ts.isObjectBindingPattern(node.name) && /^(?:window|globalThis)$/.test(node.initializer.getText(file))) {
      for (const item of node.name.elements) if (ts.isIdentifier(item.name)) { const name = (item.propertyName ?? item.name).getText(file); if (timerNames.has(name)) aliases.set(item.name.text, name); }
    }
  });
  for (let pass = 0; pass < 3; pass++) visit(file, node => {
    if ((ts.isVariableDeclaration(node) || ts.isParameter(node)) && ts.isIdentifier(node.name) && node.initializer) {
      values.set(`${owner(node, file)}:${node.name.text}`, node.initializer);
      const name = identify(node.initializer); if (name) aliases.set(node.name.text, name);
    }
  });
  const sites = [];
  const add = (node, api, delay) => {
    let loops = 0;
    for (let p = node.parent; p; p = p.parent) if (ts.isForStatement(p) || ts.isForOfStatement(p) || ts.isForInStatement(p) || ts.isWhileStatement(p) || ts.isDoStatement(p)) loops++;
    sites.push({ file: path, scope: owner(node, file), api, delay, loops, line: file.getLineAndCharacterOfPosition(node.getStart(file)).line + 1 });
  };
  visit(file, node => {
    if (ts.isCallExpression(node)) {
      let api = identify(node.expression), delay;
      // Do not confuse React state setters with global timer APIs. Missing timer delays mean zero.
      if (ts.isIdentifier(node.expression) && stateSetters.has(`${owner(node, file)}:${node.expression.text}`)) return;
      if (api.startsWith('promise:')) delay = node.arguments[0] ?? ts.factory.createNumericLiteral(0);
      else if (timerNames.has(api)) delay = node.arguments[1] ?? ts.factory.createNumericLiteral(0);
      else if (api === 'poll') delay = node.arguments[0];
      else if (api === 'useQuery') delay = node.arguments[2];
      else if (api === 'useQueryValue') delay = node.arguments[3];
      else if (ts.isPropertyAccessExpression(node.expression) && /(?:^|\.)(?:queries|store|loop)$/.test(node.expression.expression.getText(file)) && ['subscribe', 'reschedule'].includes(node.expression.name.text)) {
        api = `query.${node.expression.name.text}`;
        delay = node.arguments[node.expression.name.text === 'subscribe' ? 2 : 0];
      }
      if (!delay) return;
      if (['useQuery', 'useQueryValue'].includes(api) && ['0', 'false', 'undefined'].includes(print(delay, file))) return;
      if (ts.isIdentifier(delay)) delay = values.get(`${owner(node, file)}:${delay.text}`) ?? values.get(`<module>:${delay.text}`) ?? delay;
      add(node, api, print(delay, file));
    } else if (ts.isVariableDeclaration(node) && node.initializer && identify(node.initializer)) {
      add(node, `reference:${identify(node.initializer)}`, print(node.initializer, file));
    }
    if (path.endsWith('/polling.ts') && ts.isFunctionDeclaration(node) && node.name?.text === 'retryDelay') add(node, 'polling.retry-policy', print(node, file));
    if (path.endsWith('/query-policy.ts') && ts.isFunctionDeclaration(node)) add(node, 'query.policy', print(node, file));
    if (path.endsWith('/query-store.ts') && ts.isMethodDeclaration(node) && node.name.getText(file) === 'subscribe') add(node, 'query.default', print(node.parameters[2]?.initializer, file));
  });
  return sites;
}
function sourceFiles(root, dir) {
  const base = join(root, dir); if (!existsSync(base)) return [];
  const out = [];
  for (const entry of readdirSync(base, { withFileTypes: true })) {
    const path = join(base, entry.name);
    if (entry.isDirectory()) { if (!['node_modules', 'dist', '.cache', 'tests', '__tests__'].includes(entry.name)) out.push(...sourceFiles(root, relative(root, path))); }
    else if (/\.(?:[cm]?[jt]sx?)$/.test(entry.name) && !/\.(?:test|spec|d)\.[cm]?[jt]sx?$/.test(entry.name)) out.push(path);
  }
  return out;
}
export function goEnvironment(root = repository) {
  const version = readFileSync(join(repository, 'runtime/.go-version'), 'utf8').trim();
  return { ...process.env, CGO_ENABLED: '0', GOTOOLCHAIN: `go${version}`, GOCACHE: process.env.GOCACHE || join(root, '.cache/go-build'), GOMODCACHE: process.env.GOMODCACHE || join(root, '.cache/go-mod') };
}
export function collectSites(root = repository) {
  const result = spawnSync(process.env.GO || 'go', ['run', join(repository, 'scripts/polling-go/main.go'), root], { cwd: repository, env: goEnvironment(), encoding: 'utf8', maxBuffer: 16 << 20 });
  if (result.error || result.status !== 0) throw new Error(result.error?.message ?? result.stderr);
  const sites = JSON.parse(result.stdout) ?? [];
  for (const group of ['apps', 'packages']) {
    if (!existsSync(join(root, group))) continue;
    for (const entry of readdirSync(join(root, group), { withFileTypes: true })) if (entry.isDirectory()) {
      for (const path of sourceFiles(root, `${group}/${entry.name}/src`)) sites.push(...scanTypeScript(relative(root, path).replaceAll('\\', '/'), readFileSync(path, 'utf8')));
    }
  }
  const releaseWatcher = 'scripts/watch-release.mjs';
  if (existsSync(join(root, releaseWatcher))) sites.push(...scanTypeScript(releaseWatcher, readFileSync(join(root, releaseWatcher), 'utf8')));
  return sites.sort((a, b) => a.file.localeCompare(b.file) || a.line - b.line);
}
export function signature(site) { return { scope: site.scope, api: site.api, delay: site.delay, loops: site.loops }; }
export function checkPolicy(sites, policy, root = repository) {
  const errors = [], actual = new Map();
  for (const site of sites) if (site.api.startsWith('wiring.') && site.delay.startsWith('missing:')) errors.push(`Production event capability required: ${site.file}:${site.line} ${site.api} ${site.delay}. Supply committed notifications and deadline handling; an inventory entry cannot waive this rule.`);
  for (const site of sites) { const list = actual.get(site.file) ?? []; list.push(signature(site)); actual.set(site.file, list); }
  const seen = new Set();
  if (policy.version !== 1 || !Array.isArray(policy.files)) return ['Invalid polling review policy'];
  for (const entry of policy.files) {
    if (seen.has(entry.file)) errors.push(`Duplicate polling policy: ${entry.file}`);
    seen.add(entry.file);
    if (!entry.reason?.trim() || !entry.budget?.trim() || !Array.isArray(entry.sites) || !entry.sites.length) errors.push(`Missing rationale, idle/rate budget, or sites: ${entry.file}`);
    if (!actual.has(entry.file)) errors.push(`Remove stale polling policy: ${entry.file}`);
    else if (JSON.stringify(entry.sites) !== JSON.stringify(actual.get(entry.file))) errors.push(`Changed timing policy: ${entry.file}\nObserved: ${JSON.stringify(actual.get(entry.file))}`);
    for (const proof of entry.tests ?? []) if (!existsSync(join(root, proof))) errors.push(`Missing behavior test: ${proof}`);
  }
  for (const [file, rows] of actual) if (!seen.has(file)) errors.push(`Unreviewed timer/polling entry: ${file}\nObserved: ${JSON.stringify(rows)}`);
  return errors;
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const sites = collectSites();
    if (process.argv.includes('--list')) console.log(JSON.stringify(sites, null, 2));
    else {
      const errors = checkPolicy(sites, JSON.parse(readFileSync(join(repository, 'scripts/polling-policy.json'), 'utf8')));
      if (errors.length) throw new Error(errors.join('\n') + '\nUse commit notifications + deadline timers for internal state. Review necessity, bounded cadence, cancellation, and idle tests before changing scripts/polling-policy.json; do not bulk-accept a new baseline. See docs/dev/contributing/workflow.md#后台等待与轮询门禁.');
      console.log(`Polling review gate passed: ${sites.length} timing sites. Run npm run test:polling for idle behavior contracts.`);
    }
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
