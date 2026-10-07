import { BrandPage } from './BrandPage';
import { StatusMessage } from './StatusMessage';
import type { StatusMode } from './site-mode';

export function SiteStatusPage({ mode, heading, logoAction }: { mode: StatusMode; heading: string; logoAction: string }) {
  return (
    <BrandPage titleId="site-status-title" logoAction={logoAction}>
      <StatusMessage key={mode} mode={mode} accessibleLabel={heading} />
    </BrandPage>
  );
}
