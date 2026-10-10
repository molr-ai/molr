import { StrictMode, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Button, Tooltip, UIProvider } from '@molr/ui';
import '@molr/ui/styles.css';

function Fixture() {
  const [count, setCount] = useState(500);
  return <UIProvider locale="en"><main>
    <label>Type here<input aria-label="Type here" /></label>
    <button onClick={() => setCount(value => value ? 0 : 500)}>Toggle tooltips</button>
    <div>{Array.from({ length: count }, (_, index) => <Tooltip key={index} content={`Help ${index}`} openOnClick={index === 0}><Button>Item {index}</Button></Tooltip>)}</div>
  </main></UIProvider>;
}
createRoot(document.getElementById('root')!).render(<StrictMode><Fixture /></StrictMode>);
