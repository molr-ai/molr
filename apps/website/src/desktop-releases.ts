import type { Channel } from './release-data';
export type DesktopPlatform = { os: 'linux' | 'darwin' | 'win32'; arch: 'amd64' | 'arm64'; variant: 'full' | 'client'; signed: boolean; file: string; sha256: string; url: string };
export type DesktopCatalog = { schema: 1; channels: Record<Channel, { version: string; platforms: DesktopPlatform[] } | null> };
export function readDesktopCatalog(value: unknown): DesktopCatalog {
  const c = value as DesktopCatalog;
  if (!c || c.schema !== 1 || !c.channels) throw new Error('Invalid desktop catalog');
  for (const channel of ['stable', 'preview', 'nightly'] as const) {
    const release = c.channels[channel];
    if (release === null) continue;
    if (!release || !/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(release.version) || release.version.endsWith('-local') || !Array.isArray(release.platforms) || !release.platforms.length) throw new Error('Invalid desktop release');
    if (channel === 'stable' && release.version.includes('-')) throw new Error('Prerelease in stable channel');
    const seen = new Set<string>();
    for (const p of release.platforms) {
      if (!p || !['linux', 'darwin', 'win32'].includes(p.os) || !['amd64', 'arm64'].includes(p.arch) || p.variant !== (p.os === 'win32' ? 'client' : 'full') || typeof p.signed !== 'boolean' || !/^[a-f0-9]{64}$/.test(p.sha256)) throw new Error('Invalid desktop platform');
      const ext = ({ linux: 'deb', darwin: 'dmg', win32: 'exe' })[p.os];
      if (p.file !== `molr-desktop_${release.version}_${p.os}-${p.arch}_${p.variant}.${ext}`) throw new Error('Invalid desktop filename');
      const url = new URL(p.url);
      if (url.protocol !== 'https:' || url.username || url.password || url.hash || url.search || decodeURIComponent(url.pathname.split('/').pop()!) !== p.file) throw new Error('Invalid desktop download URL');
      const id = `${p.os}-${p.arch}`; if (seen.has(id)) throw new Error('Duplicate desktop platform'); seen.add(id);
    }
  }
  return { schema: 1, channels: Object.fromEntries((['stable', 'preview', 'nightly'] as const).map(channel => {
    const release = c.channels[channel];
    return [channel, release ? { version: release.version, platforms: release.platforms.map(p => ({ os: p.os, arch: p.arch, variant: p.variant, signed: p.signed, file: p.file, sha256: p.sha256, url: p.url })) } : null];
  })) as DesktopCatalog['channels'] };
}
