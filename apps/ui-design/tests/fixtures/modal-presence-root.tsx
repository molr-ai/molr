import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ToastProvider, UIProvider } from '@molr/ui';
import { ModalPresenceFixture } from './modal-presence';
import '@molr/ui/styles.css';

createRoot(document.getElementById('root')!).render(<StrictMode><UIProvider locale="en"><ToastProvider><ModalPresenceFixture /></ToastProvider></UIProvider></StrictMode>);
