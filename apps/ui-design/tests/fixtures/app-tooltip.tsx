import { StrictMode, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Tooltip, UIProvider, useBreakpoint } from '@molr/ui';
import { Calendar, MessageCircle, Settings } from 'lucide-react';
import '@molr/ui/styles.css';
import '../../../webui/src/styles.css';
import '../../../webui/src/app-mode.css';

function Fixture() {
  const [page, setPage] = useState('Assistants');
  const mobile = useBreakpoint('mobile');
  const app = !new URLSearchParams(location.search).has('browser');
  return <UIProvider locale="en"><div className={app ? 'app-frame' : undefined} data-app-platform="darwin">
    {app && <header className="app-titlebar">Sidebar tooltip acceptance</header>}
    <div className={`workspace${mobile ? ' workspace--mobile workspace--mobile-nav' : ''}`}>
      <nav className="workspace-nav" aria-label="Main navigation">
        {([['Assistants', MessageCircle], ['Plans', Calendar], ['Settings', Settings]] as const).map(([name, Icon]) =>
          <Tooltip key={name} content={name} side={mobile ? 'top' : 'right'} width="auto">
            <button type="button" className="nav-button" aria-label={name} aria-current={page === name ? 'page' : undefined} onClick={() => setPage(name)}><Icon size={22} /></button>
          </Tooltip>)}
      </nav>
      <main className="workspace-content"><h1>{page}</h1><p>Hover a sidebar icon until its tooltip appears, then click it. The page title should change on the first click.</p></main>
    </div>
  </div></UIProvider>;
}

createRoot(document.getElementById('root')!).render(<StrictMode><Fixture /></StrictMode>);
