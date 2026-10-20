export type LocalVersionState = 'same' | 'update' | 'service-newer' | 'different' | 'unknown';

// Match the Runtime installer's SemVer precedence, including development builds
// and arbitrary-size numeric identifiers. Build metadata has no precedence.
function parse(value: string) {
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/.exec(value);
  if (!match) return;
  const pre = match[4]?.split('.');
  if (pre?.some(id => /^\d+$/.test(id) && id.length > 1 && id[0] === '0')) return;
  return { core: match.slice(1, 4), pre };
}
function compareIdentifier(a: string, b: string) {
  const an = /^\d+$/.test(a), bn = /^\d+$/.test(b);
  if (an && bn) return BigInt(a) > BigInt(b) ? 1 : BigInt(a) < BigInt(b) ? -1 : 0;
  if (an !== bn) return an ? -1 : 1;
  return a > b ? 1 : a < b ? -1 : 0;
}
export function localVersionState(installed?: string, bundled?: string): LocalVersionState {
  if (!installed || !bundled) return 'unknown';
  if (installed === bundled) return 'same';
  const a = parse(bundled), b = parse(installed);
  if (!a || !b) return 'different';
  let order = 0;
  for (let i = 0; i < 3 && !order; i++) order = compareIdentifier(a.core[i], b.core[i]);
  if (!order) {
    if (!a.pre || !b.pre) order = a.pre ? -1 : b.pre ? 1 : 0;
    else {
      for (let i = 0; i < Math.min(a.pre.length, b.pre.length) && !order; i++) order = compareIdentifier(a.pre[i], b.pre[i]);
      if (!order) order = Math.sign(a.pre.length - b.pre.length);
    }
  }
  return order > 0 ? 'update' : order < 0 ? 'service-newer' : 'different';
}
