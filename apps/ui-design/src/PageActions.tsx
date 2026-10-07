import type { ReactNode } from 'react';
import { createPortal } from 'react-dom';

export function PageActions({ target, children }: { target: HTMLDivElement | null; children: ReactNode }) {
  return target ? createPortal(children, target) : null;
}
