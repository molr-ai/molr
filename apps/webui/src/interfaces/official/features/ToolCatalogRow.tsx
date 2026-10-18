import { ChevronRight } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

export function ToolCatalogRow({ name, description, icon: Icon, status, onOpen }: { name: string; description: string; icon: LucideIcon; status?: string; onOpen: () => void }) {
  return <button type="button" className="tools-catalog-row interactive-list-row" aria-haspopup="dialog" onClick={onOpen}>
    <span className="tools-catalog-icon"><Icon size={18} aria-hidden="true" /></span>
    <span className="tools-catalog-copy"><strong>{name}{status && <small className="tools-catalog-state">{status}</small>}</strong><span>{description}</span></span>
    <ChevronRight className="tools-catalog-chevron" size={16} aria-hidden="true" />
  </button>;
}
