import { platforms, releaseChannel } from './release-lib.mjs';
export function fixture(version = '0.2.0', selected = platforms) {
  const base = `https://github.com/example/molr/releases/download/v${version}`;
  return { schema: 1, version, channel: releaseChannel(version), commit: 'a'.repeat(40), published_at: '2026-10-11T00:00:00Z', notes_url: `https://github.com/example/molr/releases/tag/v${version}`, platforms: selected.map(p => {
    const file = `molr_${version}_${p.os}_${p.arch}.tar.gz`;
    return { id: p.id, os: p.os, arch: p.arch, label: p.label, aliases: p.aliases, requirements: p.requirements, file, sha256: 'c'.repeat(64), url: `${base}/${file}`, checksum_url: `${base}/${file}.sha256` };
  }) };
}

export function desktopFixture(runtime = fixture()) {
  const platforms = ['linux-amd64', 'linux-arm64', 'darwin-amd64', 'darwin-arm64', 'win32-amd64'].map(id => {
    const [os, arch] = id.split('-'), variant = os === 'win32' ? 'client' : 'full';
    const file = `molr-desktop_${runtime.version}_${id}_${variant}.${{ linux: 'deb', darwin: 'dmg', win32: 'exe' }[os]}`;
    const url = `https://github.com/example/molr/releases/download/v${runtime.version}/${file}`;
    return { os, arch, variant, file, url, checksum_url: url + '.sha256', sha256: 'b'.repeat(64), signed: os !== 'linux' };
  });
  return { schema: 1, version: runtime.version, commit: runtime.commit, platforms };
}
