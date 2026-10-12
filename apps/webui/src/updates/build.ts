import type { UpdateInstallation } from '@molr/sdk';
export type BuildConsistency = 'unknown' | 'version_mismatch' | 'commit_mismatch' | 'consistent';
export function compareBuilds(frontend: { version: string; commit: string }, runtime?: { version: string; commit: string }): BuildConsistency {
 if (!runtime?.version || !frontend.version) return 'unknown';
 if (frontend.version !== runtime.version) return 'version_mismatch';
 const valid = (commit: string) => /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(commit);
 if (!valid(frontend.commit) || !valid(runtime.commit)) return 'unknown';
 return frontend.commit === runtime.commit ? 'consistent' : 'commit_mismatch';
}
const quote = (value: string) => "'" + value.replaceAll("'", "'\\''") + "'";
export function upgradeCommand(info: UpdateInstallation, version: string) {
 return [...(info.system ? ['sudo'] : []), quote(info.setup_path), 'update', '--version', quote(version)].join(' ');
}
