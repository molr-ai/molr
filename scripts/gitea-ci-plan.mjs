import { appendFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { nativeBuildMatrix } from './release-lib.mjs';

export function giteaCIPlan({ platforms = '', installers = '' } = {}) {
  const targets = nativeBuildMatrix().include.map(target => ({
    ...target,
    runner: `molr-${({ linux: 'linux', darwin: 'macos', win32: 'windows' })[target.os]}-${target.arch}`,
    shell: target.os === 'win32' ? 'pwsh' : 'bash',
  }));
  const ids = targets.map(target => target.runner.slice('molr-'.length));
  const requested = platforms.trim() || 'all';
  const selected = requested === 'all' ? ids : requested.split(',').map(id => id.trim());
  if (selected.some(id => !ids.includes(id)) || new Set(selected).size !== selected.length) {
    throw new Error(`Invalid Gitea CI platforms: ${requested}. Use all or a comma-separated list of: ${ids.join(', ')}`);
  }
  const installation = installers.trim() || 'enabled';
  if (!['enabled', 'disabled'].includes(installation)) throw new Error('Gitea CI installers must be enabled or disabled');
  const native = targets.filter(target => selected.includes(target.runner.slice('molr-'.length)));
  const browsers = native.filter(target => ['molr-linux-amd64', 'molr-macos-arm64'].includes(target.runner));
  const installerTargets = native.filter(target => target.os !== 'win32').map(({ os, arch }) => ({
    environment: `${os === 'linux' ? 'ubuntu-24.04' : 'macos'}-${arch}`,
    runner: `molr-install-${os === 'linux' ? 'linux' : 'macos'}-${arch}`,
    os, arch,
  }));
  const installs = installation === 'enabled' ? installerTargets : [];
  return {
    matrix: { include: native },
    // Keep matrices valid even on servers that expand them before evaluating job.if.
    // The corresponding enabled output prevents scheduling the placeholder job.
    browser_matrix: { include: browsers.length ? browsers : [{ runner: 'molr-linux-amd64' }] },
    installer_matrix: { include: installs.length ? installs : [{ runner: 'molr-install-linux-amd64' }] },
    browser_enabled: String(browsers.length > 0),
    installer_enabled: String(installs.length > 0),
    scope: `native=${native.map(t => t.runner).join(',')}; browser=${browsers.map(t => t.runner).join(',') || 'none'}; installer=${installs.map(t => t.runner).join(',') || 'none'}`,
  };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const plan = giteaCIPlan({
    platforms: process.env.MOLR_CI_PLATFORMS,
    installers: process.env.MOLR_CI_INSTALLERS,
  });
  const output = Object.entries(plan).map(([key, value]) => `${key}=${typeof value === 'string' ? value : JSON.stringify(value)}`).join('\n') + '\n';
  if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, output);
  console.log(plan.scope);
}
