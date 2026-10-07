import type { Copy } from './messages';

export function sandboxReason(t: Copy, reason: string | undefined): string {
  if (reason === 'available') return t.sandbox.ready;
  if (reason === 'sandbox_disabled') return t.sandbox.disabled;
  return (t.sandbox.reasons as Record<string, string>)[reason ?? ''] ?? t.sandbox.unavailable;
}
