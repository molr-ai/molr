import { useEffect, useRef, useState } from 'react';
import { Button, Drawer, ModalPresence, ModalScope, TextField, UIProvider, useModalSnapshot } from '@molr/ui';

type Mode = 'right' | 'bottom' | 'contained';
export function ModalPresenceFixture() {
  const [mode, setMode] = useState<Mode>('right'), [open, setOpen] = useState(false);
  const [container, setContainer] = useState<HTMLDivElement | null>(null);
  const [reduced, setReduced] = useState(false), [live, setLive] = useState(0), [actions, setActions] = useState(0);
  const [mounted, setMounted] = useState(true);
  const [loadingOpen, setLoadingOpen] = useState('');
  const [selection, setSelection] = useState<string | null>(null), [closed, setClosed] = useState(0);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);
  return <section aria-label="Modal presence contract">
    <Button onClick={() => setReduced(value => !value)}>Toggle presence reduced motion</Button>
    <Button onClick={() => setLoadingOpen('loading')}>Open loading modal</Button>
    <ModalPresence>{loadingOpen && <LoadingModal onClose={() => setLoadingOpen('')} />}</ModalPresence>
    <Button onClick={() => setSelection('Selected record')}>Open controlled selection</Button><output aria-label="Completed modal exits">{closed}</output>
    <Drawer className="controlled-selection" open={!!selection} title={selection ?? 'No selection'} onOpenChange={value => { if (!value) setSelection(null); }} onAfterClose={() => setClosed(value => value + 1)} footer={selection && <Button onClick={() => setSelection(null)}>Clear controlled selection</Button>}>{selection && <p>Selected record contents</p>}</Drawer>
    <output aria-label="Live modal effects">{live}</output><output aria-label="Presence actions">{actions}</output>
    <UIProvider reducedMotion={reduced}><div ref={setContainer} style={{ position: 'relative', height: 600, overflow: 'hidden' }}>
      <ModalScope container={container} enabled={mode === 'contained'}>
        {(['right', 'bottom', 'contained'] as const).map(value => <Button key={value} onClick={() => { setMode(value); setMounted(true); setOpen(true); }}>Open retained {value}</Button>)}
        {mounted && <ModalPresence>{open && <RetainedDrawer mode={mode} onClose={() => setOpen(false)} onLive={delta => setLive(value => value + delta)} onAction={() => setActions(value => value + 1)}
          onReopen={() => { setOpen(false); timer.current = setTimeout(() => setOpen(true), 60); }} onUnmount={() => { setOpen(false); setMounted(false); }} />}</ModalPresence>}
      </ModalScope>
    </div></UIProvider>
  </section>;
}
function RetainedDrawer({ mode, onClose, onLive, onAction, onReopen, onUnmount }: { mode: Mode; onClose: () => void; onLive: (delta: number) => void; onAction: () => void; onReopen: () => void; onUnmount: () => void }) {
  const [draft, setDraft] = useState(''), [nested, setNested] = useState(false);
  // The fixture deliberately keeps state/effects inside the conditionally mounted subtree.
  const live = useRef(onLive);
  useEffect(() => { live.current(1); return () => live.current(-1); }, []);
  return <Drawer open side={mode === 'right' ? 'right' : 'bottom'} title="Retained details" description="Original description" onOpenChange={value => { if (!value) onClose(); }} footer={<Button onClick={onClose}>Cancel retained form</Button>}>
    <TextField label="Retained draft" value={draft} onChange={event => setDraft(event.target.value)} />
    <Button onClick={onAction}>Retained action</Button><Button onClick={onReopen}>Reverse retained exit</Button><Button onClick={onUnmount}>Unmount retained owner</Button>
    <Button onClick={() => setNested(true)}>Open retained child</Button>
    <ModalPresence>{nested && <Drawer open side="bottom" title="Retained child" onOpenChange={value => { if (!value) setNested(false); }}>
      <Button onClick={onClose}>Close retained family</Button><p>Child contents</p>
    </Drawer>}</ModalPresence>
  </Drawer>;
}

function LoadingModal({ onClose }: { onClose: () => void }) {
  const [loaded, setLoaded] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);
  const displayed = useModalSnapshot(loaded);
  if (displayed) return <LoadedModal onClose={onClose} />;
  return <Drawer open title="Loading modal" onOpenChange={value => { if (!value) onClose(); }}>
    <Button onClick={() => { onClose(); timer.current = setTimeout(() => setLoaded(true), 30); }}>Resolve during exit</Button>
  </Drawer>;
}
function LoadedModal({ onClose }: { onClose: () => void }) {
  return <Drawer open title="Loaded modal" onOpenChange={value => { if (!value) onClose(); }}>Loaded form</Drawer>;
}
