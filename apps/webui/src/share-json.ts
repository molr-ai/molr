/** Reuse unchanged JSON subtrees without treating a domain Version as an ETag. */
export function shareJSON(previous: unknown, next: unknown): unknown {
  if (Object.is(previous, next)) return previous;
  if (Array.isArray(previous) && Array.isArray(next)) {
    const shared = next.map((value, index) => shareJSON(previous[index], value));
    return previous.length === shared.length && shared.every((value, index) => Object.is(value, previous[index])) ? previous : shared;
  }
  if (previous && next && typeof previous === 'object' && typeof next === 'object'
    && Object.getPrototypeOf(previous) === Object.prototype && Object.getPrototypeOf(next) === Object.prototype) {
    const before = previous as Record<string, unknown>, after = next as Record<string, unknown>;
    const keys = Object.keys(after), shared = { ...after };
    let same = Object.keys(before).length === keys.length;
    for (const key of keys) {
      shared[key] = shareJSON(before[key], after[key]);
      if (!Object.hasOwn(before, key) || !Object.is(shared[key], before[key])) same = false;
    }
    return same ? previous : shared;
  }
  return next;
}
