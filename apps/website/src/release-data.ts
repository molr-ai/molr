export type Channel = 'stable' | 'preview' | 'nightly';
export type ReleasePlatform = {
  id: string; os: string; arch: string; label: string; aliases: string[];
  requirements: { systemd?: number; cgroup?: number; macos?: string; packages: string[] };
  file: string; sha256: string; url: string; checksum_url: string;
};
export type Release = {
  schema: number; version: string; channel: Channel; commit: string; published_at: string;
  notes_url: string; platforms: ReleasePlatform[];
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
    if (!release || release.schema !== 1 || !/^[0-9][0-9A-Za-z.-]*$/.test(release.version) || ![channel, ...(channel === 'preview' ? ['stable'] : [])].includes(release.channel) || !Number.isFinite(Date.parse(release.published_at)) || !https(release.notes_url) || !Array.isArray(release.platforms) || !release.platforms.length) throw new Error('Invalid release');
    for (const p of release.platforms) {
      if (!p || !/^[a-z0-9]+-[a-z0-9]+$/.test(p.id) || p.id !== `${p.os}-${p.arch}` || typeof p.label !== 'string' || p.file !== `molr_${release.version}_${p.os}_${p.arch}.tar.gz` || !/^[0-9a-f]{64}$/.test(p.sha256) || !https(p.url) || !https(p.checksum_url) || !p.requirements || !Array.isArray(p.requirements.packages) || p.requirements.packages.some(v => typeof v !== 'string') || !(p.os === 'linux' ? Number.isInteger(p.requirements.systemd) && p.requirements.cgroup === 2 : p.os === 'darwin' && typeof p.requirements.macos === 'string' && /^\d+\.\d+$/.test(p.requirements.macos))) throw new Error('Invalid platform');
    }
  }
  return catalog;
}
export type RuntimeMode = 'server' | 'sandbox';
export function publishedChannel(catalog: { channels: Record<Channel, unknown> } | null | undefined): Channel {
  return channels.find(channel => catalog?.channels[channel]) || 'stable';
}

export function installCommand(mode: RuntimeMode = 'server'): string {
  return `curl -fsSL https://molr.ai/install.sh | sh -s -- --mode ${mode}`;
}
