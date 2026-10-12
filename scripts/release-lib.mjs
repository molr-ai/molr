import { readFileSync } from 'node:fs';

export const platforms = JSON.parse(readFileSync(new URL('../release/platforms.json', import.meta.url))).platforms;
export const channels = ['stable', 'preview', 'nightly'];
const base = '(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)';
const pattern = new RegExp(`^${base}(?:-((?:beta|rc)\\.(?:0|[1-9][0-9]*)|0\\.nightly\\.[1-9][0-9]{7}\\.(?:0|[1-9][0-9]*)\\.g[0-9a-f]{7,40}))?$`);
export const releaseVersionPattern = pattern.source;
export function releaseChannel(version) {
  if (typeof version !== 'string' || !pattern.test(version)) throw new Error(`Unsupported release version: ${version}`);
  return version.includes('-0.nightly.') ? 'nightly' : version.includes('-') ? 'preview' : 'stable';
}
export function compareVersions(a, b) {
  releaseChannel(a); releaseChannel(b);
  const [ac, ap] = a.split('-'), [bc, bp] = b.split('-');
  const compare = (x, y) => /^\d+$/.test(x) && /^\d+$/.test(y) ? (BigInt(x) > BigInt(y) ? 1 : BigInt(x) < BigInt(y) ? -1 : 0)
    : /^\d+$/.test(x) ? -1 : /^\d+$/.test(y) ? 1 : x.localeCompare(y, 'en');
  for (let i = 0; i < 3; i++) { const n = compare(ac.split('.')[i], bc.split('.')[i]); if (n) return n; }
  if (ap === bp) return 0;
  if (!ap || !bp) return ap ? -1 : 1;
  const x = ap.split('.'), y = bp.split('.');
  for (let i = 0; i < Math.min(x.length, y.length); i++) { const n = compare(x[i], y[i]); if (n) return n; }
  return Math.sign(x.length - y.length);
}
export function nightlyVersion(baseVersion, commit, date = new Date()) {
  if (releaseChannel(baseVersion) !== 'stable' || !/^[0-9a-f]{40}$/.test(commit)) throw new Error('Invalid nightly base or commit');
  const stamp = date.toISOString().replace(/[-:TZ.]/g, '');
  return `${baseVersion}-0.nightly.${stamp.slice(0, 8)}.${Number(stamp.slice(8, 14))}.g${commit.slice(0, 12)}`;
}
export function httpsURL(value) {
  if (typeof value !== 'string' || /[\s'"`]/.test(value)) throw new Error('Invalid public URL');
  const url = new URL(value);
  if (url.protocol !== 'https:' || url.username || url.password || url.hash) throw new Error('HTTPS public URL required');
  return url.href;
}
function exactKeys(value, keys) {
  if (!value || typeof value !== 'object' || Array.isArray(value) || Object.keys(value).sort().join(',') !== keys.sort().join(',')) throw new Error('Unexpected manifest fields');
}
export function validateManifest(m) {
  exactKeys(m, ['schema', 'version', 'channel', 'commit', 'published_at', 'notes_url', 'installer_url', 'installer_sha256', 'platforms']);
  const channel = releaseChannel(m.version);
  if (m.schema !== 1 || m.channel !== channel || !/^[0-9a-f]{40}$/.test(m.commit) || !Number.isFinite(Date.parse(m.published_at)) || !Array.isArray(m.platforms) || !m.platforms.length) throw new Error('Invalid release manifest');
  httpsURL(m.notes_url); httpsURL(m.installer_url);
  if (!/^[0-9a-f]{64}$/.test(m.installer_sha256)) throw new Error('Invalid installer checksum');
  const seen = new Set();
  for (const p of m.platforms) {
    exactKeys(p, ['id', 'os', 'arch', 'label', 'aliases', 'requirements', 'file', 'sha256', 'url', 'checksum_url']);
    exactKeys(p.requirements, ['systemd', 'cgroup', 'packages']);
    const r = p.requirements;
    if (!Number.isInteger(r.systemd) || r.systemd < 254 || r.cgroup !== 2 || !Array.isArray(r.packages) || r.packages.some(n => typeof n !== 'string' || !/^[a-z0-9][a-z0-9+.-]*$/.test(n))) throw new Error('Invalid platform requirements');
    if (!/^[a-z0-9]+-[a-z0-9]+$/.test(p.id) || p.id !== `${p.os}-${p.arch}` || seen.has(p.id) || !Array.isArray(p.aliases) || !p.aliases.length || p.aliases.some(a => !/^[a-z0-9_]+$/.test(a))) throw new Error('Invalid release platform');
    seen.add(p.id);
    if (p.file !== `molr_${m.version}_${p.os}_${p.arch}.tar.gz` || !/^[0-9a-f]{64}$/.test(p.sha256) || typeof p.label !== 'string' || !p.requirements) throw new Error('Invalid release asset');
    httpsURL(p.url); httpsURL(p.checksum_url);
  }
  return m;
}
export function selectChannels(releases) {
  const selected = Object.fromEntries(channels.map(c => [c, null]));
  for (const m of releases) {
    validateManifest(m);
    for (const c of m.channel === 'stable' ? ['stable', 'preview'] : [m.channel]) {
      if (!selected[c] || compareVersions(m.version, selected[c].version) > 0) selected[c] = m;
    }
  }
  return selected;
}
