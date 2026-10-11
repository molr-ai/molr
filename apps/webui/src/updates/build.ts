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
 const environment = info.system ? [] : ['env', ...[['XDG_CONFIG_HOME', info.config_home], ['XDG_DATA_HOME', info.data_home], ['XDG_CACHE_HOME', info.cache_home]].map(([key,value]) => `${key}=${quote(value)}`)];
 return [...(info.system ? ['sudo'] : environment), 'bash', 'install.sh', '--source', quote(info.source), '--version', quote(version), '--name', quote(info.name), '--program-root', quote(info.program_root), ...(info.system ? ['--user',quote(info.user)] : [])].join(' ');
}
