import type { ReactNode } from 'react';
import './session-start.css';

export function SessionStart({ icon, title, description, children, action, status }: { icon: ReactNode; title: string; description: string; children?: ReactNode; action: ReactNode; status?: string }) {
  return <div className="session-start"><div className="session-start-content">
    <span className="session-start-icon" aria-hidden="true">{icon}</span>
    <h3>{title}</h3><p className="session-start-description">{description}</p>
    {children}
    {status && <p className="session-start-status" role="status">{status}</p>}
    {action}
  </div></div>;
}
