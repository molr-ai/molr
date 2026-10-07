import type { ReactNode } from 'react';
import './page-header.css';

type PageHeaderProps = {
  title: string;
  icon: ReactNode;
  leading?: ReactNode;
  children?: ReactNode;
  actions?: ReactNode;
  className?: string;
};

export function PageHeader({ title, icon, leading, children, actions, className = '' }: PageHeaderProps) {
  return <header className={`page-header ${className}`}>
    <div className="page-header-identity">
      {leading}
      <div className="page-header-title"><span className="page-header-icon" aria-hidden="true">{icon}</span><h1>{title}</h1></div>
      {children}
    </div>
    {actions}
  </header>;
}
