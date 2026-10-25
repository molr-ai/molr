import { api, APIError, mutationKey } from '@molr/sdk';

/** One logical operation. Identity contains only target IDs or source revisions.
 * Never retain its body, credentials, or a body fingerprint.
 */
export function createMutationOperation() {
  let pending: { key: string; path: string; version: number | string; identity: string } | undefined;
  return async function mutate<T>(path: string, body: object, version: number | string = 0, identity = ''): Promise<T> {
    if (!pending || pending.path !== path || pending.version !== version || pending.identity !== identity) {
      pending = { key: mutationKey(), path, version, identity };
    }
    const attempt = pending;
    try {
      const result = await api.mutate<T>(path, body, { idempotencyKey: attempt.key, expectedVersion: version });
      if (pending === attempt) pending = undefined;
      return result;
    } catch (error) {
      // Timeouts and rate limiting can still be retried as the same operation.
      if (pending === attempt && error instanceof APIError && error.status >= 400 && error.status < 500 && error.status !== 408 && error.status !== 429) pending = undefined;
      throw error;
    }
  };
}
