import { createRoot } from 'react-dom/client';
import '@molr/ui/styles.css';
import '../../src/styles.css';
import { LocaleProvider, SessionProvider } from '../../src/state';
import { ErrorToastProvider } from '../../src/error-toasts';
import { GrantEditor } from '../../src/interfaces/official/features/GrantEditor';
import type { Assistant, Resource } from '@molr/sdk';
const resource: Resource = { ID: 'compute', Kind: 'compute', Name: 'Local compute', Provider: 'local-sandbox', State: 'available', Actions: ['execute'], Version: 1 };
createRoot(document.getElementById('root')!).render(<LocaleProvider><SessionProvider><ErrorToastProvider><GrantEditor assistant={{ ID: 'assistant', Name: 'Scope test' } as Assistant} resources={[resource]} initialResource={resource} initialFullAccess onClose={() => {}} /></ErrorToastProvider></SessionProvider></LocaleProvider>);
