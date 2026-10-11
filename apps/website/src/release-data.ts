export type Channel = 'stable' | 'preview' | 'nightly';
export type ReleasePlatform = {
  id: string; os: string; arch: string; label: string; aliases: string[];
  requirements: { systemd: number; python: string; cgroup: number; packages: string[] };
  file: string; sha256: string; url: string; checksum_url: string;
};
export type Release = {
  schema: number; version: string; channel: Channel; commit: string; published_at: string;
  notes_url: string; installer_url: string; installer_sha256: string; platforms: ReleasePlatform[];
};
export type Catalog = { schema: number; channels: Record<Channel, Release | null> };
export const channels: Channel[] = ['stable', 'preview', 'nightly'];
export function readCatalog(value: unknown): Catalog {
  const catalog = value as Catalog;
  const https = (url: string) => { const u = new URL(url); return u.protocol === 'https:' && !u.username && !u.password && !u.hash && !/[\s'"`]/.test(url); };
  if (!catalog || catalog.schema !== 1 || !catalog.channels) throw new Error('Invalid release catalog');
  for (const channel of channels) {
    const release = catalog.channels[channel];
    if (release === null) continue;
    if (!release || release.schema !== 1 || !/^[0-9][0-9A-Za-z.-]*$/.test(release.version) || ![channel, ...(channel === 'preview' ? ['stable'] : [])].includes(release.channel) || !Number.isFinite(Date.parse(release.published_at)) || !https(release.notes_url) || !https(release.installer_url) || !Array.isArray(release.platforms) || !release.platforms.length) throw new Error('Invalid release');
    for (const p of release.platforms) {
      if (!p || !/^[a-z0-9]+-[a-z0-9]+$/.test(p.id) || p.id !== `${p.os}-${p.arch}` || typeof p.label !== 'string' || !/^[0-9a-f]{64}$/.test(p.sha256) || !https(p.url) || !https(p.checksum_url) || !p.requirements || !Array.isArray(p.requirements.packages) || p.requirements.packages.some(v => typeof v !== 'string') || !Number.isInteger(p.requirements.systemd) || typeof p.requirements.python !== 'string') throw new Error('Invalid platform');
    }
  }
  return catalog;
}
export function installCommand(release: Release, channel: Channel, source: string): string {
  const url = new URL(source);
  if (!['https:', 'http:'].includes(url.protocol) || /[\s'"`]/.test(source)) throw new Error('Invalid release source');
  return `curl -fsSL '${release.installer_url}' | bash -s -- --source '${source}' --channel ${channel} --version ${release.version}`;
}
