import type { ReactNode } from 'react';
import { ArrowRight, ArrowUpCircle, CheckCircle2, Info } from 'lucide-react';

export function ReleaseSummary({ title, tone, children }: { title: string; tone?: 'current' | 'available'; children?: ReactNode }) {
 const Icon = tone === 'current' ? CheckCircle2 : tone === 'available' ? ArrowUpCircle : Info;
 return <section className="release-summary">
  <div className="release-heading" data-tone={tone}><Icon size={20} aria-hidden="true" /><h3 role="status">{title}</h3></div>
  {children && <div className="release-content">{children}</div>}
 </section>;
}
export function VersionChange({ current, target }: { current?: string; target?: string }) {
 return <p className="release-version"><span>{current ? `v${current}` : '—'}</span><ArrowRight size={14} aria-hidden="true" /><strong>{target ? `v${target}` : '—'}</strong></p>;
}
