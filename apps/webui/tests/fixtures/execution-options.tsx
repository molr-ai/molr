import { useRef } from 'react';
import { createRoot } from 'react-dom/client';
import '@molr/ui/styles.css';
import '../../src/styles.css';
import { LocaleProvider, SessionProvider } from '../../src/state';
import { ErrorToastProvider } from '../../src/error-toasts';
import { Terminals } from '../../src/interfaces/official/features/Terminals';
import { Browser } from '../../src/interfaces/official/features/Browser';
import type { FloatingBounds } from '../../src/interfaces/official/features/useFloatingPanel';
import type { BrowserBounds } from '../../src/interfaces/official/features/useBrowserView';
function Example() {
 const terminal = useRef<FloatingBounds | null>(null), browser = useRef<BrowserBounds | null>(null);
 return new URLSearchParams(location.search).get('kind') === 'browser'
 ? <Browser assistantID="fixture" docked floating={false} onFloatingChange={() => {}} bounds={browser} onClose={() => {}}/>
 : <Terminals assistantID="fixture" assistantName="Fixture" docked floating={false} onFloatingChange={() => {}} bounds={terminal} onClose={() => {}}/>;
}
createRoot(document.getElementById('root')!).render(<LocaleProvider><SessionProvider><ErrorToastProvider><Example/></ErrorToastProvider></SessionProvider></LocaleProvider>);
