import { useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Button } from '@molr/ui';
import '@molr/ui/styles.css';
import '../../src/styles.css';
import { LocaleProvider, SessionProvider } from '../../src/state';
import { ErrorToastProvider } from '../../src/error-toasts';
import { Browser } from '../../src/interfaces/official/features/Browser';
import { Terminals } from '../../src/interfaces/official/features/Terminals';
import type { FloatingBounds } from '../../src/interfaces/official/features/useFloatingPanel';

// Component regression fixture; Playwright supplies capability and error responses.
// Real shell execution is covered separately by terminal-control.spec.ts.
function Example() {
  const bounds = useRef<FloatingBounds | null>(null), [open, setOpen] = useState(true);
  const browser = new URLSearchParams(location.search).get('panel') === 'browser';
  return <><Button onClick={() => setOpen(true)}>Show terminal</Button>{open && (browser ? <Browser assistantID="terminal-fixture" docked floating={false} onFloatingChange={() => {}} bounds={bounds} onClose={() => setOpen(false)}/> : <Terminals assistantID="terminal-fixture" assistantName="Terminal fixture" docked floating={false} onFloatingChange={() => {}} bounds={bounds} onClose={() => setOpen(false)}/>)}</>;
}
createRoot(document.getElementById('root')!).render(<LocaleProvider><SessionProvider><ErrorToastProvider><Example/></ErrorToastProvider></SessionProvider></LocaleProvider>);
