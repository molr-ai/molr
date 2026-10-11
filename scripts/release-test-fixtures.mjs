import { platforms, releaseChannel } from './release-lib.mjs';
export function fixture(version = '0.2.0', selected = platforms) {
  const base = `https://github.com/example/molr/releases/download/v${version}`;
  return { schema: 1, version, channel: releaseChannel(version), commit: 'a'.repeat(40), published_at: '2026-10-11T00:00:00Z', notes_url: `https://github.com/example/molr/releases/tag/v${version}`, installer_url: `${base}/install.sh`, installer_sha256: 'b'.repeat(64), platforms: selected.map(p => {
    const file = `molr_${version}_${p.os}_${p.arch}.tar.gz`;
    return { id: p.id, os: p.os, arch: p.arch, label: p.label, aliases: p.aliases, requirements: p.requirements, file, sha256: 'c'.repeat(64), url: `${base}/${file}`, checksum_url: `${base}/${file}.sha256` };
  }) };
}
