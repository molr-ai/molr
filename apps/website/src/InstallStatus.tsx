import type { ReactNode } from 'react';
import { Icon } from './HomeIcon';

export function InstallStatus({ children, hint, error = false }: { children: ReactNode; hint?: string; error?: boolean }) {
  return <div className="home-install-state">
    <Icon name={error ? 'server' : 'download'} />
    <p className="home-install-status" role={error ? 'alert' : 'status'}>{children}</p>
    {hint && <p>{hint}</p>}
  </div>;
}
