import type { SandboxInfo } from '@molr/sdk';

export function usesNetworkToggle(backend?: string): boolean {
  return backend === 'seatbelt-toggle' || backend === 'appcontainer-toggle';
}

/** Controlled compute is independent of separately granted full access. */
export function sandboxComputeState(info: SandboxInfo, state = 'available') {
  if (state === 'disconnected') return 'disconnected';
  if (state === 'disabled') return 'disabled';
  if (state !== 'available') return 'offline';
  if (!info.Enabled || info.Capabilities.Reason === 'sandbox_disabled') return 'disabled';
  return info.Capabilities.Shell ? 'ready' : 'unavailable';
}
