import { releaseChannel } from '../../../scripts/release-lib.mjs';

export function desktopSigning(os, version, env = process.env) {
  const channel = version.endsWith('-local') ? 'local' : releaseChannel(version);
  const unsigned = channel === 'local' || os === 'linux' || (os === 'darwin' && channel === 'nightly');
  if (unsigned) {
    // Disable certificate inputs even when a local shell has signing credentials.
    for (const key of ['CSC_LINK', 'CSC_KEY_PASSWORD', 'WIN_CSC_LINK', 'WIN_CSC_KEY_PASSWORD']) delete env[key];
    return false;
  }
  // Actions exposes missing secrets as empty strings. electron-builder treats an
  // empty CSC_LINK as a certificate path, so omit it before creating the packager.
  for (const key of ['CSC_LINK', 'CSC_KEY_PASSWORD']) if (env[key] === '') delete env[key];
  if (os === 'win32' && !env.CSC_LINK && !env.CSC_KEY_PASSWORD) return false;
  const required = os === 'darwin'
    ? ['CSC_LINK', 'APPLE_ID', 'APPLE_APP_SPECIFIC_PASSWORD', 'APPLE_TEAM_ID']
    : ['CSC_LINK'];
  if (required.some(key => !env[key])) throw new Error(`Incomplete ${os} desktop signing credentials`);
  return true;
}
