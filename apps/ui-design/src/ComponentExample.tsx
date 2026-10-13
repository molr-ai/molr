import type { ReactNode } from 'react';

export function ComponentExample({ title, description, children, wide = false, id }: { title: string; description: string; children: ReactNode; wide?: boolean; id?: string }) {
  return <section id={id} className={`component-example${wide ? ' component-example--wide' : ''}`}><header><h3>{title}</h3><p>{description}</p></header><div className="component-example__body">{children}</div></section>;
}
