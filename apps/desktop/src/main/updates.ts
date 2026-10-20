import type { AppUpdateStatus } from '@molr/ui-host/app';
import { compareVersions } from '../version.js';

const source = 'https://molr.ai/releases/desktop/catalog.json';
export class DesktopUpdates {
  private state: AppUpdateStatus = { comparison: 'unknown', checking: false };
  private pending?: Promise<AppUpdateStatus>;
  private lastAttempt?: number;
  constructor(private version: string, private os: string, private arch: string, private changed: () => void,
    private request: typeof fetch = fetch, private now: () => number = Date.now) {
    if (compareVersions(version, version) === undefined) this.state.comparison = 'uncomparable';
  }
  snapshot(): AppUpdateStatus { return { ...this.state }; }
  check(automatic = false): Promise<AppUpdateStatus> {
    if (this.pending) return this.pending;
    const now = this.now();
    if (this.state.comparison === 'uncomparable' || (this.lastAttempt !== undefined && now - this.lastAttempt < (automatic ? 86400000 : 60000))) return Promise.resolve(this.snapshot());
    this.lastAttempt = now;
    this.state = { ...this.state, checking: true, error: undefined, nextAllowedAt: new Date(now + 60000).toISOString() };
    this.changed();
    this.pending = this.fetch().finally(() => { this.pending = undefined; });
    return this.pending;
  }
  private async fetch(): Promise<AppUpdateStatus> {
    try {
      const response = await this.request(source, { signal: AbortSignal.timeout(10000), redirect: 'error', cache: 'no-store' });
      if (!response.ok) throw new Error('network');
      // Bound streamed input as well as the advertised size.
      const reader = response.body?.getReader();
      if (!reader) throw new Error('manifest');
      let size = 0, text = ''; const decoder = new TextDecoder();
      try {
        for (;;) {
          const { done, value } = await reader.read(); if (done) break;
          size += value.length; if (size > 262144) throw new Error('manifest');
          text += decoder.decode(value, { stream: true });
        }
        text += decoder.decode();
      } finally { await reader.cancel(); }
      const catalog = JSON.parse(text);
      if (catalog.schema !== 1 || !catalog.channels || !Object.hasOwn(catalog.channels, 'stable')) throw new Error('manifest');
      const release = catalog.channels.stable;
      let comparison: AppUpdateStatus['comparison'] = 'unavailable', latestVersion: string | undefined;
      if (release !== null) {
        if (typeof release.version !== 'string' || !/^\d+\.\d+\.\d+$/.test(release.version) || !Array.isArray(release.platforms)) throw new Error('manifest');
        const n = compareVersions(this.version, release.version);
        if (n === undefined) throw new Error('manifest');
        const platform = release.platforms.find((p: {os: string; arch: string}) => p?.os === this.os && p.arch === this.arch);
        if (platform) {
          const extension = ({ linux: 'deb', darwin: 'dmg', win32: 'exe' } as Record<string, string>)[this.os];
          const file = `molr-desktop_${release.version}_${this.os}-${this.arch}_full.${extension}`;
          const url = new URL(platform.url);
          if (platform.variant !== 'full' || platform.file !== file || !/^[a-f0-9]{64}$/.test(platform.sha256) || typeof platform.signed !== 'boolean' || typeof platform.notarized !== 'boolean' || (platform.notarized && (this.os !== 'darwin' || !platform.signed)) || url.protocol !== 'https:' || url.username || url.password || url.search || url.hash || decodeURIComponent(url.pathname.split('/').pop()!) !== file) throw new Error('manifest');
          latestVersion = release.version;
          comparison = n < 0 ? 'available' : n > 0 ? 'ahead' : 'current';
        }
      }
      this.state = { ...this.state, comparison, latestVersion, lastSuccessAt: new Date(this.now()).toISOString(), error: undefined };
    } catch {
      this.state.error = 'check_failed';
    } finally { this.state.checking = false; this.changed(); }
    return this.snapshot();
  }
}
