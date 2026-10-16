// Temporarily allow unsigned public builds while publisher certificates are pending.
// A partially configured signing identity remains an error, not an unsigned fallback.
export function desktopSigning(os, local, env = process.env) {
  if (local || os === 'linux') return false;
  const required = os === 'darwin'
    ? ['CSC_LINK', 'APPLE_ID', 'APPLE_APP_SPECIFIC_PASSWORD', 'APPLE_TEAM_ID']
    : ['CSC_LINK'];
  const configured = [...required, 'CSC_KEY_PASSWORD'].some(key => Boolean(env[key]));
  if (!configured) return false;
  if (required.some(key => !env[key])) throw new Error(`Incomplete ${os} desktop signing credentials`);
  return true;
}
