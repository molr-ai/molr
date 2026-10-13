import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import '@fontsource-variable/dm-sans';
import '@molr/ui/styles.css';
import './styles.css';
import { App } from './App';
import { Scrollbars } from './scrollbars';
createRoot(document.getElementById('root')!).render(<StrictMode><Scrollbars /><App /></StrictMode>);
