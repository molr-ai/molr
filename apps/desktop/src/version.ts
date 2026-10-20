// SemVer precedence; development identities never imply an available update.
export function compareVersions(a: string, b: string): number | undefined {
  const parse = (value: string) => {
    const m = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([\da-zA-Z-]+(?:\.[\da-zA-Z-]+)*))?(?:\+[\da-zA-Z-]+(?:\.[\da-zA-Z-]+)*)?$/.exec(value);
    if (!m || m[4]?.split('.').some(v => /^(local|dev|development)$/.test(v) || /^0\d+$/.test(v))) return;
    return m;
  };
  const x = parse(a), y = parse(b);
  if (!x || !y) return;
  const numeric = (v: string) => /^\d+$/.test(v);
  const cmp = (v: string, w: string) => v === w ? 0 : numeric(v) && numeric(w) ? (BigInt(v) > BigInt(w) ? 1 : -1) : v > w ? 1 : -1;
  for (let i = 1; i <= 3; i++) { const n = cmp(x[i], y[i]); if (n) return n; }
  if (x[4] === y[4]) return 0;
  if (!x[4] || !y[4]) return x[4] ? -1 : 1;
  const xp = x[4].split('.'), yp = y[4].split('.');
  for (let i = 0; i < Math.min(xp.length, yp.length); i++) {
    const v = xp[i], w = yp[i];
    const n = numeric(v) !== numeric(w) ? (numeric(v) ? -1 : 1) : cmp(v, w);
    if (n) return n;
  }
  return Math.sign(xp.length - yp.length);
}
