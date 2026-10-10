import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { DateTimePicker, Select, UIProvider } from '@molr/ui';
import '@molr/ui/styles.css';

const params = new URLSearchParams(location.search);
const fixed = Date.parse(params.get('now') || '');
// An optional fixed clock makes DST scenarios reviewable in this test fixture.
if (Number.isFinite(fixed)) {
  globalThis.Date = new Proxy(Date, {
    construct(target, args) { return Reflect.construct(target, args.length ? args : [fixed]); },
    get(target, key) { return key === 'now' ? () => fixed : Reflect.get(target, key); },
  });
}
function Example() {
  const [value, setValue] = useState<string | null>(params.get('value'));
  const [changes, setChanges] = useState(0);
  const [zone, setZone] = useState(params.get('zone') || 'America/New_York');
  return <UIProvider locale="en"><main style={{ maxWidth: 640, margin: '32px auto', padding: 16 }}>
    <h1>Date preset timezone</h1>
    <p>Base instant: {new Date().toISOString()}</p>
    <Select label="Timezone" value={zone} onValueChange={setZone} options={['America/New_York', 'Asia/Tokyo', 'UTC', 'local'].map(value => ({ value, label: value === 'local' ? 'Browser timezone' : value }))} />
    <DateTimePicker label="Expiry" timeZone={zone === 'local' ? undefined : zone} value={value} onValueChange={next => { setValue(next); setChanges(count => count + 1); }} />
    <p>Selected instant: <output aria-label="Selected instant">{value ?? 'none'}</output></p>
    <p>Changes: <output aria-label="Changes">{changes}</output></p>
  </main></UIProvider>;
}
createRoot(document.getElementById('root')!).render(<Example />);
