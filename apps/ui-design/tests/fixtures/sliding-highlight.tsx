import { StrictMode, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { AssistantList, Button, SlidingHighlight, UIProvider } from '@molr/ui';
import '@molr/ui/styles.css';

const colors = new URLSearchParams(location.search).has('colors');
if (colors) await Promise.all([import('../../../webui/src/styles.css'), import('../../../webui/src/interfaces/official/features/settings.css'), import('../../../webui/src/interfaces/official/features/resources.css')]);

function App() {
  const [sandbox, setSandbox] = useState('local');
  const [section, setSection] = useState('general');
  const [selected, setSelected] = useState<string | undefined>('one');
  const [disabled, setDisabled] = useState(false), [mounted, setMounted] = useState(true);
  return <UIProvider><main style={{ width: 320, padding: 16 }}>
    <Button onClick={() => setSelected(undefined)}>Clear selection</Button>
    <Button onClick={() => setDisabled(value => !value)}>Toggle disabled</Button>
    <Button onClick={() => setMounted(value => !value)}>Toggle mounted</Button>
    {mounted && <AssistantList label="Assistants" entries={[
      { id: 'one', name: 'Daily', preview: 'First preview', unread: 3, pinned: true },
      { id: 'two', name: 'Research', preview: 'Second preview' },
      { id: 'three', name: 'Writing', preview: 'Third preview', avatar: 'preset:fox' },
      { id: 'four', name: 'Planning', preview: 'Fourth preview', unread: 2 },
      ...['five', 'six', 'seven'].map(id => ({ id, name: id, preview: 'Distant preview' })),
    ]} selectedID={selected} onSelect={setSelected} mobile={disabled} unreadLabel={count => `${count} unread`} pinnedLabel="Pinned" pinIcon="◆" emptyLabel="Empty" />}
    {colors && <><button className="nav-button" aria-current="page">Active menu</button>
      <SlidingHighlight as="nav" className="settings-nav" value={section}><button data-sliding-item="general" aria-current={section === 'general' ? 'page' : undefined} onClick={() => setSection('general')}><svg className="settings-nav-icon" width="16" height="16" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" /></svg><span>General</span></button><button data-sliding-item="profile" aria-current={section === 'profile' ? 'page' : undefined} onClick={() => setSection('profile')}><svg className="settings-nav-icon" width="16" height="16" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" /></svg><span>Profile</span></button></SlidingHighlight>
      <SlidingHighlight as="ul" className="sandbox-list" value={sandbox}>{['local', 'remote'].map(id => <li key={id}><button className="sandbox-item resource-sidebar-item" data-sliding-item={id} aria-pressed={sandbox === id} onClick={() => setSandbox(id)}><svg width="17" height="17" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" /></svg><span className="resource-sidebar-copy"><strong>{id}</strong><small>Example sandbox</small><span className={`resource-sidebar-state ${id === 'local' ? 'is-connected' : ''}`}>{id === 'local' ? 'Connected' : 'Offline'}</span></span></button></li>)}</SlidingHighlight></>}
  </main></UIProvider>;
}
createRoot(document.getElementById('root')!).render(<StrictMode><App /></StrictMode>);
