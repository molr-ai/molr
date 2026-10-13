import { Children, createContext, useContext, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';

type Presence = { present: boolean; register: () => () => void };
export const ModalPresenceContext = createContext<Presence | null>(null);

/** Retain a conditionally rendered modal subtree until its dialogs finish closing. */
export function ModalPresence({ children }: { children: ReactNode }) {
  const parent = useContext(ModalPresenceContext);
  const present = Children.toArray(children).some(child => child !== '') && parent?.present !== false;
  const [retained, setRetained] = useState<ReactNode>(present ? children : null);
  if (present && retained !== children) setRetained(children);
  const current = useRef(present);
  const registrations = useRef(new Set<symbol>());
  const lifecycle = useMemo(() => {
    const check = () => queueMicrotask(() => {
      // Wait for all layout effects: an async form may replace its loading dialog.
      if (!current.current && registrations.current.size === 0) setRetained(null);
    });
    return {
      check,
      register() {
        const key = Symbol();
        registrations.current.add(key);
        const releaseParent = parent?.register();
        return () => { registrations.current.delete(key); releaseParent?.(); check(); };
      },
    };
  }, [parent?.register]);
  useLayoutEffect(() => { current.current = present; if (!present) lifecycle.check(); }, [present, lifecycle]);
  const context = useMemo(() => ({ present, register: lifecycle.register }), [present, lifecycle]);
  return <ModalPresenceContext.Provider value={context}>{present ? children : retained}</ModalPresenceContext.Provider>;
}

/** Async data that chooses the modal's root must not swap that root during exit. */
export function useModalSnapshot<T>(value: T): T {
  const presence = useContext(ModalPresenceContext);
  const retained = useRef(value);
  useLayoutEffect(() => { if (presence?.present !== false) retained.current = value; });
  return presence?.present === false ? retained.current : value;
}
