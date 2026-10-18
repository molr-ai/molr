import { AppleLogoIcon } from '@phosphor-icons/react/dist/csr/AppleLogo';
import { WindowsLogoIcon } from '@phosphor-icons/react/dist/csr/WindowsLogo';
import { LinuxLogoIcon } from '@phosphor-icons/react/dist/csr/LinuxLogo';
import type { DesktopPlatform } from './desktop-releases';

const systemIcons = { darwin: AppleLogoIcon, win32: WindowsLogoIcon, linux: LinuxLogoIcon };

export function SystemIcon({ os }: { os: DesktopPlatform['os'] }) {
  const Icon = systemIcons[os];
  return <Icon className="home-icon" size={20} weight="regular" aria-hidden="true" />;
}
