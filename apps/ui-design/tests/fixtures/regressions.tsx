import { StrictMode, useLayoutEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Button, ChatComposer, Checkbox, Dialog, Logo, ModalScope, Popover, ToastProvider, UIProvider, useToast } from '@molr/ui';
import type { UILocale } from '@molr/ui';
import '@molr/ui/styles.css';

declare global {
  interface Window {
    regression: { notify: (duration?: number) => void; rerender: () => void; renders: number; dismiss: () => void };
  }
}
window.regression = { notify: () => {}, rerender: () => {}, renders: 0, dismiss: () => {} };
function Consumer() {
  const { toast, dismiss, update } = useToast();
  window.regression.renders++;
  useLayoutEffect(() => {
    let id = 0;
    window.regression.notify = (duration = 0) => { id = toast({ title: 'Global notice', duration, action: { label: 'Acknowledge notice', onClick: () => { document.documentElement.dataset.ack = 'yes'; } } }); };
    window.regression.dismiss = () => dismiss(id);
    // Keep all three public methods in the consumer to exercise their identities.
    void update;
  }, [toast, dismiss, update]);
  return null;
}
function Controls() {
  const [open, setOpen] = useState(false);
  const [nested, setNested] = useState(false);
  const [popover, setPopover] = useState(false);
  const [revision, setRevision] = useState(0);
  const [seen, setSeen] = useState(-1);
  const [draft, setDraft] = useState('');
  const [checked, setChecked] = useState(false);
  const [mixed, setMixed] = useState(true);
  useLayoutEffect(() => { window.regression.rerender = () => setRevision(value => value + 1); }, []);
  return <>
    <Button onClick={() => window.regression.notify()}>Notify</Button>
    <Button onClick={() => setOpen(true)}>Open review modal</Button>
    <Dialog title="Review modal" open={open} onOpenChange={setOpen}>
      <Button onClick={() => window.regression.notify()}>Notify inside modal</Button>
      <Button onClick={() => setNested(true)}>Open review nested</Button>
      <Dialog title="Review nested" open={nested} onOpenChange={setNested}><Button onClick={() => window.regression.notify()}>Notify inside nested</Button></Dialog>
    </Dialog>
    <Popover title="Review popover" open={popover} onOpenChange={value => { setPopover(value); setSeen(revision); }} trigger={<Button>Review trigger</Button>}><input aria-label="Popover input" /></Popover>
    <output aria-label="Callback revision">{seen}</output>
    <div id="composer" style={{ width: 'min(100%, 360px)' }}><ChatComposer value={draft} onValueChange={setDraft} onSubmit={() => setDraft('')} /></div>
    <Checkbox label="Uncontrolled mixed" defaultIndeterminate />
    <Checkbox label="Controlled mixed" checked={checked} indeterminate={mixed} onIndeterminateChange={setMixed} onChange={event => setChecked(event.target.checked)} />
    <Checkbox label="Retained mixed" checked={false} indeterminate onChange={() => {}} />
  </>;
}
function ScopedControls() {
  const [open, setOpen] = useState(false);
  const { toast } = useToast();
  const notify = () => toast({ title: 'Scoped notice', duration: 0, action: { label: 'Scoped action', onClick: () => {} } });
  return <><Button onClick={notify}>Notify in preview</Button><Button onClick={() => setOpen(true)}>Open preview modal</Button><Dialog title="Preview modal" open={open} onOpenChange={setOpen}><Button onClick={notify}>Notify in preview modal</Button></Dialog></>;
}
function App() {
  const [locale, setLocale] = useState<UILocale>('en');
  const [container, setContainer] = useState<HTMLDivElement | null>(null);
  return <UIProvider locale={locale}><ToastProvider><Consumer /><main style={{ display: 'grid', gap: 20, padding: 16 }}>
    <Button onClick={() => setLocale(value => value === 'en' ? 'zh' : 'en')}>Toggle locale</Button>
    <div id="logo"><Logo animation="alive" markOnly interactive /></div>
    <div id="custom-logo"><Logo animation="alive" markOnly interactive interactionLabel="Custom identity" /></div>
    <Controls />
    <div id="preview" ref={setContainer} style={{ position: 'relative', width: 'min(360px, 100%)', height: 440, overflow: 'hidden', border: '1px solid gray' }}>
      <ModalScope container={container}><ScopedControls /></ModalScope>
    </div>
  </main></ToastProvider></UIProvider>;
}
createRoot(document.getElementById('root')!).render(<StrictMode><App /></StrictMode>);
