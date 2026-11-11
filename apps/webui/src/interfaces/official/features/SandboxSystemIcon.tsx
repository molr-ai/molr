import { AppleLogoIcon } from '@phosphor-icons/react/dist/csr/AppleLogo';
import { WindowsLogoIcon } from '@phosphor-icons/react/dist/csr/WindowsLogo';
import { LinuxLogoIcon } from '@phosphor-icons/react/dist/csr/LinuxLogo';
import { Server } from 'lucide-react';

// Match the system logos used on the website's download page.
export function SandboxSystemIcon({ os }: { os?: string }) {
  const Icon = os === 'darwin' ? AppleLogoIcon : os === 'windows' ? WindowsLogoIcon : os === 'linux' ? LinuxLogoIcon : undefined;
  return Icon ? <Icon size={17} weight="regular" data-system-icon={os} aria-hidden="true" />
    : <Server size={17} data-system-icon="fallback" aria-hidden="true" />;
}
