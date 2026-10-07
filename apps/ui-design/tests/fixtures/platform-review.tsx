import { StrictMode, useLayoutEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Button, Dialog, DropdownMenu, Logo, ModalScope, Select, ToastProvider, UIProvider, composeRefs, getOverlayViewport, useFloating, useModalScope, useTheme } from '@molr/ui';
import { getTabStops } from '../../../../packages/ui/src/focus';
import logoURL from '@molr/ui/brand/molr-mark.svg?url';
import '@molr/ui/styles.css';

function ThemeConsumer({ name }: { name: string }) {
  const { theme, preference, changePreference } = useTheme();
  return <div><output aria-label={`Theme ${name}`}>{preference}:{theme}</output><Button onClick={() => changePreference('light')}>Light {name}</Button><Button onClick={() => changePreference('dark')}>Dark {name}</Button><Button onClick={() => changePreference('system')}>System {name}</Button></div>;
}
function CustomContained() {
  const scope = useModalScope();
  const [open, setOpen] = useState(false);
  const { anchorRef, panelRef, style } = useFloating(open, setOpen, { width: 180 });
  return <><button ref={composeRefs(anchorRef)} onClick={() => setOpen(value => !value)}>Custom overlay</button>
    <div ref={panelRef} popover="manual" style={style} data-testid="custom-panel">{scope?.container ? 'Contained' : 'Unbounded'}</div>
    <Button onClick={() => { document.documentElement.dataset.customWidth = String(getOverlayViewport(scope?.container).width); }}>Measure scope</Button></>;
}
function App() {
  const [mounted, setMounted] = useState(true);
  const [open, setOpen] = useState(false);
  const [choice, setChoice] = useState('');
  const [scope, setScope] = useState<HTMLDivElement | null>(null);
  useLayoutEffect(() => {
    (window as unknown as { tabStops: () => string[] }).tabStops = () => getTabStops(document.querySelector('#media-dialog dialog')!).map(element => element.id).filter(Boolean);
  }, []);
  return <UIProvider locale="en"><ToastProvider><main style={{ padding: 16, display: 'grid', gap: 16 }}>
    <Button onClick={() => setMounted(value => !value)}>Toggle theme readers</Button>
    {mounted && <><ThemeConsumer name="A" /><ThemeConsumer name="B" /><ThemeConsumer name="C" /></>}
    <Select label="Review selection" value={choice} onValueChange={setChoice} options={[{ value: 'a', label: 'Alpha' }, { value: 'i', label: 'India' }, { value: 'o', label: 'Omega' }, { value: 'x', label: 'Disabled last', disabled: true }]} />
    <DropdownMenu label="Review menu" trigger={<Button>Open review menu</Button>} items={[{ id: 'a', label: 'Alpha', onSelect: () => {} }, { id: 'i', label: 'India', onSelect: () => {} }, { id: 'o', label: 'Omega', onSelect: () => {} }]} />
    <Button onClick={() => setOpen(true)}>Open media dialog</Button>
    <div id="media-dialog"><Dialog title="Media dialog" open={open} onOpenChange={setOpen}>
      <input id="first-field" aria-label="First field" />
      <audio id="audio" controls />
      <video id="video" controls width="180" height="110" />
      <details><summary id="summary">Disclosure</summary><button id="closed-button">Initially hidden</button></details>
      <input id="hidden-field" style={{ visibility: 'hidden' }} />
      <div contentEditable={false} id="not-editable">Not editable</div>
      <div contentEditable id="editor" suppressContentEditableWarning>Editable text</div>
    </Dialog></div>
    <UIProvider brandAssetBase="/deployment/brand"><div id="based-logo"><Logo animation="alive" markOnly interactive /></div><div id="override-logo"><Logo assetBaseUrl="/brand/" /></div></UIProvider>
    <img alt="Exported package logo" src={logoURL} width="40" height="40" />
    <div ref={setScope} id="custom-scope" style={{ position: 'relative', width: 280, height: 240, overflow: 'hidden' }}><ModalScope container={scope}><CustomContained /></ModalScope></div>
  </main></ToastProvider></UIProvider>;
}
createRoot(document.getElementById('root')!).render(<StrictMode><App /></StrictMode>);
