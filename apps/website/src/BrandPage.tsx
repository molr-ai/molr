import type { ReactNode } from 'react';
import { Logo, useTheme } from '@molr/ui';

export function BrandPage({ titleId, logoAction, children }: { titleId: string; logoAction: string; children: ReactNode }) {
  const { theme } = useTheme();
  return (
    <div className="website-shell">
      <main className="website-main" aria-labelledby={titleId}>
        <div className="launch-grid" aria-hidden="true" />
        <span className="launch-mark launch-mark--start" aria-hidden="true" />
        <span className="launch-mark launch-mark--end" aria-hidden="true" />
        <div className="launch-content">
          <Logo assetBaseUrl={`${import.meta.env.BASE_URL}brand`} variant={theme === 'dark' ? 'light' : 'dark'} animation="daily" interactive interactionLabel={logoAction} />
          {children}
          <span className="launch-accent" aria-hidden="true" />
        </div>
      </main>
    </div>
  );
}
