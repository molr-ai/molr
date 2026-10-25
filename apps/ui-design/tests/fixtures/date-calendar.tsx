import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { UIProvider } from '@molr/ui';
import { DateTimeCalendar } from '../../../../packages/ui/src/components/DateTimeCalendar';
import '@molr/ui/styles.css';

function Fixture() {
  const requestedMode = new URLSearchParams(location.search).get('mode');
  const mode = requestedMode === 'time' || requestedMode === 'month' || requestedMode === 'date' ? requestedMode : 'datetime';
  const [locale, setLocale] = useState<'en' | 'zh'>('en');
  const [zone, setZone] = useState('UTC');
  const [min, setMin] = useState('2026-03-10T00:00:00Z');
  const [value, setValue] = useState('2026-03-15T12:30:00Z');
  return <UIProvider locale={locale}><main style={{ maxWidth: 640, margin: '24px auto' }}>
    <h1>Date calendar</h1>
    <label>Minimum<input aria-label="Minimum" value={min} onChange={event => setMin(event.target.value)} /></label>
    <label>Timezone<select aria-label="Timezone" value={zone} onChange={event => setZone(event.target.value)}>
      <option>UTC</option><option>America/New_York</option>
    </select></label>
    <button onClick={() => setLocale(previous => previous === 'en' ? 'zh' : 'en')}>Toggle language</button>
    <DateTimeCalendar value={value} min={min || undefined} timeZone={zone} mode={mode} onApply={setValue} onCancel={() => {}} />
    <output aria-label="Selected instant">{value}</output>
  </main></UIProvider>;
}
createRoot(document.getElementById('root')!).render(<Fixture />);
