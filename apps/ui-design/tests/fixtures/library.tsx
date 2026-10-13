import { StrictMode, useCallback, useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Button, Checkbox, Dialog, Drawer, DropdownMenu, Popover, Progress, RadioGroup, Select, Table, Tabs, ThemeSelect, ToastProvider, Tooltip, UIProvider, useToast } from '@molr/ui';
import type { UILocale } from '@molr/ui';
import '@molr/ui/styles.css';

function Contracts() {
  const [locale, setLocale] = useState<UILocale>('en');
  return <UIProvider locale={locale}><ToastProvider><button onClick={() => setLocale(value => value === 'en' ? 'zh' : 'en')}>Switch locale</button><Examples /></ToastProvider></UIProvider>;
}
function Examples() {
  const [dialog, setDialog] = useState(false);
  const [modalMounted, setModalMounted] = useState(true);
  const [nested, setNested] = useState(false);
  const [tab, setTab] = useState('one');
  const [popover, setPopover] = useState(false);
  const [selection, setSelection] = useState<string[]>(['other-page']);
  const [loading, setLoading] = useState(false);
  const [mode, setMode] = useState('a');
  const [formResult, setFormResult] = useState('');
  const [notificationId, setNotificationId] = useState<number | null>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  const { toast, update, dismiss } = useToast();
  return <main style={{ display: 'grid', gap: 24, maxWidth: 680 }}>
    <form onSubmit={event => { event.preventDefault(); setFormResult(JSON.stringify([...new FormData(event.currentTarget)])); }}>
      <RadioGroup label="Delivery" name="delivery" value={mode} onValueChange={setMode} options={[{ value: 'a', label: 'Automatic' }, { value: 'b', label: 'Manual' }]} />
      <Checkbox label="Include files" name="files" value="included" defaultChecked />
      <Button type="submit">Read form</Button><output>{formResult}</output>
    </form>
    <Tooltip content="Open modal details"><Button onClick={() => { setModalMounted(true); setDialog(true); }}>Open modal</Button></Tooltip>
    <Button onClick={() => setModalMounted(false)}>Unmount closed modal</Button>
    {modalMounted && <Dialog title="Initial focus" open={dialog} onOpenChange={setDialog} initialFocusRef={cancel} footer={<button ref={cancel} onClick={() => setDialog(false)}>Cancel modal</button>}>
      <RadioGroup label="Modal choice" name="modal-choice" value={mode} onValueChange={setMode} options={[{ value: 'a', label: 'Automatic' }, { value: 'b', label: 'Manual' }]} />
      <Tooltip content="Help inside a modal"><Button>Modal help</Button></Tooltip>
      <Button onClick={() => setNested(true)}>Open nested modal</Button>
      <Dialog title="Nested details" size="lg" open={nested} onOpenChange={setNested} footer={<Button onClick={() => setNested(false)}>Finish nested modal</Button>}><p>Independent modal layer</p></Dialog>
    </Dialog>}
    <Tabs label="Vertical sections" orientation="vertical" value={tab} onValueChange={setTab} items={[
      { value: 'one', label: 'First', content: 'First panel' },
      { value: 'disabled', label: 'Disabled', disabled: true, content: null },
      { value: 'two', label: 'Second', content: 'Second panel' },
    ]} />
    <Button onClick={() => setPopover(true)}>Open controlled details</Button>
    <Popover title="Controlled details" open={popover} onOpenChange={setPopover} trigger={<Button>Details trigger</Button>}><Button onClick={() => setPopover(false)}>Apply details</Button></Popover>
    <Select label="Inherited placeholder" options={[{ value: 'a', label: 'A' }]} value="" onValueChange={() => {}} />
    <ThemeSelect value="system" onValueChange={() => {}} />
    <Button onClick={() => setLoading(value => !value)}>Toggle table loading</Button>
    <Table caption="Selection contract" rows={[{ id: 'a', name: 'Available' }, { id: 'b', name: 'Unavailable' }]} columns={[{ key: 'name', header: 'Name', cell: row => row.name }]} rowKey={row => row.id} loading={loading} selection={{ keys: selection, onChange: setSelection, rowLabel: row => row.name, isDisabled: row => row.id === 'b' }} />
    <output aria-label="Selected keys">{selection.join(',')}</output>
    <Progress label="Below minimum" value={-20} /><Progress label="Above maximum" value={120} /><Progress label="Unknown total" value={Number.NaN} />
    <Button onClick={() => setNotificationId(toast({ title: 'Before update', duration: 0 }))}>Create persistent notice</Button>
    <Button onClick={() => { if (notificationId) update(notificationId, { title: 'After update', description: 'Updated in place', tone: 'positive' }); }}>Update notice</Button>
    <Button onClick={() => { if (notificationId) dismiss(notificationId); }}>Dismiss notice</Button>
    <Button onClick={() => toast({ title: 'Timed notice', duration: 3000 })}>Create timed notice</Button>
    <TriggerRefs />
    <DrawerMotion />
  </main>;
}

function DrawerMotion() {
  const [open, setOpen] = useState(false);
  const [mounted, setMounted] = useState(true);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);
  return <section aria-label="Drawer motion">
    <Tooltip content="Open drawer details"><Button onClick={() => { setMounted(true); setOpen(true); }}>Open motion drawer</Button></Tooltip>
    {mounted && <Drawer title="Motion drawer" open={open} onOpenChange={setOpen}>
      <Button onClick={() => { setOpen(false); timer.current = setTimeout(() => setOpen(true), 60); }}>Close and reopen</Button>
      <Button onClick={() => { if (timer.current) clearTimeout(timer.current); setOpen(false); setMounted(false); }}>Unmount drawer</Button>
    </Drawer>}
  </section>;
}

function TriggerRefs() {
  const [mounted, setMounted] = useState(true);
  const [revision, setRevision] = useState(0);
  const [report, setReport] = useState('');
  const active = useRef(new Set<HTMLElement>());
  const attachments = useRef(0);
  const legacy = useRef<HTMLElement | null>(null);
  const observe = useCallback((element: HTMLElement | null) => {
    if (!element) return;
    active.current.add(element);
    attachments.current++;
    return () => { active.current.delete(element); };
  }, []);
  const observeLegacy = useCallback((element: HTMLElement | null) => { legacy.current = element; }, []);
  return <section aria-label="Trigger ref lifecycle">
    <Button onClick={() => setRevision(value => value + 1)}>Rerender triggers</Button>
    <Button onClick={() => setMounted(false)}>Unmount triggers</Button>
    <Button onClick={() => setReport(JSON.stringify({ active: active.current.size, attachments: attachments.current, legacy: !!legacy.current }))}>Read ref state</Button>
    <output aria-label="Ref state">{report}</output>
    {mounted && <>
      <Popover title="Ref popover" trigger={<Button ref={observe}>Ref details {revision}</Button>}>Details</Popover>
      <DropdownMenu label="Ref menu" trigger={<Button ref={observe}>Ref actions {revision}</Button>} items={[{ id: 'action', label: 'Action', onSelect: () => {} }]} />
      <Tooltip content="Ref tooltip"><Button ref={observe}>Ref help {revision}</Button></Tooltip>
      <Tooltip content="Legacy ref tooltip"><Button ref={observeLegacy}>Legacy ref help</Button></Tooltip>
    </>}
  </section>;
}
document.body.style.cssText = 'margin:24px;font-family:var(--font-sans);background:var(--color-background);color:var(--color-text)';
createRoot(document.getElementById('root')!).render(<StrictMode><Contracts /></StrictMode>);
