import { createRoot } from 'react-dom/client';
import '@fontsource-variable/dm-sans';
import '@molr/ui/styles.css';
import '../../../webui/src/styles.css';
import { LocaleProvider } from '../../../webui/src/state';
import { Login } from '../../../webui/src/interfaces/official/features/Login';

// Render the real login component for visual review without an authentication
// session or backend. Capture submission before React's handler can send it.
window.addEventListener('submit', event => { event.preventDefault(); event.stopImmediatePropagation(); }, true);
createRoot(document.getElementById('root')!).render(<LocaleProvider><Login /></LocaleProvider>);
