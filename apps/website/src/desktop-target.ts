import type { DesktopPlatform } from './desktop-releases';

type Hints = { platform?: string; architecture?: string; bitness?: string; mobile?: boolean };
export type BrowserDevice = { userAgent: string; platform: string; maxTouchPoints?: number; userAgentData?: Hints & { getHighEntropyValues?: (keys: string[]) => Promise<Hints> } };
export type DesktopTarget = { os?: DesktopPlatform['os']; arch?: DesktopPlatform['arch'] };

export function desktopTarget(browser: BrowserDevice, hints: Hints = {}): DesktopTarget {
  const ua = browser.userAgent;
  const platform = hints.platform || browser.userAgentData?.platform || browser.platform;
  if (hints.mobile || browser.userAgentData?.mobile || /Android|iPhone|iPad|iPod|Windows Phone|CrOS/i.test(ua) || (/Mac/i.test(platform) && (browser.maxTouchPoints || 0) > 1)) return {};
  const os = /Win/i.test(platform) || /Windows/i.test(ua) ? 'win32'
    : /Mac/i.test(platform) || /Macintosh/i.test(ua) ? 'darwin'
    : /Linux/i.test(platform) || /Linux/i.test(ua) ? 'linux' : undefined;
  if (!os) return {};
  if (hints.architecture) return { os, arch: hints.bitness === '64' ? ({ x86: 'amd64', arm: 'arm64' } as const)[hints.architecture as 'x86' | 'arm'] : undefined };
  // macOS user agents can report Intel on Apple Silicon; keep ambiguous CPUs unknown.
  const text = `${platform} ${ua}`;
  const arch = /aarch64|arm64/i.test(text) ? 'arm64' : os !== 'darwin' && /x86_64|amd64|x64|Win64|WOW64/i.test(text) ? 'amd64' : undefined;
  return { os, arch };
}

export async function browserDesktopTarget(browser: BrowserDevice): Promise<DesktopTarget> {
  try {
    const hints = await browser.userAgentData?.getHighEntropyValues?.(['architecture', 'bitness']);
    return desktopTarget(browser, hints);
  } catch { return desktopTarget(browser); }
}

export function preferredDesktopPlatform(platforms: DesktopPlatform[], target: DesktopTarget): DesktopPlatform | undefined {
  return platforms.find(p => p.os === target.os && p.arch === target.arch) || platforms[0];
}

export const desktopOSLabel = (os: DesktopPlatform['os']) => ({ linux: 'Linux', darwin: 'macOS', win32: 'Windows' })[os];
export const desktopArchLabel = (p: DesktopPlatform) => p.os === 'darwin' ? (p.arch === 'arm64' ? 'Apple Silicon' : 'Intel') : (p.arch === 'arm64' ? 'ARM64' : 'x64 / AMD64');
